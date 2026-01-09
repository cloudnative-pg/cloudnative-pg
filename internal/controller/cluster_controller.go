/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

// Package controller contains the controller of the CRD
package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
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
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgiClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/operatorclient"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	rolloutManager "github.com/cloudnative-pg/cloudnative-pg/internal/controller/rollout"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/hibernation"
	instanceReconciler "github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/instance"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/majorupgrade"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/replicaclusterswitch"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	podOwnerKey                   = ".metadata.controller"
	pvcOwnerKey                   = ".metadata.controller"
	jobOwnerKey                   = ".metadata.controller"
	poolerClusterKey              = ".spec.cluster.name"
	disableDefaultQueriesSpecPath = ".spec.monitoring.disableDefaultQueries"
	imageCatalogKey               = ".spec.imageCatalog.name"
)

var apiSGVString = apiv1.SchemeGroupVersion.String()

// errOldPrimaryDetected occurs when a primary Pod loses connectivity with the
// API server and, upon reconnection, attempts to retain its previous primary
// role.
var errOldPrimaryDetected = errors.New("old primary detected")

// ClusterReconciler reconciles a Cluster objects
type ClusterReconciler struct {
	client.Client

	DiscoveryClient discovery.DiscoveryInterface
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	InstanceClient  remote.InstanceClient
	Plugins         repository.Interface

	drainTaints    []string
	rolloutManager *rolloutManager.Manager
}

// NewClusterReconciler creates a new ClusterReconciler initializing it
func NewClusterReconciler(
	mgr manager.Manager,
	discoveryClient *discovery.DiscoveryClient,
	plugins repository.Interface,
	drainTaints []string,
) *ClusterReconciler {
	return &ClusterReconciler{
		InstanceClient:  remote.NewClient().Instance(),
		DiscoveryClient: discoveryClient,
		Client:          operatorclient.NewExtendedClient(mgr.GetClient()),
		Scheme:          mgr.GetScheme(),
		Recorder:        mgr.GetEventRecorderFor("cloudnative-pg"),
		Plugins:         plugins,
		rolloutManager: rolloutManager.New(
			configuration.Current.GetClustersRolloutDelay(),
			configuration.Current.GetInstancesRolloutDelay(),
		),
		drainTaints: drainTaints,
	}
}

// ErrNextLoop see utils.ErrNextLoop
var ErrNextLoop = utils.ErrNextLoop

// Alphabetical order to not repeat or miss permissions
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;patch
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
// +kubebuilder:rbac:groups="",resources=events,verbs=create
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;create;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;list;get;watch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=create;patch;update;list;watch;get
// +kubebuilder:rbac:groups="",resources=services,verbs=get;create;delete;update;patch;list;watch
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;create;watch;list;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=imagecatalogs,verbs=get;watch;list
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusterimagecatalogs,verbs=get;watch;list
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=failoverquorums,verbs=create;get;watch;delete;list
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=failoverquorums/status,verbs=get;patch;update;watch

// Reconcile is the operator reconcile loop
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug("Reconciliation loop start")
	defer func() {
		contextLogger.Debug("Reconciliation loop end")
	}()

	cluster, err := r.getCluster(ctx, req)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cluster == nil || cluster.GetDeletionTimestamp() != nil {
		if err := r.deleteDanglingMonitoringQueries(ctx, req.Namespace); err != nil {
			contextLogger.Error(
				err,
				"error while deleting dangling monitoring configMap",
				"configMapName", apiv1.DefaultMonitoringConfigMapName,
				"namespace", req.Namespace,
			)
			return ctrl.Result{}, err
		}
		if err := r.notifyDeletionToOwnedResources(ctx, req.NamespacedName); err != nil {
			// Optimistic locking conflict is transient - requeue to retry
			if apierrs.IsConflict(err) {
				contextLogger.Info(
					"Optimistic locking conflict while removing finalizers, requeueing",
					"clusterName", req.Name,
					"namespace", req.Namespace,
				)
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
			contextLogger.Error(
				err,
				"error while deleting finalizers of objects on the cluster",
				"clusterName", req.Name,
				"namespace", req.Namespace,
			)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	ctx = cluster.SetInContext(ctx)

	// Load the plugins required to bootstrap and reconcile this cluster
	enabledPluginNames := apiv1.GetPluginConfigurationEnabledPluginNames(cluster.Spec.Plugins)
	enabledPluginNames = append(
		enabledPluginNames,
		apiv1.GetExternalClustersEnabledPluginNames(cluster.Spec.ExternalClusters)...,
	)

	pluginLoadingContext, cancelPluginLoading := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPluginLoading()

	pluginClient, err := cnpgiClient.WithPlugins(pluginLoadingContext, r.Plugins, enabledPluginNames...)
	if err != nil {
		var errUnknownPlugin *repository.ErrUnknownPlugin
		if errors.As(err, &errUnknownPlugin) {
			return ctrl.Result{
					RequeueAfter: 10 * time.Second,
				}, r.RegisterPhase(
					ctx,
					cluster,
					apiv1.PhaseUnknownPlugin,
					fmt.Sprintf("Unknown plugin: '%s'. "+
						"This may be caused by the plugin not being loaded correctly by the operator. "+
						"Check the operator and plugin logs for errors", errUnknownPlugin.Name),
				)
		}

		if regErr := r.RegisterPhase(
			ctx,
			cluster,
			apiv1.PhaseFailurePlugin,
			fmt.Sprintf("Error while discovering plugins: %s", err.Error()),
		); regErr != nil {
			contextLogger.Error(regErr, "unable to register phase", "outerErr", err.Error())
		}

		contextLogger.Error(err, "Error loading plugins, retrying")
		return ctrl.Result{}, err
	}
	defer func() {
		pluginClient.Close(ctx)
	}()

	ctx = cnpgiClient.SetPluginClientInContext(ctx, pluginClient)

	// Run the inner reconcile loop. Translate any ErrNextLoop to an errorless return
	result, err := r.reconcile(ctx, cluster)
	if errors.Is(err, ErrNextLoop) {
		return result, nil
	}
	if errors.Is(err, utils.ErrTerminateLoop) {
		return ctrl.Result{}, nil
	}

	// This code assumes that we always end the reconciliation loop if we encounter an error.
	// In case that the assumption is false this code could overwrite an error phase.
	if cnpgiClient.ContainsPluginError(err) {
		if regErr := r.RegisterPhase(
			ctx,
			cluster,
			apiv1.PhaseFailurePlugin,
			fmt.Sprintf("Encountered an error while interacting with plugins: %s", err.Error()),
		); regErr != nil {
			contextLogger.Error(regErr, "unable to register phase", "outerErr", err.Error())
		}
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	if err != nil {
		return ctrl.Result{}, err
	}
	return result, nil
}

// Inner reconcile loop. Anything inside can require the reconciliation loop to stop by returning ErrNextLoop
// nolint:gocognit,gocyclo
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

	// Discover the image to be used and set it into the status
	if result, err := r.reconcileImage(ctx, cluster); result != nil || err != nil {
		if result != nil {
			return *result, err
		}

		return ctrl.Result{}, fmt.Errorf("cannot set image name: %w", err)
	}

	// Ensure we load all the plugins that are required to reconcile this cluster
	if err := r.updatePluginsStatus(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot reconcile required plugins: %w", err)
	}

	// Ensure we reconcile the orphan resources if present when we reconcile for the first time a cluster
	if res, err := r.reconcileRestoredCluster(ctx, cluster); res != nil || err != nil {
		if res != nil {
			return *res, nil
		}
		return ctrl.Result{}, fmt.Errorf("cannot reconcile restored Cluster: %w", err)
	}

	// Ensure we have the required global objects
	if err := r.createPostgresClusterObjects(ctx, cluster); err != nil {
		if errors.Is(err, ErrNextLoop) {
			return ctrl.Result{}, err
		}
		contextLogger.Error(err, "while reconciling postgres cluster objects")
		if regErr := r.RegisterPhase(ctx, cluster, apiv1.PhaseCannotCreateClusterObjects, err.Error()); regErr != nil {
			contextLogger.Error(regErr, "unable to register phase", "outerErr", err.Error())
		}
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

	// Calls pre-reconcile hooks
	if hookResult := preReconcilePluginHooks(ctx, cluster, cluster); hookResult.StopReconciliation {
		contextLogger.Info("Pre-reconcile hook stopped the reconciliation loop",
			"hookResult", hookResult)
		return hookResult.Result, hookResult.Err
	}

	if cluster.Status.CurrentPrimary != "" &&
		cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		contextLogger.Info("There is a switchover or a failover "+
			"in progress, waiting for the operation to complete",
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary)

		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	if cluster.ShouldPromoteFromReplicaCluster() {
		if !(cluster.Status.Phase == apiv1.PhaseReplicaClusterPromotion ||
			cluster.Status.Phase == apiv1.PhaseUnrecoverable) {
			if err := r.RegisterPhase(ctx,
				cluster,
				apiv1.PhaseReplicaClusterPromotion,
				"Replica cluster promotion in progress"); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Store in the context the TLS configuration required communicating with the Pods
	ctx, err = certs.NewTLSConfigForContext(
		ctx,
		r.Client,
		cluster.GetServerCASecretObjectKey(),
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Get the replication status
	instancesStatus := r.InstanceClient.GetStatusFromInstances(ctx, resources.instances)

	// we update all the cluster status fields that require the instances status
	if err := r.updateClusterStatusThatRequiresInstancesState(ctx, cluster, instancesStatus); err != nil {
		if apierrs.IsConflict(err) {
			contextLogger.Debug("Conflict error while reconciling cluster status and instance state",
				"error", err)
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("cannot update the instances status on the cluster: %w", err)
	}

	// If a Pod loses connectivity, the operator will fail over but the faulty
	// Pod would not receive a change of its role from primary to replica.
	//
	// When the connectivity resumes the operator will find two primaries:
	// the previously faulting one and the new primary that has been
	// promoted. The operator should just wait for the Pods to get its
	// current role from auto-healing to proceed. Without this safety
	// measure, the operator would just fail back to the first primary of
	// the list.
	if primaryNames := instancesStatus.PrimaryNames(); len(primaryNames) > 1 {
		contextLogger.Error(
			errOldPrimaryDetected,
			"An old primary pod has been detected. Awaiting its recognition of the new role",
			"primaryNames", primaryNames,
		)
		instancesStatus.LogStatus(ctx)
		return ctrl.Result{
			RequeueAfter: 5 * time.Second,
		}, nil
	}

	if err := persistentvolumeclaim.ReconcileMetadata(
		ctx,
		r.Client,
		cluster,
		resources.pvcs.Items,
	); err != nil {
		return ctrl.Result{}, err
	}

	if err := instanceReconciler.ReconcileMetadata(
		ctx,
		r.Client,
		cluster,
		resources.instances.Items,
	); err != nil {
		return ctrl.Result{}, err
	}

	if err := persistentvolumeclaim.ReconcileSerialAnnotation(
		ctx,
		r.Client,
		cluster,
		resources.instances.Items,
		resources.pvcs.Items,
	); err != nil {
		return ctrl.Result{}, err
	}

	if instancesStatus.AllReadyInstancesStatusUnreachable() {
		contextLogger.Warning(
			"Failed to extract instance status from ready instances. Attempting to requeue...",
		)
		if err := r.RegisterPhase(
			ctx,
			cluster,
			"Instance Status Extraction Error: HTTP communication issue",
			"Communication issue detected: The operator was unable to receive the status from all the ready instances. "+
				"This may be due to network restrictions such as NetworkPolicy and/or any other network plugin setting. "+
				"Please verify your network configuration.",
		); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if res, err := r.ensureNoFailoverOnFullDisk(ctx, cluster, instancesStatus); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := r.requireWALArchivingPluginOrDelete(ctx, instancesStatus); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := replicaclusterswitch.Reconcile(
		ctx, r.Client, cluster, r.InstanceClient, instancesStatus); res != nil || err != nil {
		if res != nil {
			return *res, nil
		}
		return ctrl.Result{}, err
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
		mostAdvancedInstance := instancesStatus.Items[0]
		hasHTTPStatus := mostAdvancedInstance.HasHTTPStatus()
		isPodReady := mostAdvancedInstance.IsPodReady

		if hasHTTPStatus && !isPodReady {
			// The readiness probe status from the Kubelet is not updated, so
			// we need to wait for it to be refreshed
			contextLogger.Info(
				"Waiting for the Kubelet to refresh the readiness probe",
				"mostAdvancedInstanceName", mostAdvancedInstance.Pod.Name,
				"hasHTTPStatus", hasHTTPStatus,
				"isPodReady", isPodReady)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
	}

	// If the user has requested to hibernate the cluster, we do that before
	// ensuring the primary to be healthy. The hibernation starts from the
	// primary Pod to ensure the replicas are in sync and doing it here avoids
	// any unwanted switchover.
	hibernationResult, err := hibernation.Reconcile(
		ctx,
		r.Client,
		cluster,
		resources.instances.Items,
	)
	if hibernationResult != nil {
		return *hibernationResult, err
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// We have already updated the status in updateResourceStatus call,
	// so we need to issue an extra update when the OnlineUpdateEnabled changes.
	// It's okay because it should not change often.
	//
	// We cannot merge this code with updateResourceStatus because
	// it needs to run after retrieving the status from the pods,
	// which is a time-expensive operation.
	onlineUpdateEnabled := configuration.Current.EnableInstanceManagerInplaceUpdates
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
	res, err := r.reconcileResources(ctx, cluster, resources, instancesStatus)
	if err != nil || !res.IsZero() {
		return res, err
	}

	// Calls post-reconcile hooks
	if hookResult := postReconcilePluginHooks(ctx, cluster, cluster); hookResult.Err != nil ||
		!hookResult.Result.IsZero() {
		contextLogger.Info("Post-reconcile hook stopped the reconciliation loop",
			"hookResult", hookResult)
		return hookResult.Result, hookResult.Err
	}

	return setStatusPluginHook(ctx, r.Client, cnpgiClient.GetPluginClientFromContext(ctx), cluster)
}

func (r *ClusterReconciler) ensureNoFailoverOnFullDisk(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instances postgres.PostgresqlStatusList,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).WithName("ensure_sufficient_disk_space")

	var instanceNames []string
	for _, state := range instances.Items {
		if !isWALSpaceAvailableOnPod(state.Pod) {
			instanceNames = append(instanceNames, state.Pod.Name)
		}
	}
	if len(instanceNames) == 0 {
		return ctrl.Result{}, nil
	}

	contextLogger = contextLogger.WithValues("instanceNames", instanceNames)
	contextLogger.Warning(
		"Insufficient disk space detected in a pod. PostgreSQL cannot proceed until the PVC group is enlarged",
	)

	reason := "Insufficient disk space detected in one or more pods is preventing PostgreSQL from running." +
		"Please verify your storage settings. Further information inside .status.instancesReportedState"
	if err := r.RegisterPhase(
		ctx,
		cluster,
		"Not enough disk space",
		reason,
	); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *ClusterReconciler) requireWALArchivingPluginOrDelete(
	ctx context.Context,
	instances postgres.PostgresqlStatusList,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).WithName("require_wal_archiving_plugin_delete")

	for _, state := range instances.Items {
		if isTerminatedBecauseOfMissingWALArchivePlugin(state.Pod) {
			contextLogger.Warning(
				"Detected instance manager initialization procedure that failed "+
					"because the required WAL archive plugin is missing. Deleting it to trigger rollout",
				"targetPod", state.Pod.Name)
			if err := r.Delete(ctx, state.Pod); err != nil {
				contextLogger.Error(err, "Cannot delete the pod", "pod", state.Pod.Name)
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	return ctrl.Result{}, nil
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
	selectedPrimary, err := r.reconcileTargetPrimaryFromPods(ctx, cluster, instancesStatus, resources)
	if err != nil {
		if errors.Is(err, ErrWaitingOnFailOverDelay) {
			contextLogger.Info("Waiting for the failover delay to expire")
			return &ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
		if errors.Is(err, ErrWalReceiversRunning) {
			contextLogger.Info("Waiting for all WAL receivers to be down to elect a new primary")
			return &ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
		contextLogger.Info("Cannot update target primary: operation cannot be fulfilled. "+
			"An immediate retry will be scheduled",
			"error", err)
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
	contextLogger := log.FromContext(ctx)
	runningJobs := resources.runningJobNames()

	// Act on Pods and PVCs only if there is nothing that is currently being created or deleted

	if len(runningJobs) > 0 {
		contextLogger.Debug("A job is currently running. Waiting", "runningJobs", runningJobs)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if result, err := r.deleteTerminatedPods(ctx, cluster, resources); err != nil {
		contextLogger.Error(err, "While deleting terminated pods")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	} else if result != nil {
		return *result, nil
	}

	if result, err := r.processUnschedulableInstances(ctx, cluster, resources); err != nil {
		contextLogger.Error(err, "While processing unschedulable instances")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	} else if result != nil {
		return *result, err
	}

	if !resources.allInstancesAreActive() {
		contextLogger = contextLogger.WithValues(
			"inactiveInstances", resources.inactiveInstanceNames())

		// Preserve phases that handle the in-place restart behaviour for the following reasons:
		// 1. Technically: The Inplace phases help determine if a switchover is required.
		// 2. Descriptive: They precisely describe the cluster's current state externally.
		if cluster.IsInplaceRestartPhase() {
			contextLogger.Debug(
				"Cluster is in an in-place restart phase. Waiting...",
				"phase", cluster.Status.Phase,
			)
		} else {
			// If not in an Inplace phase, notify that the reconciliation is halted due
			// to an unready instance.
			contextLogger.Debug("Instance pod not active. Retrying...")

			// Register a phase indicating some instances aren't active yet
			if err := r.RegisterPhase(
				ctx,
				cluster,
				apiv1.PhaseWaitingForInstancesToBeActive,
				"Some instances are not yet active. Please wait.",
			); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Requeue reconciliation after a short delay
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	if res, err := persistentvolumeclaim.Reconcile(
		ctx,
		r.Client,
		cluster,
		resources.instances.Items,
		resources.pvcs.Items,
	); err != nil || !res.IsZero() {
		return res, err
	}

	// In-place Postgres major version upgrades
	if result, err := majorupgrade.Reconcile(
		ctx,
		r.Client,
		cluster,
		resources.instances.Items,
		resources.pvcs.Items,
		resources.jobs.Items,
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot reconcile in-place major version upgrades: %w", err)
	} else if result != nil {
		return *result, err
	}

	// Reconcile Pods
	if res, err := r.reconcilePods(ctx, cluster, resources, instancesStatus); !res.IsZero() || err != nil {
		return res, err
	}

	if len(resources.instances.Items) > 0 && resources.noInstanceIsAlive() {
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseUnrecoverable,
			"No pods are active, the cluster needs manual intervention "); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// If we still need more instances, we need to wait before setting healthy status
	if instancesStatus.InstancesReportingStatus() != cluster.Spec.Instances {
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	// PhaseInplacePrimaryRestart will be patched to healthy in instance manager
	if cluster.Status.Phase == apiv1.PhaseInplacePrimaryRestart {
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	// When everything is reconciled, update the status
	if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseHealthy, ""); err != nil {
		return ctrl.Result{}, err
	}

	r.cleanupCompletedJobs(ctx, resources.jobs)

	return ctrl.Result{}, nil
}

// deleteTerminatedPods will delete the Pods that are terminated
func (r *ClusterReconciler) deleteTerminatedPods(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)
	deletedPods := false

	for idx := range resources.instances.Items {
		pod := &resources.instances.Items[idx]

		if pod.GetDeletionTimestamp() != nil {
			continue
		}

		if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			continue
		}

		contextLogger.Info(
			"Deleting terminated pod",
			"podName", pod.Name,
			"phase", pod.Status.Phase,
		)
		if err := r.Delete(ctx, pod); err != nil && !apierrs.IsNotFound(err) {
			return nil, err
		}
		deletedPods = true

		r.Recorder.Eventf(cluster,
			"Normal",
			"DeletePod",
			"Deleted '%s' pod: '%v'", pod.Status.Phase, pod.Name)
	}

	if deletedPods {
		// We deleted objects. Give time to the informer cache to notice that.
		return &ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	return nil, nil
}

// processUnschedulableInstances will delete the Pods that cannot schedule
func (r *ClusterReconciler) processUnschedulableInstances(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)
	for idx := range resources.instances.Items {
		pod := &resources.instances.Items[idx]

		if pod.GetDeletionTimestamp() != nil {
			continue
		}

		if !utils.IsPodUnschedulable(pod) {
			continue
		}

		if podRollout := isPodNeedingRollout(ctx, pod, cluster); podRollout.required {
			if err := r.upgradePod(
				ctx,
				cluster,
				pod,
				fmt.Sprintf("recreating unschedulable pod: %s", podRollout.reason),
			); err != nil {
				return nil, err
			}
			return &ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		if !cluster.IsNodeMaintenanceWindowInProgress() || cluster.IsReusePVCEnabled() {
			continue
		}

		contextLogger.Warning("Deleting unschedulable pod", "pod", pod.Name, "podStatus", pod.Status)
		if err := r.Delete(ctx, pod); err != nil && !apierrs.IsNotFound(err) {
			return nil, err
		}

		r.Recorder.Eventf(cluster, "Normal", "DeletePod",
			"Deleted unschedulable pod %v",
			pod.Name)

		if err := persistentvolumeclaim.EnsureInstancePVCGroupIsDeleted(
			ctx,
			r.Client,
			cluster,
			pod.Name,
			pod.Namespace,
		); err != nil {
			return nil, err
		}
		r.Recorder.Eventf(cluster, "Normal", "DeletePVCs",
			"Deleted unschedulable pod %v PVCs",
			pod.Name)

		// We deleted the pod and the PVCGroup. Give time to the informer cache to notice that.
		return &ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	return nil, nil
}

// reconcilePods decides when to create, scale up/down or wait for pods
func (r *ClusterReconciler) reconcilePods(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
	instancesStatus postgres.PostgresqlStatusList,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if err := persistentvolumeclaim.MarkPVCReadyForCompletedJobs(
		ctx,
		r.Client,
		resources.pvcs.Items,
		resources.jobs.Items,
	); err != nil {
		return ctrl.Result{}, err
	}

	if res, err := r.ensureInstancesAreCreated(ctx, cluster, resources, instancesStatus); err != nil || !res.IsZero() {
		return res, err
	}

	if err := persistentvolumeclaim.EnsureHealthyPVCsAnnotation(ctx, r.Client, cluster, resources.pvcs.Items); err != nil {
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
		contextLogger.Debug(
			"Waiting for Pods to be ready",
			"podStatus", cluster.Status.InstancesStatus)
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
	if res, err := r.reconcileUnrecoverableInstances(ctx, cluster, resources); !res.IsZero() || err != nil {
		return res, err
	}

	// Should we scale down the cluster?
	if cluster.Status.Instances > cluster.Spec.Instances {
		if err := r.scaleDownCluster(ctx, cluster, resources); err != nil {
			return ctrl.Result{}, fmt.Errorf("cannot scale down cluster: %w", err)
		}
	}

	// Requeue here if there are non-ready Pods.
	// In the rest of the function we are sure that
	// cluster.Status.Instances == cluster.Spec.Instances and
	// we don't need to modify the cluster topology
	if cluster.Status.ReadyInstances != cluster.Status.Instances ||
		cluster.Status.ReadyInstances != len(instancesStatus.Items) {
		contextLogger.Debug("Waiting for Pods to be ready")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	// If there is a Pod that doesn't report its HTTP status,
	// we wait until the Pod gets marked as non ready or until we're
	// able to connect to it.
	if !instancesStatus.IsComplete() {
		podsReportingStatus := stringset.New()
		podsNotReportingStatus := make(map[string]string)
		for i := range instancesStatus.Items {
			podName := instancesStatus.Items[i].Pod.Name
			if instancesStatus.Items[i].Error != nil {
				podsNotReportingStatus[podName] = instancesStatus.Items[i].Error.Error()
			} else {
				podsReportingStatus.Put(podName)
			}
		}

		contextLogger.Info(
			"Waiting for Pods to report HTTP status",
			"podsReportingStatus", podsReportingStatus.ToSortedList(),
			"podsNotReportingStatus", podsNotReportingStatus,
		)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	report := instancesStatus.GetConfigurationReport()

	// If any pod is not reporting its configuration (i.e., uniform == nil),
	// proceed with a rolling update to upgrade the instance manager
	// to a version that reports the configuration status.
	// If all pods report their configuration, wait until all instances
	// report the same configuration.
	if uniform := report.IsUniform(); uniform != nil && !*uniform {
		contextLogger.Debug(
			"Waiting for all Pods to have the same PostgreSQL configuration",
			"configurationReport", report)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	return r.handleRollingUpdate(ctx, cluster, instancesStatus)
}

func (r *ClusterReconciler) handleRollingUpdate(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instancesStatus postgres.PostgresqlStatusList,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).WithName("handle_rolling_update")

	// If we need to roll out a restart of any instance, this is the right moment
	done, err := r.rolloutRequiredInstances(ctx, cluster, &instancesStatus)
	switch {
	case errors.Is(err, errLogShippingReplicaElected):
		contextLogger.Warning(
			"The primary needs to be restarted, but the chosen new primary is still " +
				"not connected via streaming replication, waiting for 5 seconds",
		)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	case errors.Is(err, errRolloutDelayed):
		contextLogger.Warning(
			"A Pod need to be rolled out, but the rollout is being delayed",
		)
		if err := r.RegisterPhase(
			ctx,
			cluster,
			apiv1.PhaseUpgradeDelayed,
			"The cluster need to be update, but the operator is configured to delay "+
				"the operation",
		); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	case err != nil:
		return ctrl.Result{}, err
	case done:
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
func (r *ClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, maxConcurrentReconciles int) error {
	err := r.createFieldIndexes(ctx, mgr)
	if err != nil {
		return err
	}

	b := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles,
		}).
		For(&apiv1.Cluster{}).
		Named("cluster").
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
			&apiv1.ImageCatalog{},
			handler.EnqueueRequestsFromMapFunc(r.mapImageCatalogsToClusters()),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		)

	// Only monitor cluster wide resources if not namespaced
	if !configuration.Current.Namespaced {
		b = b.Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.mapNodeToClusters()),
			builder.WithPredicates(r.nodesPredicate()),
		).
			Watches(
				&apiv1.ClusterImageCatalog{},
				handler.EnqueueRequestsFromMapFunc(r.mapClusterImageCatalogsToClusters()),
				builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
			)
	}

	return b.Complete(r)
}

// jobOwnerIndexFunc maps a job definition to its owning cluster and
// is used as an index function to speed up the lookup of jobs
// created by the operator.
func jobOwnerIndexFunc(rawObj client.Object) []string {
	job := rawObj.(*batchv1.Job)

	if ownerName, ok := IsOwnedByCluster(job); ok {
		return []string{ownerName}
	}

	return nil
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
	// This is not used in namespaced mode
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

	// Create a new indexed field on ImageCatalogs. This field will be used to easily
	// find all the ImageCatalogs pointing to a cluster.
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&apiv1.Cluster{},
		imageCatalogKey, func(rawObj client.Object) []string {
			cluster := rawObj.(*apiv1.Cluster)
			if cluster.Spec.ImageCatalogRef == nil || cluster.Spec.ImageCatalogRef.Name == "" {
				return nil
			}
			return []string{cluster.Spec.ImageCatalogRef.Name}
		}); err != nil {
		return err
	}

	// Create a new indexed field on Jobs.
	return mgr.GetFieldIndexer().IndexField(
		ctx,
		&batchv1.Job{},
		jobOwnerKey, jobOwnerIndexFunc)
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

	if owner.APIVersion != apiSGVString {
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
		if !isNodeUnschedulableOrBeingDrained(node, r.drainTaints) {
			return nil
		}

		var childPods corev1.PodList
		// get all the pods handled by the operator on that node
		err := r.List(ctx, &childPods,
			client.MatchingFields{".spec.nodeName": node.Name},
			client.MatchingLabels{
				utils.ClusterInstanceRoleLabelName: specs.ClusterRoleLabelPrimary,
				utils.PodRoleLabelName:             string(utils.PodRoleInstance),
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
