/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package controllers contains the controller of the CRD
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
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/postgres"
)

const (
	podOwnerKey = ".metadata.controller"
)

var (
	apiGVString = v1alpha1.GroupVersion.String()
)

// ClusterReconciler reconciles a Cluster objects
type ClusterReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=clusters/status,verbs=get;watch;update;patch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;list;get;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=create;update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=create
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;create;delete;update;patch;list;watch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;delete;get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=create;patch;update
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create;patch;update
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=create;patch;update

// Reconcile is the operator reconciler loop
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

	var namespace corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Namespace: "", Name: req.Namespace}, &namespace); err != nil {
		return ctrl.Result{}, err
	}

	if !namespace.DeletionTimestamp.IsZero() {
		// This happens when you delete a namespace containing a Cluster resource. If that's the case,
		// let's just wait for the Kubernetes to remove all object in the namespace.
		return ctrl.Result{}, nil
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

	if cluster.Status.CurrentPrimary != "" && cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		r.Log.Info("Switchover in progress, waiting for the cluster to align")
		// TODO: check if the TargetPrimary is active, otherwise recovery?
		return ctrl.Result{}, err
	}

	// Update the status section of this Cluster resource
	if err = r.updateResourceStatus(ctx, &cluster, childPods); err != nil {
		if apierrs.IsConflict(err) {
			// Let's wait for another reconciler loop, since the
			// status already changed
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	// Find if we have Pods that we are upgrading
	if cluster.Status.InstancesBeingUpdated != 0 {
		r.Log.V(2).Info("There are nodes being upgraded, waiting for the new image to be applied",
			"clusterName", cluster.Name,
			"namespace", cluster.Namespace)
		return ctrl.Result{}, nil
	}

	// Get the replication status
	var instancesStatus postgres.PostgresqlStatusList
	if instancesStatus, err = r.getStatusFromInstances(ctx, childPods); err != nil {
		return ctrl.Result{}, err
	}

	// Update the target primary name from the Pods status
	if err = r.updateTargetPrimaryFromPods(ctx, &cluster, instancesStatus); err != nil {
		return ctrl.Result{}, err
	}

	// Update the labels for the -rw service to work correctly
	if err = r.updateLabelsOnPods(ctx, &cluster, childPods); err != nil {
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

		return ctrl.Result{}, r.createPrimaryInstance(ctx, newNodeSerial, &cluster)
	}

	// Find if we have Pods that are not ready
	if cluster.Status.ReadyInstances != cluster.Status.Instances {
		// A pod is not ready, let's retry
		r.Log.V(2).Info("Waiting for node to be ready",
			"clusterName", cluster.Name,
			"namespace", cluster.Namespace)
		return ctrl.Result{}, nil
	}

	// Is there a switchover or failover in progress?
	// Let's wait for it to terminate before applying the
	// following operations
	if cluster.Status.TargetPrimary != cluster.Status.CurrentPrimary {
		r.Log.V(2).Info("There is a switchover or a failover "+
			"in progress, waiting for the operation to complete",
			"clusterName", cluster.Name,
			"namespace", cluster.Namespace,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary)
		return ctrl.Result{}, nil
	}

	// Are there missing nodes? Let's create one
	if cluster.Status.Instances < cluster.Spec.Instances {
		newNodeSerial, err := r.generateNodeSerial(ctx, &cluster)
		if err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, r.joinReplicaInstance(ctx, newNodeSerial, &cluster)
	}

	// Are there nodes to be removed? Remove one of them
	if cluster.Status.Instances > cluster.Spec.Instances {
		return ctrl.Result{}, r.scaleDownCluster(ctx, &cluster, childPods)
	}

	// Check if we need to handle a rolling upgrade
	if cluster.Status.ImageName != cluster.Spec.ImageName {
		if err = r.upgradeCluster(ctx, &cluster, childPods, instancesStatus); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager creates a ClusterReconciler
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
