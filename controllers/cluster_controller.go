/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package controllers contains the controller of the CRD
package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	goruntime "runtime"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/hibernation"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
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

	DiscoveryClient discovery.DiscoveryInterface
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder

	*instanceStatusClient
}

// NewClusterReconciler creates a new ClusterReconciler initializing it
func NewClusterReconciler(mgr manager.Manager, discoveryClient *discovery.DiscoveryClient) *ClusterReconciler {
	return &ClusterReconciler{
		instanceStatusClient: newInstanceStatusClient(),

		DiscoveryClient: discoveryClient,
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		Recorder:        mgr.GetEventRecorderFor("cloudnative-pg"),
	}
}

// ErrNextLoop see utils.ErrNextLoop
var ErrNextLoop = utils.ErrNextLoop

// Alphabetical order to not repeat or miss permissions
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;update;list;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;update;list;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;update;list
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;create;list;watch;delete;patch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;delete;get;list;watch;update;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/status,verbs=get;watch;update;patch
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

	cluster, err := r.getCluster(ctx, req)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cluster == nil {
		if err := r.deleteDanglingMonitoringQueries(ctx, req.Namespace); err != nil {
			contextLogger.Error(
				err,
				"error while deleting dangling monitoring configMap",
				"configMapName", apiv1.DefaultMonitoringConfigMapName,
				"namespace", req.Namespace,
			)
		}
		return ctrl.Result{}, err
	}

	// Run the inner reconcile loop. Translate any ErrNextLoop to an errorless return
	result, err := r.reconcile(ctx, cluster)
	if errors.Is(err, ErrNextLoop) {
		return result, nil
	}
	return result, err
}

// Inner reconcile loop. Anything inside can require the reconciliation loop to stop by returning ErrNextLoop
// nolint:gocognit
func (r *ClusterReconciler) reconcile(ctx context.Context, cluster *apiv1.Cluster) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if utils.IsReconciliationDisabled(&cluster.ObjectMeta) {
		contextLogger.Warning("Disable reconciliation loop annotation set, skipping the reconciliation.")
		return ctrl.Result{}, nil
	}

	// IMPORTANT: the following call will delete conditions using
	// invalid condition reasons.
	//
	// This operation is necessary to migrate from a version using
	// the customized Condition structure to one using the standard
	// one from K8S, that has more strict validations.
	//
	// The next reconciliation loop of the instance manager will
	// recreate the dropped conditions.
	err := r.removeConditionsWithInvalidReason(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Make sure default values are populated.
	err = r.setDefaults(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Ensure we reconcile the orphan resources if present when we reconcile for the first time a cluster
	if err := r.reconcileRestoredCluster(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot reconcile restored Cluster: %w", err)
	}

	// Ensure we have the required global objects
	if err := r.createPostgresClusterObjects(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot create Cluster auxiliary objects: %w", err)
	}

	// Update the status of this resource
	resources, err := r.getManagedResources(ctx, cluster)
	if err != nil {
		contextLogger.Error(err, "Cannot extract the list of managed resources")
		return ctrl.Result{}, err
	}

	// Update the status section of this Cluster resource
	if err = r.updateResourceStatus(ctx, cluster, resources); err != nil {
		if apierrs.IsConflict(err) {
			// Requeue a new reconciliation cycle, as in this point we need
			// to quickly react the changes
			contextLogger.Debug("Conflict error while reconciling resource status", "error", err)
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
	instancesStatus := r.instanceStatusClient.getStatusFromInstances(ctx, resources.instances)

	// we update all the cluster status fields that require the instances status
	if err := r.updateClusterStatusThatRequiresInstancesState(ctx, cluster, instancesStatus); err != nil {
		if apierrs.IsConflict(err) {
			contextLogger.Debug("Conflict error while reconciling cluster status nad instance state",
				"error", err)
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("cannot update the instances status on the cluster: %w", err)
	}

	// Verify the architecture of all the instances and update the OnlineUpdateEnabled
	// field in the status
	onlineUpdateEnabled := configuration.Current.EnableInstanceManagerInplaceUpdates
	isArchitectureConsistent := r.checkPodsArchitecture(ctx, &instancesStatus)
	if !isArchitectureConsistent && onlineUpdateEnabled {
		contextLogger.Info("Architecture mismatch detected, disabling instance manager online updates")
		onlineUpdateEnabled = false
	}

	// The instance list is sorted and will present the primary as the first
	// element, followed by the replicas, the most updated coming first.
	// Pods that are not responding will be at the end of the list. We use
	// the information reported by the instance manager to sort the
	// instances. When we need to elect a new primary, we take the first item
	// on this list.
	//
	// Here we check the readiness status of the first Pod as we can't
	// promote an instance that is not ready from the Kubernetes
	// point-of-view: the services will not forward traffic to it even if
	// PostgreSQL is up and running.
	//
	// An instance can be up and running even if the readiness probe is
	// negative: this is going to happen, i.e., when an instance is
	// un-fenced, and the Kubelet still hasn't refreshed the status of the
	// readiness probe.
	if instancesStatus.Len() > 0 {
		isPostgresReady := instancesStatus.Items[0].IsPostgresqlReady()
		isPodReady := instancesStatus.Items[0].IsPodReady

		if isPostgresReady && !isPodReady {
			// The readiness probe status from the Kubelet is not updated, so
			// we need to wait for it to be refreshed
			contextLogger.Info(
				"Waiting for the Kubelet to refresh the readiness probe",
				"instanceName", instancesStatus.Items[0].Node,
				"instanceStatus", instancesStatus.Items[0],
				"isPostgresReady", isPostgresReady,
				"isPodReady", isPodReady)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
	}

	// If the user has requested to hibernate the cluster, we do that before
	// ensuring the primary to be healthy. The hibernation starts from the
	// primary Pod to ensure the replicas are in sync and doing it here avoids
	// any unwanted switchover.
	if result, err := hibernation.Reconcile(
		ctx,
		r.Client,
		cluster,
		resources.instances.Items,
	); result != nil || err != nil {
		return *result, err
	}

	// We have already updated the status in updateResourceStatus call,
	// so we need to issue an extra update when the OnlineUpdateEnabled changes.
	// It's okay because it should not change often.
	//
	// We cannot merge this code with updateResourceStatus because
	// it needs to run after retrieving the status from the pods,
	// which is a time-expensive operation.
	if err = r.updateOnlineUpdateEnabled(ctx, cluster, onlineUpdateEnabled); err != nil {
		if apierrs.IsConflict(err) {
			// Requeue a new reconciliation cycle, as in this point we need
			// to quickly react the changes
			contextLogger.Debug("Conflict error while reconciling online update", "error", err)
			return ctrl.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, fmt.Errorf("cannot update the resource status: %w", err)
	}
	result, err := r.handleSwitchover(ctx, cluster, resources, instancesStatus)
	if err != nil {
		return ctrl.Result{}, err
	}
	if result != nil {
		return *result, nil
	}

	// Updates all the objects managed by the controller
	return r.reconcileResources(ctx, cluster, resources, instancesStatus)
}

func (r *ClusterReconciler) handleSwitchover(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
	instancesStatus postgres.PostgresqlStatusList,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)
	if cluster.IsInstanceFenced(cluster.Status.CurrentPrimary) ||
		instancesStatus.ReportingMightBeUnavailable(cluster.Status.CurrentPrimary) {
		contextLogger.Info("The current primary instance is fenced or is still recovering from it," +
			" we won't trigger a switchover")
		return nil, nil
	}
	if cluster.Status.Phase == apiv1.PhaseInplaceDeletePrimaryRestart {
		if cluster.Status.ReadyInstances != cluster.Spec.Instances {
			contextLogger.Info("Waiting for the primary to be restarted without triggering a switchover")
			return nil, nil
		}
		contextLogger.Info("All instances ready, will proceed",
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseHealthy, ""); err != nil {
			return nil, err
		}
		return nil, nil
	}

	// Update the target primary name from the Pods status.
	// This means issuing a failover or switchover when needed.
	selectedPrimary, err := r.updateTargetPrimaryFromPods(ctx, cluster, instancesStatus, resources)
	if err != nil {
		if err == ErrWaitingOnFailOverDelay {
			contextLogger.Info("Waiting for the failover delay to expire")
			return &ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
		if err == ErrWalReceiversRunning {
			contextLogger.Info("Waiting for all WAL receivers to be down to elect a new primary")
			return &ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
		contextLogger.Info("Cannot update target primary: operation cannot be fulfilled. "+
			"An immediate retry will be scheduled",
			"cluster", cluster.Name)
		return &ctrl.Result{Requeue: true}, nil
	}
	if selectedPrimary != "" {
		// If we selected a new primary, stop the reconciliation loop here
		contextLogger.Info("Waiting for the new primary to notice the promotion request",
			"newPrimary", selectedPrimary)
		return &ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Primary is healthy, No switchover in progress.
	// If we have a currentPrimaryFailingSince timestamp, let's unset it.
	if cluster.Status.CurrentPrimaryFailingSinceTimestamp != "" {
		cluster.Status.CurrentPrimaryFailingSinceTimestamp = ""
		if err := r.Status().Update(ctx, cluster); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

func (r *ClusterReconciler) getCluster(
	ctx context.Context,
	req ctrl.Request,
) (*apiv1.Cluster, error) {
	contextLogger := log.FromContext(ctx)
	cluster := &apiv1.Cluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		// This also happens when you delete a Cluster resource in k8s. If
		// that's the case, let's just wait for the Kubernetes garbage collector
		// to remove all the Pods of the cluster.
		if apierrs.IsNotFound(err) {
			contextLogger.Info("Resource has been deleted")
			return nil, nil
		}

		// This is a real error, maybe the RBAC configuration is wrong?
		return nil, fmt.Errorf("cannot get the managed resource: %w", err)
	}

	var namespace corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Namespace: "", Name: req.Namespace}, &namespace); err != nil {
		// This is a real error, maybe the RBAC configuration is wrong?
		return nil, fmt.Errorf("cannot get the containing namespace: %w", err)
	}

	if !namespace.DeletionTimestamp.IsZero() {
		// This happens when you delete a namespace containing a Cluster resource. If that's the case,
		// let's just wait for the Kubernetes to remove all object in the namespace.
		return nil, nil
	}

	return cluster, nil
}

func (r *ClusterReconciler) setDefaults(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)
	originCluster := cluster.DeepCopy()
	cluster.SetDefaults()
	if !reflect.DeepEqual(originCluster.Spec, cluster.Spec) {
		contextLogger.Info("Admission controllers (webhooks) appear to have been disabled. " +
			"Please enable them for this object/namespace")
		err := r.Patch(ctx, cluster, client.MergeFrom(originCluster))
		if err != nil {
			return err
		}
	}
	return nil
}

// reconcileResources updates all the objects managed by the controller
func (r *ClusterReconciler) reconcileResources(
	ctx context.Context, cluster *apiv1.Cluster,
	resources *managedResources, instancesStatus postgres.PostgresqlStatusList,
) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	// TODO: refactor how we handle label and annotation reconciliation.
	// TODO: We should generate a fake pod containing the expected labels and annotations and compare it to the living pod

	// Update the labels for the -rw service to work correctly
	if err := r.updateRoleLabelsOnPods(ctx, cluster, resources.instances); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update role labels on pods: %w", err)
	}

	// updated any labels that are coming from the operator
	if err := r.updateOperatorLabelsOnInstances(ctx, resources.instances); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update instance labels on pods: %w", err)
	}

	// Update any modified/new labels coming from the cluster resource
	if err := r.updateClusterLabelsOnPods(ctx, cluster, resources.instances); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update cluster labels on pods: %w", err)
	}

	// Update any modified/new annotations coming from the cluster resource
	if err := r.updateClusterAnnotationsOnPods(ctx, cluster, resources.instances); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update annotations on pods: %w", err)
	}

	// Act on Pods and PVCs only if there is nothing that is currently being created or deleted
	if runningJobs := resources.countRunningJobs(); runningJobs > 0 {
		contextLogger.Debug("A job is currently running. Waiting", "count", runningJobs)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Delete Pods which have been evicted by the Kubelet
	result, err := r.deleteEvictedOrUnscheduledInstances(ctx, cluster, resources)
	if err != nil {
		contextLogger.Error(err, "While deleting evicted pods")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	if result != nil {
		return *result, err
	}

	// TODO: move into a central waiting phase
	// If we are joining a node, we should wait for the process to finish
	if resources.countRunningJobs() > 0 {
		contextLogger.Debug("Waiting for jobs to finish",
			"clusterName", cluster.Name,
			"namespace", cluster.Namespace,
			"jobs", len(resources.jobs.Items))
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	if !resources.allInstancesAreActive() {
		contextLogger.Debug("A managed resource is currently being created or deleted. Waiting")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	if res, err := persistentvolumeclaim.Reconcile(
		ctx,
		r.Client,
		cluster,
		resources.instances.Items,
		resources.pvcs.Items,
	); !res.IsZero() || err != nil {
		return res, err
	}

	// Reconcile Pods
	if res, err := r.ReconcilePods(ctx, cluster, resources, instancesStatus); err != nil {
		return res, err
	}

	if len(resources.instances.Items) > 0 && resources.noInstanceIsAlive() {
		return ctrl.Result{RequeueAfter: 1 * time.Second}, r.RegisterPhase(ctx, cluster, apiv1.PhaseUnrecoverable,
			"No pods are active, the cluster needs manual intervention ")
	}

	// If we still need more instances, we need to wait before setting healthy status
	if instancesStatus.InstancesReportingStatus() != cluster.Spec.Instances {
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	// When everything is reconciled, update the status
	if err = r.RegisterPhase(ctx, cluster, apiv1.PhaseHealthy, ""); err != nil {
		return ctrl.Result{}, err
	}

	r.cleanupCompletedJobs(ctx, resources.jobs)

	return ctrl.Result{}, nil
}

// deleteEvictedOrUnscheduledInstances will delete the Pods that the Kubelet has evicted or cannot schedule
func (r *ClusterReconciler) deleteEvictedOrUnscheduledInstances(ctx context.Context, cluster *apiv1.Cluster,
	resources *managedResources,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)
	deletedPods := false

	for idx := range resources.instances.Items {
		instance := &resources.instances.Items[idx]

		// we process unscheduled pod only if we are in IsNodeMaintenanceWindow, and we can delete the PVC Group
		// This will be better handled in a next patch
		if !utils.IsPodEvicted(instance) && !(utils.IsPodUnscheduled(instance) &&
			cluster.IsNodeMaintenanceWindowInProgress() &&
			!cluster.IsReusePVCEnabled()) {
			continue
		}
		contextLogger.Warning("Deleting evicted/unscheduled pod",
			"pod", instance.Name,
			"podStatus", instance.Status)
		if err := r.Delete(ctx, instance); err != nil {
			if apierrs.IsConflict(err) {
				contextLogger.Debug("Conflict error while deleting instances item", "error", err)
				return &ctrl.Result{Requeue: true}, nil
			}
			return nil, err
		}
		deletedPods = true

		r.Recorder.Eventf(cluster, "Normal", "DeletePod",
			"Deleted evicted/unscheduled Pod %v",
			instance.Name)

		// we never delete the pvc unless we are in node Maintenance Window and the Reuse PVC is false
		if !cluster.IsNodeMaintenanceWindowInProgress() || cluster.IsReusePVCEnabled() {
			continue
		}

		if err := persistentvolumeclaim.EnsureInstancePVCGroupIsDeleted(
			ctx,
			r.Client,
			cluster,
			instance.Name,
			instance.Namespace,
		); err != nil {
			return nil, err
		}
		r.Recorder.Eventf(cluster, "Normal", "DeletePVCs",
			"Deleted evicted/unscheduled Pod %v PVCs",
			instance.Name)
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
		// Ignore architecture in podStatus with errors
		if podStatus.Error != nil {
			continue
		}

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

// ReconcilePods decides when to create, scale up/down or wait for pods
func (r *ClusterReconciler) ReconcilePods(ctx context.Context, cluster *apiv1.Cluster,
	resources *managedResources, instancesStatus postgres.PostgresqlStatusList,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if err := r.markPVCReadyForCompletedJobs(ctx, resources); err != nil {
		return ctrl.Result{}, err
	}

	if res, err := r.ensureInstancesAreCreated(ctx, cluster, resources, instancesStatus); !res.IsZero() || err != nil {
		return res, err
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
	// The user have chosen to wait for the missing nodes to come up
	if !(cluster.IsNodeMaintenanceWindowInProgress() && cluster.IsReusePVCEnabled()) &&
		instancesStatus.InstancesReportingStatus() < cluster.Status.Instances {
		contextLogger.Debug("Waiting for Pods to be ready")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	// Are there missing nodes? Let's create one
	if cluster.Status.Instances < cluster.Spec.Instances &&
		instancesStatus.InstancesReportingStatus() == cluster.Status.Instances {
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
		cluster.Status.ReadyInstances != len(instancesStatus.Items) ||
		!instancesStatus.IsComplete() {
		contextLogger.Debug("Waiting for Pods to be ready")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	return r.handleRollingUpdate(ctx, cluster, instancesStatus)
}

func (r *ClusterReconciler) ensureHealthyPVCsAnnotation(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) error {
	contextLogger := log.FromContext(ctx)

	// Make sure that all healthy PVCs are marked as ready
	for _, pvcName := range cluster.Status.HealthyPVC {
		pvc := resources.getPVC(pvcName)
		if pvc == nil {
			return fmt.Errorf(
				"could not find the pvc: %s, from the list of managed pvc",
				pvcName,
			)
		}

		if pvc.Annotations[persistentvolumeclaim.StatusAnnotationName] == persistentvolumeclaim.StatusReady {
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

func (r *ClusterReconciler) handleRollingUpdate(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instancesStatus postgres.PostgresqlStatusList,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	// If we need to roll out a restart of any instance, this is the right moment
	// Do I have to roll out a new image?
	done, err := r.rolloutDueToCondition(ctx, cluster, &instancesStatus, IsPodNeedingRollout)
	if err != nil {
		return ctrl.Result{}, err
	}
	if done {
		// Rolling upgrade is in progress, let's avoid marking stuff as synchronized
		return ctrl.Result{}, ErrNextLoop
	}

	if instancesStatus.ArePodsWaitingForDecreasedSettings() {
		// requeue and wait for the pods to be ready to be restarted,
		// which will be handled by rolloutDueToCondition
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	// Stop acting here if there are Pods that are waiting for
	// an instance manager upgrade
	if instancesStatus.ArePodsUpgradingInstanceManager() {
		contextLogger.Debug("Waiting for Pods to complete instance manager upgrade")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	// Execute online update, if enabled and if not already executing
	if cluster.Status.OnlineUpdateEnabled && cluster.Status.Phase != apiv1.PhaseOnlineUpgrading {
		if err := r.upgradeInstanceManager(ctx, cluster, &instancesStatus); err != nil {
			return ctrl.Result{}, err
		}
		// Stop the reconciliation loop if upgradeInstanceManager initiated an upgrade
		if cluster.Status.Phase == apiv1.PhaseOnlineUpgrading {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, ErrNextLoop
		}
	}

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
		Owns(&policyv1.PodDisruptionBudget{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigMapsToClusters()),
			builder.WithPredicates(configMapsPredicate),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretsToClusters()),
			builder.WithPredicates(secretsPredicate),
		).
		Watches(
			&apiv1.Pooler{},
			handler.EnqueueRequestsFromMapFunc(r.mapPoolersToClusters()),
		).
		Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.mapNodeToClusters()),
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

			if ownerName, ok := IsOwnedByCluster(pod); ok {
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

			if ownerName, ok := IsOwnedByCluster(persistentVolumeClaim); ok {
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

			if ownerName, ok := IsOwnedByCluster(job); ok {
				return []string{ownerName}
			}

			return nil
		})
}

// IsOwnedByCluster checks that an object is owned by a Cluster and returns
// the owner name
func IsOwnedByCluster(obj client.Object) (string, bool) {
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
func (r *ClusterReconciler) mapSecretsToClusters() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}
		clusters, err := r.getClustersForSecretsOrConfigMapsToClustersMapper(ctx, secret)
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting cluster list", "namespace", secret.Namespace)
			return nil
		}
		// build requests for cluster referring the secret
		return filterClustersUsingSecret(clusters, secret)
	}
}

func (r *ClusterReconciler) getClustersForSecretsOrConfigMapsToClustersMapper(
	ctx context.Context,
	object metav1.Object,
) (clusters apiv1.ClusterList, err error) {
	_, isSecret := object.(*corev1.Secret)
	_, isConfigMap := object.(*corev1.ConfigMap)

	if !isSecret && !isConfigMap {
		return clusters, fmt.Errorf("unsupported object: %+v", object)
	}

	// Get all the clusters handled by the operator in the secret namespaces
	if object.GetNamespace() == configuration.Current.OperatorNamespace &&
		((isConfigMap && object.GetName() == configuration.Current.MonitoringQueriesConfigmap) ||
			(isSecret && object.GetName() == configuration.Current.MonitoringQueriesSecret)) {
		// The events in MonitoringQueriesSecrets impacts all the clusters.
		// We proceed to fetch all the clusters and create a reconciliation request for them.
		// This works as long as the replicated MonitoringQueriesConfigmap in the different namespaces
		// have the same name.
		//
		// See cluster.UsesSecret method
		err = r.List(
			ctx,
			&clusters,
			client.MatchingFields{disableDefaultQueriesSpecPath: "false"},
		)
	} else {
		// This is a configmap that affects only a given namespace, so we fetch only the clusters residing in there.
		err = r.List(
			ctx,
			&clusters,
			client.InNamespace(object.GetNamespace()),
		)
	}
	return clusters, err
}

// mapPoolersToClusters returns a function mapping pooler events watched to cluster reconcile requests
func (r *ClusterReconciler) mapPoolersToClusters() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
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
func (r *ClusterReconciler) mapConfigMapsToClusters() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		config, ok := obj.(*corev1.ConfigMap)
		if !ok {
			return nil
		}
		clusters, err := r.getClustersForSecretsOrConfigMapsToClustersMapper(ctx, config)
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
func (r *ClusterReconciler) mapNodeToClusters() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
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
			client.MatchingLabels{
				specs.ClusterRoleLabelName: specs.ClusterRoleLabelPrimary,
				utils.PodRoleLabelName:     string(utils.PodRoleInstance),
			},
		)
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting primary instances for node")
			return nil
		}
		var requests []reconcile.Request
		// build requests for nodes the pods are running on
		for idx := range childPods.Items {
			if cluster, ok := IsOwnedByCluster(&childPods.Items[idx]); ok {
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
) error {
	contextLogger := log.FromContext(ctx)

	completeJobs := utils.FilterJobsWithOneCompletion(resources.jobs.Items)
	if len(completeJobs) == 0 {
		return nil
	}

	for _, job := range completeJobs {
		for _, pvc := range resources.pvcs.Items {
			pvc := pvc
			if !persistentvolumeclaim.IsUsedByPodSpec(job.Spec.Template.Spec, pvc.Name) {
				continue
			}

			roleName := job.Labels[utils.JobRoleLabelName]
			contextLogger.Info("job has been finished, setting PVC as ready",
				"pvcName", pvc.Name,
				"role", roleName,
			)

			if err := r.setPVCStatusReady(ctx, &pvc); err != nil {
				contextLogger.Error(err, "unable to annotate PVC as ready")
				return err
			}
		}
	}

	return nil
}

// TODO: only required to cleanup custom monitoring queries configmaps from older versions (v1.10 and v1.11)
// that could have been copied with the source configmap name instead of the new default one.
// Should be removed in future releases.
func (r *ClusterReconciler) deleteOldCustomQueriesConfigmap(ctx context.Context, cluster *apiv1.Cluster) {
	contextLogger := log.FromContext(ctx)

	// if the cluster didn't have default monitoring queries, do nothing
	if cluster.Spec.Monitoring.AreDefaultQueriesDisabled() ||
		configuration.Current.MonitoringQueriesConfigmap == "" ||
		configuration.Current.MonitoringQueriesConfigmap == apiv1.DefaultMonitoringConfigMapName {
		return
	}

	// otherwise, remove the old default monitoring queries configmap from the cluster and delete it, if present
	oldCmID := -1
	for idx, cm := range cluster.Spec.Monitoring.CustomQueriesConfigMap {
		if cm.Name == configuration.Current.MonitoringQueriesConfigmap &&
			cm.Key == apiv1.DefaultMonitoringKey {
			oldCmID = idx
			break
		}
	}

	// if we didn't find it, do nothing
	if oldCmID < 0 {
		return
	}

	// if we found it, we are going to get it and check it was actually created by the operator or was already deleted
	var oldCm corev1.ConfigMap
	err := r.Get(ctx, types.NamespacedName{
		Name:      configuration.Current.MonitoringQueriesConfigmap,
		Namespace: cluster.Namespace,
	}, &oldCm)
	// if we found it, we check the annotation the operator should have set to be sure it was created by us
	if err == nil { // nolint:nestif
		// if it was, we delete it and proceed to remove it from the cluster monitoring spec
		if _, ok := oldCm.Annotations[utils.OperatorVersionAnnotationName]; ok {
			err = r.Delete(ctx, &oldCm)
			// if there is any error except the cm was already deleted, we return
			if err != nil && !apierrs.IsNotFound(err) {
				contextLogger.Warning("error while deleting old default monitoring custom queries configmap",
					"err", err,
					"configmap", configuration.Current.MonitoringQueriesConfigmap)
				return
			}
		} else {
			// it exists, but it's not handled by the operator, we do nothing
			contextLogger.Warning("A configmap with the same name as the old default monitoring queries "+
				"configmap exists, but doesn't have the required annotation, so it won't be deleted, "+
				"nor removed from the cluster monitoring spec",
				"configmap", oldCm.Name)
			return
		}
	} else if !apierrs.IsNotFound(err) {
		// if there is any error except the cm was already deleted, we return
		contextLogger.Warning("error while getting old default monitoring custom queries configmap",
			"err", err,
			"configmap", configuration.Current.MonitoringQueriesConfigmap)
		return
	}
	// both if it exists or not, if we are here we should delete it from the list of custom queries configmaps
	oldCluster := cluster.DeepCopy()
	cluster.Spec.Monitoring.CustomQueriesConfigMap = append(cluster.Spec.Monitoring.CustomQueriesConfigMap[:oldCmID],
		cluster.Spec.Monitoring.CustomQueriesConfigMap[oldCmID+1:]...)
	err = r.Patch(ctx, cluster, client.MergeFrom(oldCluster))
	if err != nil {
		log.Warning("had an error while removing the old custom monitoring queries configmap from "+
			"the monitoring section in the cluster",
			"err", err,
			"configmap", configuration.Current.MonitoringQueriesConfigmap)
	}
}
