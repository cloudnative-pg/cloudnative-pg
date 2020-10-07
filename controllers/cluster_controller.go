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

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/postgres"
)

const (
	podOwnerKey = ".metadata.controller"
	pvcOwnerKey = ".metadata.controller"
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
// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=clusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;list;get;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;create;watch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;create;delete;update;patch;list;watch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;delete;get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=create;patch;update
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create;patch;update
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=create;patch;update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create

// Reconcile is the operator reconciler loop
func (r *ClusterReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithName("cloud-native-postgresql").WithValues("namespace", req.Namespace, "name", req.Name)

	var cluster v1alpha1.Cluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		// This also happens when you delete a Cluster resource in k8s. If
		// that's the case, let's just wait for the Kubernetes garbage collector
		// to remove all the Pods of the cluster.
		if apierrs.IsNotFound(err) {
			log.Info("Resource has been deleted")

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

	// Update the status of this resource
	childPods, err := r.getManagedPods(ctx, cluster)
	if err != nil {
		log.Error(err, "Cannot extract the list of managed Pods")
		return ctrl.Result{}, err
	}

	childPVCs, err := r.getManagedPVCs(ctx, cluster)
	if err != nil {
		log.Error(err, "Cannot extract the list of managed PVCs")
		return ctrl.Result{}, err
	}

	if cluster.Status.CurrentPrimary != "" && cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		log.Info("There is a switchover or a failover "+
			"in progress, waiting for the operation to complete",
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary)
		return ctrl.Result{}, err
	}

	// Update the status section of this Cluster resource
	if err = r.updateResourceStatus(ctx, &cluster, childPods, childPVCs); err != nil {
		if apierrs.IsConflict(err) {
			// Let's wait for another reconciler loop, since the
			// status already changed
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	// Ensure we have the required global objects
	if err := r.createPostgresClusterObjects(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	// Get the replication status
	var instancesStatus postgres.PostgresqlStatusList
	if instancesStatus, err = r.getStatusFromInstances(ctx, childPods); err != nil {
		return ctrl.Result{}, err
	}

	// Recreate missing Pods
	if len(cluster.Status.DanglingPVC) > 0 {
		if !cluster.IsNodeMaintenanceWindowInProgress() && cluster.Status.ReadyInstances != cluster.Status.Instances {
			// A pod is not ready, let's retry
			log.V(2).Info("Waiting for node to be ready before attaching PVCs")
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, r.handleDanglingPVC(ctx, &cluster)
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
		return r.createPrimaryInstance(ctx, &cluster)
	}

	if cluster.Status.ReadyInstances != cluster.Status.Instances ||
		cluster.Status.Instances != cluster.Spec.Instances {
		return ctrl.Result{}, r.ReconcilePods(ctx, req, &cluster, childPods)
	}

	// Check if we need to handle a rolling upgrade
	return ctrl.Result{}, r.upgradeCluster(ctx, &cluster, childPods, instancesStatus)
}

// ReconcilePods decides when to create, scale up/down or wait for pods
func (r *ClusterReconciler) ReconcilePods(ctx context.Context,
	req ctrl.Request, cluster *v1alpha1.Cluster, childPods corev1.PodList) error {
	log := r.Log.WithName("cloud-native-postgresql").WithValues("namespace", req.Namespace, "name", req.Name)

	// Find if we have Pods that are not ready, this is OK
	// only if we are in upgrade mode and we have choose to just
	// wait for the node to come up
	if !cluster.IsNodeMaintenanceWindowReusePVC() && cluster.Status.ReadyInstances < cluster.Status.Instances {
		// A pod is not ready, let's retry
		log.V(2).Info("Waiting for Pod to be ready")
		return nil
	}

	// Are there missing nodes? Let's create one
	if cluster.Status.Instances < cluster.Spec.Instances {
		newNodeSerial, err := r.generateNodeSerial(ctx, cluster)
		if err != nil {
			return err
		}
		return r.joinReplicaInstance(ctx, newNodeSerial, cluster)
	}

	// Are there nodes to be removed? Remove one of them
	if cluster.Status.Instances > cluster.Spec.Instances {
		return r.scaleDownCluster(ctx, cluster, childPods)
	}

	return nil
}

// SetupWithManager creates a ClusterReconciler
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a new indexed field on Pods. This field will be used to easily
	// find all the Pods created by this controller
	if err := mgr.GetFieldIndexer().IndexField(
		&corev1.Pod{},
		podOwnerKey, func(rawObj runtime.Object) []string {
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

	// Create a new indexed field on PVCs.
	if err := mgr.GetFieldIndexer().IndexField(
		&corev1.PersistentVolumeClaim{},
		pvcOwnerKey, func(rawObj runtime.Object) []string {
			persistentVolumeClaim := rawObj.(*corev1.PersistentVolumeClaim)
			owner := metav1.GetControllerOf(persistentVolumeClaim)
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
