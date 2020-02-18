/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
)

const (
	podOwnerKey = ".metadata.controller"
)

var (
	apiGVString = v1alpha1.GroupVersion.String()
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=clusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=create;update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=create
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;create;delete;update;patch;list;watch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;delete;get;list;watch;update;patch

func (r *ClusterReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	_ = r.Log.WithValues("postgresql", req.NamespacedName)

	r.Log.V(4).Info("Reconciling", "namespace", req.Namespace, "name", req.Name)
	var cluster v1alpha1.Cluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		// This also happens when you delete a Cluster resource in k8s. If
		// that's the case, let's just wait for the Kubernetes garbage collector
		// to remove all the Pods of the cluster.
		if apierrs.IsNotFound(err) {
			r.Log.Info("Resource has been deleted", "namespace", req.Namespace, "name", req.Name)

			return ctrl.Result{}, nil
		}

		// This is a real error, maybe the RBAC configuration is wrong?
		return ctrl.Result{}, err
	}

	// Ensure we have one the required global objects
	if err := r.createPostgresClusterObjects(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	// Update the status of this resource
	var childPods corev1.PodList
	var err error

	childPods, err = r.getManagedPods(ctx, cluster)
	if err != nil {
		r.Log.Error(err, "Cannot create extract the list of managed Pods")
		return ctrl.Result{}, err
	}

	// Update the status section of this Cluster resource
	if err := r.updateResourceStatus(ctx, &cluster, childPods); err != nil {
		return ctrl.Result{}, err
	}

	// We have these cases now:
	//
	// 1 - There is no existent Pod for this PostgreSQL cluster ==> we need to create the
	// first node from which we will join the others
	//
	// 2 - There is one Pod, and that one is still not ready ==> we need to wait
	// for the first node to be ready
	//
	// 3 - We have already some Pods, all they all ready ==> we can create the other
	// pods joining the node that we already have.
	if cluster.Status.Instances == 0 {
		newNodeSerial, err := r.generateNodeSerial(ctx, &cluster)
		if err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, r.createMasterInstance(ctx, newNodeSerial, &cluster)
	}

	// Find if we have Pods that are not ready
	if cluster.Status.ReadyInstances != cluster.Status.Instances {
		// A pod is not ready, let's retry
		r.Log.Info("Waiting for node to be ready")
		return ctrl.Result{}, nil
	}

	// TODO failing nodes?

	// Are there missing nodes? Let's create one.
	if cluster.Status.Instances < cluster.Spec.Instances {
		newNodeSerial, err := r.generateNodeSerial(ctx, &cluster)
		if err != nil {
			return ctrl.Result{}, err
		}

		return r.joinReplicaInstance(ctx, newNodeSerial, &cluster)
	}

	// Are there nodes to be removed? Remove one of them
	if cluster.Status.Instances > cluster.Spec.Instances {
		// Is there one pod to be deleted?
		sacrificialPod := getSacrificialPod(childPods.Items)
		if sacrificialPod == nil {
			r.Log.Info("There are no instances to be sacrificed. Wait for the next sync loop")
			return ctrl.Result{}, nil
		}

		r.Log.Info("Too many nodes for cluster, deleting an instance",
			"cluster", cluster.Name,
			"namespace", cluster.Namespace,
			"pod", sacrificialPod.Name)
		err = r.Delete(ctx, sacrificialPod)
		if err != nil {
			r.Log.Error(err, "Cannot kill the Pod to scale down")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a new indexed field on Pods. This field will be used to easily
	// find all the Pods created by this controller
	if err := mgr.GetFieldIndexer().IndexField(&corev1.Pod{}, podOwnerKey, func(rawObj runtime.Object) []string {
		pod := rawObj.(*corev1.Pod)
		owner := metav1.GetControllerOf(pod)
		if owner == nil {
			return nil
		}

		if owner.APIVersion != apiGVString || owner.Kind != "Cluster" {
			return nil
		}

		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Cluster{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
