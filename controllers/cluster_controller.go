/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package controllers contains the controller of the CRD
package controllers

import (
	"context"
	"fmt"
	goruntime "runtime"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

const (
	podOwnerKey                   = ".metadata.controller"
	pvcOwnerKey                   = ".metadata.controller"
	jobOwnerKey                   = ".metadata.controller"
	poolerClusterKey              = ".spec.cluster.name"
	disableDefaultQueriesSpecPath = ".spec.monitoring.disableDefaultQueries"
)

var apiGVString = apiv1.GroupVersion.String()

// ClusterReconciler reconciles a Cluster objects
type ClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Alphabetical order to not repeat or miss permissions
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;update;list
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;update;list
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;update;list
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;delete;get;list;watch;update;patch
// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=clusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=clusters/status,verbs=get;watch;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create;patch;update;get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=create;patch;update;get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;create;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;list;get;watch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=create;patch;update;list;watch;get
// +kubebuilder:rbac:groups="",resources=services,verbs=get;create;delete;update;patch;list;watch

// Reconcile is the operator reconcile loop
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug(fmt.Sprintf("reconciling object %#q", req.NamespacedName))

	defer func() {
		contextLogger.Debug(fmt.Sprintf("object %#q has been reconciled", req.NamespacedName))
	}()

	var cluster apiv1.Cluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		// This also happens when you delete a Cluster resource in k8s. If
		// that's the case, let's just wait for the Kubernetes garbage collector
		// to remove all the Pods of the cluster.
		if apierrs.IsNotFound(err) {
			contextLogger.Info("Resource has been deleted")

			if err := r.deleteDanglingMonitoringConfigMaps(ctx, req.Namespace); err != nil && !apierrs.IsNotFound(err) {
				contextLogger.Error(
					err,
					"error while deleting dangling monitoring configMap",
					"configMapName", configuration.Current.MonitoringQueriesConfigmap,
					"namespace", req.Namespace,
				)
			}

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

	if utils.IsReconciliationDisabled(&cluster.ObjectMeta) {
		contextLogger.Warning("Disable reconciliation loop annotation set, skipping the reconciliation.")
		return ctrl.Result{}, nil
	}

	// Make sure default values are populated.
	cluster.Default()

	// Ensure we have the required global objects
	if err := r.createPostgresClusterObjects(ctx, &cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot create Cluster auxiliary objects: %w", err)
	}

	// Update the status of this resource
	resources, err := r.getManagedResources(ctx, cluster)
	if err != nil {
		contextLogger.Error(err, "Cannot extract the list of managed resources")
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

	if cluster.Status.CurrentPrimary != "" &&
		cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		contextLogger.Info("There is a switchover or a failover "+
			"in progress, waiting for the operation to complete",
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary)

		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Get the replication status
	instancesStatus := r.getStatusFromInstances(ctx, resources.pods)

	// Verify the architecture of all the instances and update the OnlineUpdateEnabled
	// field in the status
	onlineUpdateEnabled := configuration.Current.EnableInstanceManagerInplaceUpdates
	isArchitectureConsistent := r.checkPodsArchitecture(ctx, &instancesStatus)
	if !isArchitectureConsistent && onlineUpdateEnabled {
		contextLogger.Info("Architecture mismatch detected, disabling instance manager online updates")
		onlineUpdateEnabled = false
	}

	// We have already updated the status in updateResourceStatus call,
	// so we need to issue an extra update when the OnlineUpdateEnabled changes.
	// It's okay because it should not change often.
	//
	// We cannot merge this code with updateResourceStatus because
	// it needs to run after retrieving the status from the pods,
	// which is a time-expensive operation.
	if err = r.updateOnlineUpdateEnabled(ctx, &cluster, onlineUpdateEnabled); err != nil {
		if apierrs.IsConflict(err) {
			// Requeue a new reconciliation cycle, as in this point we need
			// to quickly react the changes
			return ctrl.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, fmt.Errorf("cannot update the resource status: %w", err)
	}

	// Update the target primary name from the Pods status.
	// This means issuing a failover or switchover when needed.
	selectedPrimary, err := r.updateTargetPrimaryFromPods(ctx, &cluster, instancesStatus, resources)
	if err != nil {
		if err == ErrWalReceiversRunning {
			contextLogger.Info("Waiting for all WAL receivers to be down to elect a new primary")
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
		contextLogger.Info("Cannot update target primary: operation cannot be fulfilled. "+
			"An immediate retry will be scheduled",
			"cluster", cluster.Name)
		return ctrl.Result{Requeue: true}, nil
	}
	if selectedPrimary != "" {
		// If we selected a new primary, stop the reconciliation loop here
		contextLogger.Info("Waiting for the new primary to notice the promotion request",
			"newPrimary", selectedPrimary)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Updates all the objects managed by the controller
	return r.reconcileResources(ctx, req, &cluster, resources, instancesStatus)
}

// reconcileResources updates all the objects managed by the controller
func (r *ClusterReconciler) reconcileResources(
	ctx context.Context, req ctrl.Request, cluster *apiv1.Cluster,
	resources *managedResources, instancesStatus postgres.PostgresqlStatusList,
) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	// Update the labels for the -rw service to work correctly
	if err := r.updateRoleLabelsOnPods(ctx, cluster, resources.pods); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update role labels on pods: %w", err)
	}

	// Update any modified/new labels coming from the cluster resource
	if err := r.updateClusterLabelsOnPods(ctx, cluster, resources.pods); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update cluster labels on pods: %w", err)
	}

	// Update any modified/new annotations coming from the cluster resource
	if err := r.updateClusterAnnotationsOnPods(ctx, cluster, resources.pods); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update annotations on pods: %w", err)
	}

	// Act on Pods and PVCs only if there is nothing that is currently being created or deleted
	if runningJobs := resources.countRunningJobs(); runningJobs > 0 {
		contextLogger.Debug("A job is currently running. Waiting", "count", runningJobs)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Delete Pods which have been evicted by the Kubelet
	result, err := r.deleteEvictedPods(ctx, cluster, resources)
	if err != nil {
		contextLogger.Error(err, "While deleting evicted pods")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	if result != nil {
		return *result, err
	}

	if !resources.allPodsAreActive() {
		contextLogger.Debug("A managed resource is currently being created or deleted. Waiting")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Reconcile PVC resource requirements
	if err := r.ReconcilePVCs(ctx, cluster, resources); err != nil {
		if apierrs.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	// Reconcile Pods
	if res, err := r.ReconcilePods(ctx, req, cluster, resources, instancesStatus); err != nil {
		return res, err
	}

	if len(resources.pods.Items) > 0 && resources.noPodsAreAlive() {
		return ctrl.Result{RequeueAfter: 1 * time.Second}, r.RegisterPhase(ctx, cluster, apiv1.PhaseUnrecoverable,
			"No pods are active, the cluster needs manual intervention ")
	}

	return ctrl.Result{}, nil
}

// deleteEvictedPods will delete the Pods that the Kubelet has evicted
func (r *ClusterReconciler) deleteEvictedPods(ctx context.Context, cluster *apiv1.Cluster,
	resources *managedResources) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)
	deletedPods := false

	for idx := range resources.pods.Items {
		if utils.IsPodEvicted(resources.pods.Items[idx]) {
			contextLogger.Warning("Deleting evicted pod",
				"pod", resources.pods.Items[idx].Name,
				"podStatus", resources.pods.Items[idx].Status)
			if err := r.Delete(ctx, &resources.pods.Items[idx]); err != nil {
				if apierrs.IsConflict(err) {
					return &ctrl.Result{Requeue: true}, nil
				}
				return nil, err
			}
			deletedPods = true
			r.Recorder.Eventf(cluster, "Normal", "DeletePod",
				"Deleted evicted Pod %v",
				resources.pods.Items[idx].Name)
		}
	}
	if deletedPods {
		// We cleaned up Pods which were evicted.
		// Let's wait for the informer cache to notice that
		return &ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}
	return nil, nil
}

// checkPodsArchitecture checks whether the architecture of the instances is consistent with the runtime one
func (r *ClusterReconciler) checkPodsArchitecture(ctx context.Context, status *postgres.PostgresqlStatusList) bool {
	contextLogger := log.FromContext(ctx)
	isConsistent := true

	for _, podStatus := range status.Items {
		switch podStatus.InstanceArch {
		case goruntime.GOARCH:
			// architecture matches, everything ok for this pod

		case "":
			// an empty podStatus.InstanceArch should be due to an old version of the instance manager
			contextLogger.Info("ignoring empty architecture from the instance",
				"pod", podStatus.Pod.Name)

		default:
			contextLogger.Info("Warning: mismatch architecture between controller and instances. "+
				"This is an unsupported configuration.",
				"controllerArch", goruntime.GOARCH,
				"instanceArch", podStatus.InstanceArch,
				"pod", podStatus.Pod.Name)
			isConsistent = false
		}
	}

	return isConsistent
}

// ReconcilePVCs align the PVCs that are backing our cluster with the user specifications
func (r *ClusterReconciler) ReconcilePVCs(ctx context.Context, cluster *apiv1.Cluster,
	resources *managedResources) error {
	contextLogger := log.FromContext(ctx)
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
				contextLogger.Error(err, "error while changing PVC storage requirement",
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
			contextLogger.Info("cannot decrease storage requirement",
				"from", oldQuantity, "to", quantity,
				"pvcName", resources.pvcs.Items[idx].Name)
		}
	}

	return nil
}

// ReconcilePods decides when to create, scale up/down or wait for pods
func (r *ClusterReconciler) ReconcilePods(ctx context.Context, req ctrl.Request, cluster *apiv1.Cluster,
	resources *managedResources, instancesStatus postgres.PostgresqlStatusList) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	// If we are joining a node, we should wait for the process to finish
	if resources.countRunningJobs() > 0 {
		contextLogger.Debug("Waiting for jobs to finish",
			"clusterName", cluster.Name,
			"namespace", cluster.Namespace,
			"jobs", len(resources.jobs.Items))
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	r.markPVCReadyForCompletedJobs(ctx, resources)

	// Work on the PVCs we currently have
	pvcNeedingMaintenance := len(cluster.Status.DanglingPVC) + len(cluster.Status.InitializingPVC)
	if pvcNeedingMaintenance > 0 {
		return r.reconcilePVCs(ctx, cluster)
	}

	if err := r.ensureHealthyPVCsAnnotation(ctx, cluster, resources); err != nil {
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
		return r.createPrimaryInstance(ctx, cluster)
	}

	// Stop acting here if there are non-ready Pods unless in maintenance reusing PVCs.
	// The user have choose to wait for the missing nodes to come up
	if !(cluster.IsNodeMaintenanceWindowInProgress() && cluster.IsReusePVCEnabled()) &&
		cluster.Status.ReadyInstances < cluster.Status.Instances {
		contextLogger.Debug("Waiting for Pods to be ready")
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
		cluster.Status.ReadyInstances != int32(len(instancesStatus.Items)) ||
		!instancesStatus.IsComplete() {
		contextLogger.Debug("Waiting for Pods to be ready")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	return r.handleRollingUpdate(ctx, cluster, resources, instancesStatus)
}

func (r *ClusterReconciler) ensureHealthyPVCsAnnotation(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources) error {
	contextLogger := log.FromContext(ctx)

	// Make sure that all healthy PVCs are marked as ready
	for _, pvcName := range cluster.Status.HealthyPVC {
		pvc := resources.getPVC(pvcName)
		if pvc == nil {
			contextLogger.Warning("unable to find pvc to annotate it as ready", "pvc", pvc.Name)
			continue
		}

		if pvc.Annotations[specs.PVCStatusAnnotationName] == specs.PVCStatusReady {
			continue
		}

		contextLogger.Info("PVC is already attached to the pod, marking it as ready",
			"pvc", pvc.Name)
		if err := r.setPVCStatusReady(ctx, pvc); err != nil {
			contextLogger.Error(err, "can't update PVC annotation as ready")
			return err
		}
	}

	return nil
}

func (r *ClusterReconciler) handleRollingUpdate(ctx context.Context, cluster *apiv1.Cluster,
	resources *managedResources, instancesStatus postgres.PostgresqlStatusList) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	// Stop acting here if there are Pods that are waiting for
	// an instance manager upgrade
	if instancesStatus.ArePodsUpgradingInstanceManager() {
		contextLogger.Debug("Waiting for Pods to complete instance manager upgrade")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Execute online update, if enabled
	if cluster.Status.OnlineUpdateEnabled {
		if err := r.upgradeInstanceManager(ctx, cluster, &instancesStatus); err != nil {
			return ctrl.Result{}, err
		}
	}

	// If we need to roll out a restart of any instance, this is the right moment
	// Do I have to roll out a new image?
	done, err := r.rolloutDueToCondition(ctx, cluster, &instancesStatus, IsPodNeedingRollout)
	if err != nil {
		return ctrl.Result{}, err
	}
	if done {
		// Rolling upgrade is in progress, let's avoid marking stuff as synchronized
		// (but recheck in one second, just to be sure)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// When everything is reconciled, update the status
	if err = r.RegisterPhase(ctx, cluster, apiv1.PhaseHealthy, ""); err != nil {
		return ctrl.Result{}, err
	}

	r.cleanupCompletedJobs(ctx, resources.jobs)
	return ctrl.Result{}, nil
}

// SetupWithManager creates a ClusterReconciler
func (r *ClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	err := r.createFieldIndexes(ctx, mgr)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Cluster{}).
		Owns(&corev1.Pod{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&policyv1beta1.PodDisruptionBudget{}).
		Watches(
			&source.Kind{Type: &corev1.ConfigMap{}},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigMapsToClusters(ctx)),
			builder.WithPredicates(configMapsPredicate),
		).
		Watches(
			&source.Kind{Type: &corev1.Secret{}},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretsToClusters(ctx)),
			builder.WithPredicates(secretsPredicate),
		).
		Watches(
			&source.Kind{Type: &apiv1.Pooler{}},
			handler.EnqueueRequestsFromMapFunc(r.mapPoolersToClusters(ctx)),
		).
		Watches(
			&source.Kind{Type: &corev1.Node{}},
			handler.EnqueueRequestsFromMapFunc(r.mapNodeToClusters(ctx)),
			builder.WithPredicates(nodesPredicate),
		).
		Complete(r)
}

// createFieldIndexes creates the indexes needed by this controller
func (r *ClusterReconciler) createFieldIndexes(ctx context.Context, mgr ctrl.Manager) error {
	// Create a new indexed field on Pods. This field will be used to easily
	// find all the Pods created by this controller
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&corev1.Pod{},
		podOwnerKey, func(rawObj client.Object) []string {
			pod := rawObj.(*corev1.Pod)

			if ownerName, ok := isOwnedByCluster(pod); ok {
				return []string{ownerName}
			}

			return nil
		}); err != nil {
		return err
	}

	// Create a new indexed field on Clusters. This field will be used to easily
	// find all Clusters with default queries enabled
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&apiv1.Cluster{},
		disableDefaultQueriesSpecPath, func(rawObj client.Object) []string {
			cluster := rawObj.(*apiv1.Cluster)

			if cluster.Spec.Monitoring == nil ||
				cluster.Spec.Monitoring.DisableDefaultQueries == nil ||
				!*cluster.Spec.Monitoring.DisableDefaultQueries {
				return []string{"false"}
			}
			return []string{"true"}
		}); err != nil {
		return err
	}

	// Create a new indexed field on Pods. This field will be used to easily
	// find all the Pods created by node
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&corev1.Pod{},
		".spec.nodeName", func(rawObj client.Object) []string {
			pod := rawObj.(*corev1.Pod)
			if pod.Spec.NodeName == "" {
				return nil
			}

			return []string{pod.Spec.NodeName}
		}); err != nil {
		return err
	}

	// Create a new indexed field on PVCs.
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&corev1.PersistentVolumeClaim{},
		pvcOwnerKey, func(rawObj client.Object) []string {
			persistentVolumeClaim := rawObj.(*corev1.PersistentVolumeClaim)

			if ownerName, ok := isOwnedByCluster(persistentVolumeClaim); ok {
				return []string{ownerName}
			}

			return nil
		}); err != nil {
		return err
	}

	// Create a new indexed field on Poolers. This field will be used to easily
	// find all the Poolers pointing to a cluster.
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&apiv1.Pooler{},
		poolerClusterKey, func(rawObj client.Object) []string {
			pooler := rawObj.(*apiv1.Pooler)
			if pooler.Spec.Cluster.Name == "" {
				return nil
			}

			return []string{pooler.Spec.Cluster.Name}
		}); err != nil {
		return err
	}

	// Create a new indexed field on Jobs.
	return mgr.GetFieldIndexer().IndexField(
		ctx,
		&batchv1.Job{},
		jobOwnerKey, func(rawObj client.Object) []string {
			job := rawObj.(*batchv1.Job)

			if ownerName, ok := isOwnedByCluster(job); ok {
				return []string{ownerName}
			}

			return nil
		})
}

// isOwnedByCluster checks that an object is owned by a Cluster and returns
// the owner name
func isOwnedByCluster(obj client.Object) (string, bool) {
	owner := metav1.GetControllerOf(obj)
	if owner == nil {
		return "", false
	}

	if owner.Kind != apiv1.ClusterKind {
		return "", false
	}

	if owner.APIVersion != apiGVString {
		return "", false
	}

	return owner.Name, true
}

// mapSecretsToClusters returns a function mapping cluster events watched to cluster reconcile requests
func (r *ClusterReconciler) mapSecretsToClusters(ctx context.Context) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}
		var clusters apiv1.ClusterList
		// get all the clusters handled by the operator in the secret namespaces
		err := r.List(ctx, &clusters,
			client.InNamespace(secret.Namespace),
		)
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting cluster list", "namespace", secret.Namespace)
			return nil
		}
		// build requests for cluster referring the secret
		return filterClustersUsingSecret(clusters, secret)
	}
}

// mapPoolersToClusters returns a function mapping pooler events watched to cluster reconcile requests
func (r *ClusterReconciler) mapPoolersToClusters(ctx context.Context) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		pooler, ok := obj.(*apiv1.Pooler)
		if !ok || pooler.Spec.Cluster.Name == "" {
			return nil
		}
		var cluster apiv1.Cluster
		clusterNamespacedName := types.NamespacedName{Namespace: pooler.Namespace, Name: pooler.Spec.Cluster.Name}
		// get all the clusters handled by the operator in the secret namespaces
		err := r.Get(ctx, clusterNamespacedName, &cluster)
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting cluster for pooler", "pooler", pooler)
			return nil
		}
		// build requests for cluster referring the secret
		return []reconcile.Request{{NamespacedName: clusterNamespacedName}}
	}
}

// mapNodeToClusters returns a function mapping cluster events watched to cluster reconcile requests
func (r *ClusterReconciler) mapConfigMapsToClusters(ctx context.Context) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		config, ok := obj.(*corev1.ConfigMap)
		if !ok {
			return nil
		}
		var clusters apiv1.ClusterList
		var err error
		const shouldFetchDefaultConfigMap = "false"

		if configuration.Current.MonitoringQueriesConfigmap != "" &&
			config.Namespace == configuration.Current.OperatorNamespace &&
			config.Name == configuration.Current.MonitoringQueriesConfigmap {
			// The events in MonitoringQueriesConfigmap impacts all the clusters.
			// We proceed to fetch all the clusters and create a reconciliation request for them
			// This works as long the replicated MonitoringQueriesConfigmap in the different namespaces
			// have the same name.
			//
			// See cluster.UsesConfigMap method
			err = r.List(
				ctx,
				&clusters,
				client.MatchingFields{disableDefaultQueriesSpecPath: shouldFetchDefaultConfigMap},
			)
		} else {
			// This is a configmap that affects only a given namespace, so we fetch only the clusters residing in there.
			err = r.List(
				ctx,
				&clusters,
				client.InNamespace(config.Namespace),
			)
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting cluster list", "namespace", config.Namespace)
			return nil
		}
		// build requests for clusters that refer the configmap
		return filterClustersUsingConfigMap(clusters, config)
	}
}

// filterClustersUsingConfigMap returns a list of reconcile.Request for the clusters
// that reference the secret
func filterClustersUsingSecret(
	clusters apiv1.ClusterList,
	secret *corev1.Secret,
) (requests []reconcile.Request) {
	for _, cluster := range clusters.Items {
		if cluster.UsesSecret(secret.Name) {
			requests = append(requests,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      cluster.Name,
						Namespace: cluster.Namespace,
					},
				},
			)
			continue
		}
	}
	return requests
}

// filterClustersUsingConfigMap returns a list of reconcile.Request for the clusters
// that reference the configMap
func filterClustersUsingConfigMap(
	clusters apiv1.ClusterList,
	config *corev1.ConfigMap,
) (requests []reconcile.Request) {
	for _, cluster := range clusters.Items {
		if cluster.UsesConfigMap(config.Name) {
			requests = append(requests,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      cluster.Name,
						Namespace: cluster.Namespace,
					},
				},
			)
			continue
		}
	}
	return requests
}

// mapNodeToClusters returns a function mapping cluster events watched to cluster reconcile requests
func (r *ClusterReconciler) mapNodeToClusters(ctx context.Context) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		node := obj.(*corev1.Node)
		// exit if the node is schedulable (e.g. not cordoned)
		// could be expanded here with other conditions (e.g. pressure or issues)
		if !node.Spec.Unschedulable {
			return nil
		}
		var childPods corev1.PodList
		// get all the pods handled by the operator on that node
		err := r.List(ctx, &childPods,
			client.MatchingFields{".spec.nodeName": node.Name},
			client.MatchingLabels{specs.ClusterRoleLabelName: specs.ClusterRoleLabelPrimary},
			client.HasLabels{specs.ClusterLabelName},
		)
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting primary instances for node")
			return nil
		}
		var requests []reconcile.Request
		// build requests for nodes the pods are running on
		for idx := range childPods.Items {
			if cluster, ok := isOwnedByCluster(&childPods.Items[idx]); ok {
				requests = append(requests,
					reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      cluster,
							Namespace: childPods.Items[idx].Namespace,
						},
					},
				)
			}
		}
		return requests
	}
}

func (r *ClusterReconciler) markPVCReadyForCompletedJobs(
	ctx context.Context,
	resources *managedResources,
) {
	contextLogger := log.FromContext(ctx)

	completeJobs := utils.FilterCompleteJobs(resources.jobs.Items)
	if len(completeJobs) == 0 {
		return
	}

	for _, job := range completeJobs {
		var pvcName string
		for _, pvc := range resources.pvcs.Items {
			if specs.IsJobOperatingOnPVC(job, pvc) {
				pvcName = pvc.Name
				break
			}
		}
		roleName := job.Labels[utils.JobRoleLabelName]
		if pvcName == "" {
			continue
		}

		// finding the PVC having the same name as pod
		pvc := resources.getPVC(pvcName)

		contextLogger.Info("job has been finished, setting PVC as ready", "pod", pvcName, "role", roleName)
		err := r.setPVCStatusReady(ctx, pvc)
		if err != nil {
			contextLogger.Error(err, "unable to annotate PVC as ready")
		}
	}
}
