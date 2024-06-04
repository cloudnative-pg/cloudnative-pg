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

package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/conditions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/backup/volumesnapshot"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// backupPhase indicates the path inside the Backup kind
// where the phase can be located
const backupPhase = ".status.phase"

// clusterName indicates the path inside the Backup kind
// where the name of the cluster is written
const clusterName = ".spec.cluster.name"

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	DiscoveryClient discovery.DiscoveryInterface

	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	instanceStatusClient *instance.StatusClient
}

// NewBackupReconciler properly initializes the BackupReconciler
func NewBackupReconciler(mgr manager.Manager, discoveryClient *discovery.DiscoveryClient) *BackupReconciler {
	return &BackupReconciler{
		Client:               mgr.GetClient(),
		DiscoveryClient:      discoveryClient,
		Scheme:               mgr.GetScheme(),
		Recorder:             mgr.GetEventRecorderFor("cloudnative-pg-backup"),
		instanceStatusClient: instance.NewStatusClient(),
	}
}

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;create;watch;list;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get

// Reconcile is the main reconciliation loop
// nolint: gocognit
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)
	contextLogger.Debug(fmt.Sprintf("reconciling object %#q", req.NamespacedName))

	var backup apiv1.Backup
	if err := r.Get(ctx, req.NamespacedName, &backup); err != nil {
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	switch backup.Status.Phase {
	case apiv1.BackupPhaseFailed, apiv1.BackupPhaseCompleted:
		return ctrl.Result{}, nil
	}

	clusterName := backup.Spec.Cluster.Name
	var cluster apiv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: backup.Namespace,
		Name:      clusterName,
	}, &cluster); err != nil {
		if apierrs.IsNotFound(err) {
			r.Recorder.Eventf(&backup, "Warning", "FindingCluster",
				"Unknown cluster %v, will retry in 30 seconds", clusterName)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		tryFlagBackupAsFailed(ctx, r.Client, &backup, fmt.Errorf("while getting cluster %s: %w", clusterName, err))
		r.Recorder.Eventf(&backup, "Warning", "FindingCluster",
			"Error getting cluster %v, will not retry: %s", clusterName, err.Error())
		return ctrl.Result{}, nil
	}

	// Plugin pre-hooks
	if hookResult := preReconcilePluginHooks(ctx, &cluster, &backup); hookResult.StopReconciliation {
		return hookResult.Result, hookResult.Err
	}

	// This check is still needed for when the backup resource creation is forced through the webhook
	if backup.Spec.Method == apiv1.BackupMethodVolumeSnapshot && !utils.HaveVolumeSnapshot() {
		message := "cannot proceed with the backup as the Kubernetes cluster has no VolumeSnapshot support"
		contextLogger.Warning(message)
		r.Recorder.Event(&backup, "Warning", "ClusterHasNoVolumeSnapshotCRD", message)
		tryFlagBackupAsFailed(ctx, r.Client, &backup, errors.New(message))
		return ctrl.Result{}, nil
	}

	contextLogger.Debug("Found cluster for backup", "cluster", clusterName)

	isRunning, err := r.isValidBackupRunning(ctx, &backup, &cluster)
	if err != nil {
		contextLogger.Error(err, "while running isValidBackupRunning")
		return ctrl.Result{}, err
	}

	if backup.Spec.Method == apiv1.BackupMethodBarmanObjectStore {
		if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
			tryFlagBackupAsFailed(ctx, r.Client, &backup,
				errors.New("no barmanObjectStore section defined on the target cluster"))
			return ctrl.Result{}, nil
		}

		if isRunning {
			return ctrl.Result{}, nil
		}

		r.Recorder.Eventf(&backup, "Normal", "Starting",
			"Starting backup for cluster %v", cluster.Name)
	}

	if backup.Spec.Method == apiv1.BackupMethodPlugin {
		if isRunning {
			return ctrl.Result{}, nil
		}

		r.Recorder.Eventf(&backup, "Normal", "Starting",
			"Starting backup for cluster %v", cluster.Name)
	}

	origBackup := backup.DeepCopy()

	// From now on, we differentiate backups managed by the instance manager (barman and plugins)
	// from the ones managed directly by the operator (VolumeSnapshot)

	switch backup.Spec.Method {
	case apiv1.BackupMethodBarmanObjectStore, apiv1.BackupMethodPlugin:
		// If no good running backups are found we elect a pod for the backup
		pod, err := r.getBackupTargetPod(ctx, &cluster, &backup)
		if apierrs.IsNotFound(err) {
			r.Recorder.Eventf(&backup, "Warning", "FindingPod",
				"Couldn't find target pod %s, will retry in 30 seconds", cluster.Status.TargetPrimary)
			contextLogger.Info("Couldn't find target pod, will retry in 30 seconds", "target",
				cluster.Status.TargetPrimary)
			backup.Status.Phase = apiv1.BackupPhasePending
			return ctrl.Result{RequeueAfter: 30 * time.Second}, r.Status().Patch(ctx, &backup, client.MergeFrom(origBackup))
		}
		if err != nil {
			tryFlagBackupAsFailed(ctx, r.Client, &backup, fmt.Errorf("while getting pod: %w", err))
			r.Recorder.Eventf(&backup, "Warning", "FindingPod", "Error getting target pod: %s",
				cluster.Status.TargetPrimary)
			return ctrl.Result{}, nil
		}
		contextLogger.Debug("Found pod for backup", "pod", pod.Name)

		if !utils.IsPodReady(*pod) {
			contextLogger.Info("Backup target is not ready, will retry in 30 seconds", "target", pod.Name)
			backup.Status.Phase = apiv1.BackupPhasePending
			r.Recorder.Eventf(&backup, "Warning", "BackupPending", "Backup target pod not ready: %s",
				cluster.Status.TargetPrimary)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, r.Status().Patch(ctx, &backup, client.MergeFrom(origBackup))
		}

		contextLogger.Info("Starting backup",
			"cluster", cluster.Name,
			"pod", pod.Name)

		// This backup can be started
		if err := startInstanceManagerBackup(ctx, r.Client, &backup, pod, &cluster); err != nil {
			r.Recorder.Eventf(&backup, "Warning", "Error", "Backup exit with error %v", err)
			tryFlagBackupAsFailed(ctx, r.Client, &backup, fmt.Errorf("encountered an error while taking the backup: %w", err))
			return ctrl.Result{}, nil
		}
	case apiv1.BackupMethodVolumeSnapshot:
		if cluster.Spec.Backup == nil || cluster.Spec.Backup.VolumeSnapshot == nil {
			tryFlagBackupAsFailed(ctx, r.Client, &backup,
				errors.New("no volumeSnapshot section defined on the target cluster"))
			return ctrl.Result{}, nil
		}

		res, err := r.reconcileSnapshotBackup(ctx, &cluster, &backup)
		if err != nil {
			return ctrl.Result{}, err
		}
		if res != nil {
			return *res, nil
		}
	default:
		return ctrl.Result{}, fmt.Errorf("unrecognized method: %s", backup.Spec.Method)
	}

	// plugin post hooks
	contextLogger.Debug(fmt.Sprintf("object %#q has been reconciled", req.NamespacedName))

	hookResult := postReconcilePluginHooks(ctx, &cluster, &backup)
	return hookResult.Result, hookResult.Err
}

func (r *BackupReconciler) isValidBackupRunning(
	ctx context.Context,
	backup *apiv1.Backup,
	cluster *apiv1.Cluster,
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	if backup.Status.Phase == "" || backup.Status.InstanceID == nil {
		return false, nil
	}

	// Detect the pod where a backup is being executed or will be executed
	var pod corev1.Pod
	err := r.Get(ctx, client.ObjectKey{
		Namespace: backup.Namespace,
		Name:      backup.Status.InstanceID.PodName,
	}, &pod)

	if apierrs.IsNotFound(err) {
		// We need to restart the backup as the previously selected instance doesn't look healthy
		r.Recorder.Eventf(
			backup,
			"Normal",
			"ReStarting",
			"Could not find the elected backup pod. Restarting backup for cluster %v on instance %v",
			cluster.Name,
			pod.Name,
		)
		return false, nil
	}

	// we can't make decisions to start another backup if we received a different error type
	if err != nil {
		return false, err
	}

	var isCorrectPodElected bool
	switch backup.Spec.Target {
	case apiv1.BackupTargetPrimary:
		isCorrectPodElected = backup.Status.InstanceID.PodName == cluster.Status.TargetPrimary
	case apiv1.BackupTargetStandby, "":
		// we don't really care for this type
		isCorrectPodElected = true
	default:
		return false, fmt.Errorf("unknown.spec.target received: %s", backup.Spec.Target)
	}

	containerIsNotRestarted := utils.PodHasContainerStatuses(pod) &&
		backup.Status.InstanceID.ContainerID == pod.Status.ContainerStatuses[0].ContainerID
	isPodActive := utils.IsPodActive(pod)
	if isCorrectPodElected && containerIsNotRestarted && isPodActive {
		contextLogger.Info("Backup is already running on",
			"cluster", cluster.Name,
			"pod", pod.Name,
			"started at", backup.Status.StartedAt)

		// Nothing to do here
		return true, nil
	}
	contextLogger.Info("restarting backup",
		"isCorrectPodElected", isCorrectPodElected,
		"containerNotRestarted", containerIsNotRestarted,
		"isPodActive", isPodActive,
		"target", backup.Spec.Target,
	)

	// We need to restart the backup as the previously selected instance doesn't look healthy
	r.Recorder.Eventf(backup, "Normal", "ReStarting",
		"Restarted backup for cluster %v on instance %v", cluster.Name, pod.Name)

	return false, nil
}

func (r *BackupReconciler) reconcileSnapshotBackup(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	targetPod, err := r.getSnapshotTargetPod(ctx, cluster, backup)
	if apierrs.IsNotFound(err) {
		r.Recorder.Eventf(
			backup,
			"Warning",
			"FindingPod",
			"Couldn't find target pod %s, will retry in 30 seconds",
			cluster.Status.TargetPrimary,
		)
		contextLogger.Info(
			"Couldn't find target pod, will retry in 30 seconds",
			"target",
			cluster.Status.TargetPrimary,
		)
		origBackup := backup.DeepCopy()
		backup.Status.Phase = apiv1.BackupPhasePending
		err := r.Patch(ctx, backup, client.MergeFrom(origBackup))
		return &ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	if err != nil {
		tryFlagBackupAsFailed(ctx, r.Client, backup, fmt.Errorf("while getting pod: %w", err))
		r.Recorder.Eventf(backup, "Warning", "FindingPod", "Error getting target pod: %s",
			cluster.Status.TargetPrimary)
		return &ctrl.Result{}, nil
	}

	ctx = log.IntoContext(ctx, contextLogger.WithValues("targetPod", targetPod))

	// Validate we don't have other running backups
	var clusterBackups apiv1.BackupList
	if err := r.List(
		ctx,
		&clusterBackups,
		client.InNamespace(backup.GetNamespace()),
		client.MatchingFields{clusterName: cluster.Name},
	); err != nil {
		return nil, err
	}

	if !clusterBackups.CanExecuteBackup(backup.Name) {
		contextLogger.Info(
			"A backup is already in progress or waiting to be started, retrying",
			"targetBackup", backup.Name,
		)
		return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if !utils.PodHasContainerStatuses(*targetPod) {
		return nil, fmt.Errorf("target pod lacks container statuses")
	}

	if len(backup.Status.Phase) == 0 || backup.Status.Phase == apiv1.BackupPhasePending {
		backup.Status.SetAsStarted(
			targetPod.Name,
			targetPod.Status.ContainerStatuses[0].ContainerID,
			apiv1.BackupMethodVolumeSnapshot,
		)
		// given that we use only kubernetes resources we can use the backup name as ID
		backup.Status.BackupID = backup.Name
		backup.Status.BackupName = backup.Name
		backup.Status.StartedAt = ptr.To(metav1.Now())
		if err := postgres.PatchBackupStatusAndRetry(ctx, r.Client, backup); err != nil {
			return nil, err
		}
	}

	if errCond := conditions.Patch(ctx, r.Client, cluster, apiv1.BackupStartingCondition); errCond != nil {
		contextLogger.Error(errCond, "Error while updating backup condition (backup starting)")
	}

	pvcs, err := persistentvolumeclaim.GetInstancePVCs(ctx, r.Client, targetPod.Name, cluster.Namespace)
	if err != nil {
		return nil, fmt.Errorf("cannot get PVCs: %w", err)
	}

	reconciler := volumesnapshot.
		NewReconcilerBuilder(r.Client, r.Recorder).
		Build()

	res, err := reconciler.Reconcile(ctx, cluster, backup, targetPod, pvcs)
	if isErrorRetryable(err) {
		contextLogger.Error(err, "detected retryable error while executing snapshot backup, retrying...")
		return &ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	if err != nil {
		// Volume Snapshot errors are not retryable, we need to set this backup as failed
		// and un-fence the Pod
		contextLogger.Error(err, "while executing snapshot backup")
		// Update backup status in cluster conditions
		if errCond := conditions.Patch(ctx, r.Client, cluster, apiv1.BuildClusterBackupFailedCondition(err)); errCond != nil {
			contextLogger.Error(errCond, "Error while updating backup condition (backup snapshot failed)")
		}

		r.Recorder.Eventf(backup, "Warning", "Error", "snapshot backup failed: %v", err)
		tryFlagBackupAsFailed(ctx, r.Client, backup, fmt.Errorf("can't execute snapshot backup: %w", err))
		return nil, volumesnapshot.EnsurePodIsUnfenced(ctx, r.Client, r.Recorder, cluster, backup, targetPod)
	}

	if res != nil {
		return res, nil
	}

	if err := conditions.Patch(ctx, r.Client, cluster, apiv1.BackupSucceededCondition); err != nil {
		contextLogger.Error(err, "Can't update the cluster with the completed snapshot backup data")
	}

	if err := updateClusterWithSnapshotsBackupTimes(ctx, r.Client, cluster.Namespace, cluster.Name); err != nil {
		contextLogger.Error(err, "could not update cluster's backups metadata")
	}

	return nil, nil
}

func (r *BackupReconciler) getSnapshotTargetPod(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) (*corev1.Pod, error) {
	contextLogger := log.FromContext(ctx)

	// If the backup already has a target pod assigned (on a previous reconciliation loop)
	// it will keep it. Otherwise, will use the pod computed by r.getBackupTargetPod()
	targetPod, err := backup.GetAssignedInstance(ctx, r.Client)
	if err != nil {
		return nil, err
	}
	if targetPod != nil {
		contextLogger.Info("found a previously elected pod, reusing it",
			"targetPodName", targetPod.Name)
		return targetPod, nil
	}

	// If no good running backups are found we elect a pod for the backup
	targetPod, err = r.getBackupTargetPod(ctx, cluster, backup)
	if err != nil {
		return nil, err
	}
	contextLogger.Debug("Found pod for backup", "pod", targetPod.Name)

	return targetPod, nil
}

// updateClusterWithSnapshotsBackupTimes updates a cluster's FirstRecoverabilityPoint
// and LastSuccessfulBackup based on the available snapshots
func updateClusterWithSnapshotsBackupTimes(
	ctx context.Context,
	cli client.Client,
	namespace string,
	name string,
) error {
	wrapErr := func(msg string, err error) error {
		return fmt.Errorf("in updateFirstRecoverabilityPont, %s: %w", msg, err)
	}

	// refresh the cluster, as this function will get called after the backup
	// has finished, potentially a long time
	var cluster apiv1.Cluster
	if err := cli.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, &cluster); err != nil {
		return wrapErr("could not refresh cluster", err)
	}

	oldestSnapshot, newestSnapshot, err := volumesnapshot.GetSnapshotsBackupTimes(ctx, cli,
		namespace, name)
	if err != nil {
		return wrapErr("could not get snapshots metadata", err)
	}

	origCluster := cluster.DeepCopy()

	cluster.UpdateBackupTimes(apiv1.BackupMethodVolumeSnapshot, oldestSnapshot, newestSnapshot)

	if !reflect.DeepEqual(origCluster.Status, cluster.Status) {
		err = cli.Status().Patch(ctx, &cluster, client.MergeFrom(origCluster))
		if err != nil {
			return wrapErr("could not patch cluster status", err)
		}
	}
	return nil
}

// isErrorRetryable detects is an error is retryable or not
func isErrorRetryable(err error) bool {
	return apierrs.IsServerTimeout(err) || apierrs.IsConflict(err) || apierrs.IsInternalError(err)
}

// getBackupTargetPod returns the pod that should run the backup according to the current
// cluster's target policy
func (r *BackupReconciler) getBackupTargetPod(ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) (*corev1.Pod, error) {
	contextLogger := log.FromContext(ctx)
	pods, err := GetManagedInstances(ctx, cluster, r.Client)
	if err != nil {
		return nil, err
	}
	var backupTarget apiv1.BackupTarget
	if cluster.Spec.Backup != nil {
		backupTarget = cluster.Spec.Backup.Target
	}
	if backup.Spec.Target != "" {
		backupTarget = backup.Spec.Target
	}
	postgresqlStatusList := r.instanceStatusClient.GetStatusFromInstances(ctx, pods)
	for _, item := range postgresqlStatusList.Items {
		if !item.IsPodReady {
			contextLogger.Debug("Instance not ready, discarded as target for backup",
				"pod", item.Pod.Name)
			continue
		}
		switch backupTarget {
		case apiv1.BackupTargetPrimary:
			if item.IsPrimary {
				contextLogger.Debug("Primary Instance is elected as backup target",
					"instance", item.Pod.Name)
				return item.Pod, nil
			}
		case apiv1.BackupTargetStandby, "":
			if !item.IsPrimary {
				contextLogger.Debug("Standby Instance is elected as backup target",
					"instance", item.Pod.Name)
				return item.Pod, nil
			}
		}
	}

	contextLogger.Debug("No ready instances found as target for backup, defaulting to primary")

	var pod corev1.Pod
	err = r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.Status.TargetPrimary,
	}, &pod)

	return &pod, err
}

// startInstanceManagerBackup request a backup in a Pod and marks the backup started
// or failed if needed
func startInstanceManagerBackup(
	ctx context.Context,
	client client.Client,
	backup *apiv1.Backup,
	pod *corev1.Pod,
	cluster *apiv1.Cluster,
) error {
	// This backup has been started
	status := backup.GetStatus()
	status.SetAsStarted(pod.Name, pod.Status.ContainerStatuses[0].ContainerID, backup.Spec.Method)

	if err := postgres.PatchBackupStatusAndRetry(ctx, client, backup); err != nil {
		return err
	}
	config := ctrl.GetConfigOrDie()
	clientInterface := kubernetes.NewForConfigOrDie(config)

	var err error
	var stdout, stderr string
	err = retry.OnError(retry.DefaultBackoff, func(error) bool { return true }, func() error {
		stdout, stderr, err = utils.ExecCommand(
			ctx,
			clientInterface,
			config,
			*pod,
			specs.PostgresContainerName,
			nil,
			"/controller/manager",
			"backup",
			backup.GetName(),
		)
		return err
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "executing backup", "stdout", stdout, "stderr", stderr)
		status.SetAsFailed(fmt.Errorf("can't execute backup: %w", err))
		status.CommandError = stderr
		status.CommandError = stdout

		// Update backup status in cluster conditions
		if errCond := conditions.Patch(ctx, client, cluster, apiv1.BuildClusterBackupFailedCondition(err)); errCond != nil {
			log.FromContext(ctx).Error(errCond, "Error while updating backup condition (backup failed)")
		}
		return postgres.PatchBackupStatusAndRetry(ctx, client, backup)
	}

	return nil
}

// SetupWithManager sets up this controller given a controller manager
func (r *BackupReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&apiv1.Backup{},
		backupPhase, func(rawObj client.Object) []string {
			return []string{string(rawObj.(*apiv1.Backup).Status.Phase)}
		}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&apiv1.Backup{},
		clusterName, func(rawObj client.Object) []string {
			return []string{rawObj.(*apiv1.Backup).Spec.Cluster.Name}
		}); err != nil {
		return err
	}

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Backup{}).
		Watches(&apiv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClustersToBackup()),
			builder.WithPredicates(clustersWithBackupPredicate),
		)
	if utils.HaveVolumeSnapshot() {
		controllerBuilder = controllerBuilder.Watches(
			&storagesnapshotv1.VolumeSnapshot{},
			handler.EnqueueRequestsFromMapFunc(r.mapVolumeSnapshotsToBackups()),
			builder.WithPredicates(volumeSnapshotsPredicate),
		)
	}
	// TODO: allow concurrent reconciliations when the hot snapshot backup reconciler
	// will allow that
	controllerBuilder = controllerBuilder.WithOptions(controller.Options{MaxConcurrentReconciles: 1})
	return controllerBuilder.Complete(r)
}

func tryFlagBackupAsFailed(
	ctx context.Context,
	cli client.Client,
	backup *apiv1.Backup,
	err error,
) {
	contextLogger := log.FromContext(ctx)
	origBackup := backup.DeepCopy()
	backup.Status.SetAsFailed(err)

	if err := cli.Status().Patch(ctx, backup, client.MergeFrom(origBackup)); err != nil {
		contextLogger.Error(err, "while flagging backup as failed")
	}
}
