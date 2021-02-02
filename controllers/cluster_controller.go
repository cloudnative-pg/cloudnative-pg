/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package controllers contains the controller of the CRD
package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	apiv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/expectations"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

const (
	podOwnerKey = ".metadata.controller"
	pvcOwnerKey = ".metadata.controller"
	jobOwnerKey = ".metadata.controller"
)

var (
	apiGVString         = apiv1.GroupVersion.String()
	apiv1alpha1GVString = apiv1alpha1.GroupVersion.String()
)

// ClusterReconciler reconciles a Cluster objects
type ClusterReconciler struct {
	client.Client
	Log             logr.Logger
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	podExpectations *expectations.ControllerExpectations
	jobExpectations *expectations.ControllerExpectations
	pvcExpectations *expectations.ControllerExpectations
}

// Alphabetical order to not repeat or miss permissions
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;update;list
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;update;list
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;update;list
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update
// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=clusters/status,verbs=get;watch;update;patch
// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=clusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;delete;get;list;watch;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create;patch;update
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=create;patch;update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;create;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;list;get;watch
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=create;patch;update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;create;delete;update;patch;list;watch

// Reconcile is the operator reconciler loop
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("namespace", req.Namespace, "name", req.Name)

	var cluster apiv1.Cluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		// This also happens when you delete a Cluster resource in k8s. If
		// that's the case, let's just wait for the Kubernetes garbage collector
		// to remove all the Pods of the cluster.
		if apierrs.IsNotFound(err) {
			log.Info("Resource has been deleted")

			// Let's remove every expectation we have about this cluster
			r.deleteExpectations(&apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      req.Name,
					Namespace: req.Namespace,
				},
			})

			return ctrl.Result{}, nil
		}

		// This is a real error, maybe the RBAC configuration is wrong?
		return ctrl.Result{}, fmt.Errorf("cannot get the managed resource: %w", err)
	}

	var namespace corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Namespace: "", Name: req.Namespace}, &namespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot get the containing namespace: %w", err)
	}

	if !namespace.DeletionTimestamp.IsZero() {
		// This happens when you delete a namespace containing a Cluster resource. If that's the case,
		// let's just wait for the Kubernetes to remove all object in the namespace.
		return ctrl.Result{}, nil
	}

	// Update the status of this resource
	resources, err := r.getManagedResources(ctx, cluster)
	if err != nil {
		log.Error(err, "Cannot extract the list of managed resources")
		return ctrl.Result{}, err
	}

	// Update the status section of this Cluster resource
	if err = r.updateResourceStatus(ctx, &cluster, resources); err != nil {
		if apierrs.IsConflict(err) {
			// Requeue a new reconciliation cycle, as in this point we need
			// to quickly react the changes
			return ctrl.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, fmt.Errorf("cannot update the resource status: %w", err)
	}

	if cluster.Status.CurrentPrimary != "" && cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		log.Info("There is a switchover or a failover "+
			"in progress, waiting for the operation to complete",
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary)

		// There is a switchover or a failover in progress.
		// We need to ensure that we are keeping synchronous_standby_names
		// aligned with the actual target primary server.
		// This is the reason why we aligning the ConfigMap here
		return ctrl.Result{RequeueAfter: 1 * time.Second}, r.createOrPatchPostgresConfigMap(ctx, &cluster)
	}

	// Ensure we have the required global objects
	if err := r.createPostgresClusterObjects(ctx, &cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot create Cluster auxiliary objects: %w", err)
	}

	// Get the replication status
	var instancesStatus postgres.PostgresqlStatusList
	if instancesStatus, err = r.getStatusFromInstances(ctx, resources.pods); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot get status from instances: %w", err)
	}

	// Update the target primary name from the Pods status.
	// This means issuing a failover or switchover when needed.
	selectedPrimary, err := r.updateTargetPrimaryFromPods(ctx, &cluster, instancesStatus, resources)
	if err != nil {
		if err == ErrWalReceiversRunning {
			log.Info("Waiting for the all WAL receivers to be down to elect a new primary")
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		return ctrl.Result{}, fmt.Errorf("cannot update target primary from pods: %w", err)
	}
	if selectedPrimary != "" {
		// If we selected a new primary, stop the reconciliation loop here
		log.Info("Waiting for the new primary to notice the promotion request",
			"newPrimary", selectedPrimary)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Update the labels for the -rw service to work correctly
	if err = r.updateLabelsOnPods(ctx, &cluster, resources.pods); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update labels on pods: %w", err)
	}

	// Act on Pods and PVCs only if there is nothing that is currently being created or deleted
	if !r.satisfiedExpectations(&cluster) {
		// If an expectation is not met, let's try invoking a reconciliation loop
		// in one seconds, just to see if something has changed or not
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	if !resources.allPodsAreActive() {
		log.V(2).Info("A managed resource is currently being created or deleted. Waiting")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Reconcile PVC resource requirements
	if err = r.ReconcilePVCs(ctx, &cluster, resources); err != nil {
		if apierrs.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	// Reconcile Pods
	return r.ReconcilePods(ctx, req, &cluster, resources, instancesStatus)
}

// satisfiedExpectations check if the expectations for a certain cluster are met
func (r *ClusterReconciler) satisfiedExpectations(cluster *apiv1.Cluster) bool {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	key := expectations.KeyFunc(cluster)
	if !r.podExpectations.SatisfiedExpectations(key) {
		log.Info("Pod expectations are not met")
		return false
	}
	if !r.jobExpectations.SatisfiedExpectations(key) {
		log.Info("Job expectations are not met")
		return false
	}
	if !r.pvcExpectations.SatisfiedExpectations(key) {
		log.Info("PVC expectations are not met")
		return false
	}

	return true
}

// deleteExpectations remove expectations we have about a certain cluster
func (r *ClusterReconciler) deleteExpectations(cluster *apiv1.Cluster) {
	key := expectations.KeyFunc(cluster)
	r.podExpectations.DeleteExpectations(key)
	r.jobExpectations.DeleteExpectations(key)
	r.pvcExpectations.DeleteExpectations(key)
}

// ReconcilePVCs align the PVCs that are backing our cluster with the user specifications
func (r *ClusterReconciler) ReconcilePVCs(ctx context.Context, cluster *apiv1.Cluster,
	resources *managedResources) error {
	if !cluster.ShouldResizeInUseVolumes() {
		return nil
	}

	quantity, err := resource.ParseQuantity(cluster.Spec.StorageConfiguration.Size)
	if err != nil {
		return fmt.Errorf("while parsing PVC size %v: %w", cluster.Spec.StorageConfiguration.Size, err)
	}

	for idx := range resources.pvcs.Items {
		oldPVC := resources.pvcs.Items[idx].DeepCopy()
		oldQuantity, ok := resources.pvcs.Items[idx].Spec.Resources.Requests["storage"]

		switch {
		case !ok:
			// Missing storage requirement for PVC
			fallthrough

		case oldQuantity.AsDec().Cmp(quantity.AsDec()) == -1:
			// Increasing storage resources
			resources.pvcs.Items[idx].Spec.Resources.Requests["storage"] = quantity
			if err = r.Patch(ctx, &resources.pvcs.Items[idx], client.MergeFrom(oldPVC)); err != nil {
				// Decreasing resources is not possible
				log.Error(err, "error while changing PVC storage requirement",
					"from", oldQuantity, "to", quantity,
					"pvcName", resources.pvcs.Items[idx].Name)

				// We are reaching two errors in two different conditions:
				//
				// 1. we hit a Conflict => a successive reconciliation loop will fix it
				// 2. the StorageClass we used don't support PVC resizing => there's nothing we can do
				//    about it
			}

		case oldQuantity.AsDec().Cmp(quantity.AsDec()) == 1:
			// Decreasing resources is not possible
			log.Info("cannot decrease storage requirement",
				"from", oldQuantity, "to", quantity,
				"pvcName", resources.pvcs.Items[idx].Name)
		}
	}

	return nil
}

// ReconcilePods decides when to create, scale up/down or wait for pods
func (r *ClusterReconciler) ReconcilePods(ctx context.Context, req ctrl.Request, cluster *apiv1.Cluster,
	resources *managedResources, instancesStatus postgres.PostgresqlStatusList) (ctrl.Result, error) {
	log := r.Log.WithValues("namespace", req.Namespace, "name", req.Name)

	// If we are joining a node, we should wait for the process to finish
	if resources.countRunningJobs() > 0 {
		log.V(2).Info("Waiting for jobs to finish",
			"clusterName", cluster.Name,
			"namespace", cluster.Namespace,
			"jobs", resources.jobs.Items)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Recreate missing Pods
	if len(cluster.Status.DanglingPVC) > 0 {
		if !cluster.IsNodeMaintenanceWindowInProgress() && cluster.Status.ReadyInstances != cluster.Status.Instances {
			// A pod is not ready, let's retry
			log.V(2).Info("Waiting for node to be ready before attaching PVCs")
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		if err := r.handleDanglingPVC(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}

		// Do another reconcile cycle after handling a dangling PVC
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
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
		return r.createPrimaryInstance(ctx, cluster)
	}

	// Stop acting here if there are non-ready Pods and cluster
	// is in maintenance mode reusing PVCs.
	// The user have choose to wait for the missing nodes to come up
	if !cluster.IsNodeMaintenanceWindowReusePVC() &&
		cluster.Status.ReadyInstances < cluster.Status.Instances {
		log.V(2).Info("Waiting for Pods to be ready")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Are there missing nodes? Let's create one
	if cluster.Status.Instances < cluster.Spec.Instances &&
		cluster.Status.ReadyInstances == cluster.Status.Instances {
		newNodeSerial, err := r.generateNodeSerial(ctx, cluster)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("cannot generate node serial: %w", err)
		}
		return r.joinReplicaInstance(ctx, newNodeSerial, cluster)
	}

	// Are there nodes to be removed? Remove one of them
	if cluster.Status.Instances > cluster.Spec.Instances {
		if err := r.scaleDownCluster(ctx, cluster, resources); err != nil {
			return ctrl.Result{}, fmt.Errorf("cannot scale down cluster: %w", err)
		}
	}

	// Stop acting here if there are non-ready Pods
	// In the rest of the function we are sure that
	// cluster.Status.Instances == cluster.Spec.Instances and
	// we don't need to modify the cluster topology
	if cluster.Status.ReadyInstances != cluster.Status.Instances ||
		cluster.Status.ReadyInstances != int32(len(instancesStatus.Items)) {
		log.V(2).Info("Waiting for Pods to be ready")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// If we need to apply an upgrade to the cluster, this is the right moment
	upgradedPod, err := r.upgradeCluster(ctx, cluster, resources.pods, instancesStatus)
	if err != nil {
		return ctrl.Result{}, err
	}
	if upgradedPod != "" {
		// Rolling upgrade is in progress, let's avoid marking stuff as synchronized
		// (but recheck in one second, just to be sure)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// When everything is reconciled, update the status
	if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseHealthy, ""); err != nil {
		return ctrl.Result{}, err
	}

	// Cleanup stuff
	return ctrl.Result{}, r.removeCompletedJobs(ctx, cluster, resources.jobs)
}

// removeCompletedJobs remove all the Jobs which are completed
func (r *ClusterReconciler) removeCompletedJobs(
	ctx context.Context,
	cluster *apiv1.Cluster,
	jobs batchv1.JobList) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	// Retrieve the cluster key
	key := expectations.KeyFunc(cluster)

	completeJobs := utils.FilterCompleteJobs(jobs.Items)
	if len(completeJobs) == 0 {
		return nil
	}

	if err := r.jobExpectations.ExpectDeletions(key, 0); err != nil {
		log.Error(err, "Unable to initialize jobExpectations", "key", key, "dels", 0)
	}

	for idx := range completeJobs {
		log.V(2).Info("Removing job", "job", jobs.Items[idx].Name)

		// We expect the deletion of the selected Job
		r.jobExpectations.RaiseExpectations(key, 0, 1)

		foreground := metav1.DeletePropagationForeground
		if err := r.Delete(ctx, &jobs.Items[idx], &client.DeleteOptions{
			PropagationPolicy: &foreground,
		}); err != nil {
			// We cannot observe a deletion if it was not accepted by the server
			r.jobExpectations.DeletionObserved(key)

			return fmt.Errorf("cannot delete job: %w", err)
		}
	}

	return nil
}

// SetupWithManager creates a ClusterReconciler
func (r *ClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Initialize expectations
	r.podExpectations = expectations.NewControllerExpectations()
	r.jobExpectations = expectations.NewControllerExpectations()
	r.pvcExpectations = expectations.NewControllerExpectations()

	// Create a new indexed field on Pods. This field will be used to easily
	// find all the Pods created by this controller
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&corev1.Pod{},
		podOwnerKey, func(rawObj client.Object) []string {
			pod := rawObj.(*corev1.Pod)
			owner := metav1.GetControllerOf(pod)
			if owner == nil {
				return nil
			}

			if owner.Kind != apiv1.ClusterKind {
				return nil
			}

			if owner.APIVersion != apiGVString && owner.APIVersion != apiv1alpha1GVString {
				return nil
			}

			return []string{owner.Name}
		}); err != nil {
		return err
	}

	// Create a new indexed field on PVCs.
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&corev1.PersistentVolumeClaim{},
		pvcOwnerKey, func(rawObj client.Object) []string {
			persistentVolumeClaim := rawObj.(*corev1.PersistentVolumeClaim)
			owner := metav1.GetControllerOf(persistentVolumeClaim)
			if owner == nil {
				return nil
			}

			if owner.Kind != apiv1.ClusterKind {
				return nil
			}

			if owner.APIVersion != apiGVString && owner.APIVersion != apiv1alpha1GVString {
				return nil
			}

			return []string{owner.Name}
		}); err != nil {
		return err
	}

	// Create a new indexed field on Jobs.
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&batchv1.Job{},
		jobOwnerKey, func(rawObj client.Object) []string {
			job := rawObj.(*batchv1.Job)
			owner := metav1.GetControllerOf(job)
			if owner == nil {
				return nil
			}

			if owner.Kind != apiv1.ClusterKind {
				return nil
			}

			if owner.APIVersion != apiGVString && owner.APIVersion != apiv1alpha1GVString {
				return nil
			}

			return []string{owner.Name}
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Cluster{}).
		Owns(&corev1.Pod{}).
		Owns(&batchv1.Job{}).
		WithEventFilter(&ClusterPredicate{}).
		Complete(r)
}
