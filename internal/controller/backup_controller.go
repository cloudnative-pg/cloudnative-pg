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

package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgiClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	postgresStatus "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/backup/volumesnapshot"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	resourcestatus "github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// backupPhase indicates the path inside the Backup kind
// where the phase can be located
const backupPhase = ".status.phase"

// clusterName indicates the path inside the Backup kind
// where the name of the cluster is written
const clusterNameField = ".spec.cluster.name"

// ErrPrimaryImageNeedsUpdate is returned when the primary instance is not running with the latest image
var ErrPrimaryImageNeedsUpdate = fmt.Errorf("primary instance not having expected image, cannot run backup")

// ErrInstanceStatusUnavailable is returned when instance status cannot be retrieved
var ErrInstanceStatusUnavailable = fmt.Errorf("instance status unavailable")

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	DiscoveryClient discovery.DiscoveryInterface

	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Plugins  repository.Interface

	instanceStatusClient remote.InstanceClient
	vsr                  *volumesnapshot.Reconciler
}

// NewBackupReconciler properly initializes the BackupReconciler
func NewBackupReconciler(
	mgr manager.Manager,
	discoveryClient *discovery.DiscoveryClient,
	plugins repository.Interface,
) *BackupReconciler {
	cli := mgr.GetClient()
	recorder := mgr.GetEventRecorderFor("cloudnative-pg-backup")
	return &BackupReconciler{
		Client:               cli,
		DiscoveryClient:      discoveryClient,
		Scheme:               mgr.GetScheme(),
		Recorder:             recorder,
		instanceStatusClient: remote.NewClient().Instance(),
		Plugins:              plugins,
		vsr:                  volumesnapshot.NewReconcilerBuilder(cli, recorder).Build(),
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
// nolint: gocognito
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)
	contextLogger.Debug(fmt.Sprintf("reconciling object %#q", req.NamespacedName))

	var backup apiv1.Backup
	if err := r.Get(ctx, req.NamespacedName, &backup); err != nil {
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, reconcile.TerminalError(err)
		}
		return ctrl.Result{}, err
	}

	switch backup.Status.Phase {
	case apiv1.BackupPhaseFailed, apiv1.BackupPhaseCompleted:
		return ctrl.Result{}, nil
	}

	var cluster apiv1.Cluster
	if res, err := r.getCluster(ctx, &backup, &cluster); err != nil || res != nil {
		if res != nil {
			return *res, err
		}
		return ctrl.Result{}, err
	}

	ctx = cluster.SetInContext(ctx)

	if res, err := r.checkPrerequisites(ctx, backup, cluster); err != nil || res != nil {
		if res != nil {
			return *res, err
		}
		return ctrl.Result{}, err
	}

	// Load the required plugins
	pluginClient, err := cnpgiClient.WithPlugins(
		ctx,
		r.Plugins,
		apiv1.GetPluginConfigurationEnabledPluginNames(cluster.Spec.Plugins)...,
	)
	if err != nil {
		contextLogger.Error(err, "Error loading plugins, retrying")
		return ctrl.Result{}, err
	}
	defer func() {
		pluginClient.Close(ctx)
	}()

	ctx = cnpgiClient.SetPluginClientInContext(ctx, pluginClient)

	contextLogger.Debug("Found cluster for backup", "cluster", cluster.Name)

	// Store in the context the TLS configuration required communicating with the Pods
	ctx, err = certs.NewTLSConfigForContext(
		ctx,
		r.Client,
		cluster.GetServerCASecretObjectKey(),
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	// preflight checks that AREN'T formal.
	// We ask questions like: "are there other backups running?", "is the current backup running?",
	// "is the target instance healthy?"
	if res, err := r.waitIfOtherBackupsRunning(ctx, &backup, &cluster); err != nil || !res.IsZero() {
		return res, err
	}
	isRunning, err := r.isCurrentBackupRunning(ctx, backup, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	if hookResult := preReconcilePluginHooks(ctx, &cluster, &backup); hookResult.StopReconciliation {
		return hookResult.Result, hookResult.Err
	}

	// When the instance manager is working we have to wait for it to finish
	if isRunning && backup.Spec.Method.IsManagedByInstance() {
		return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
	}

	// The backup is ready to start, and before starting it we store
	// the major version inside the Backup resource.
	if err := r.reconcileMajorVersion(ctx, &backup, &cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("error setting major version for backup: %w", err)
	}

	switch {
	case backup.Spec.Method.IsManagedByInstance():
		res, err := r.startBackupManagedByInstance(ctx, cluster, backup)
		if err != nil {
			return ctrl.Result{}, err
		}
		if res != nil {
			return *res, nil
		}
	case backup.Spec.Method.IsManagedByOperator():
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

func (r *BackupReconciler) startBackupManagedByInstance(
	ctx context.Context,
	cluster apiv1.Cluster,
	backup apiv1.Backup,
) (*ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	origBackup := backup.DeepCopy()

	// If no good running backups are found we elect a pod for the backup
	podStatus, err := r.getBackupTargetPod(ctx, &cluster, &backup)
	if apierrs.IsNotFound(err) ||
		errors.Is(err, ErrPrimaryImageNeedsUpdate) ||
		errors.Is(err, ErrInstanceStatusUnavailable) {
		r.Recorder.Eventf(&backup, "Warning", "FindingPod",
			"Couldn't find target pod %s, will retry in 30 seconds", cluster.Status.TargetPrimary)
		contextLogger.Info("Couldn't find target pod, will retry in 30 seconds", "target",
			cluster.Status.TargetPrimary)
		backup.Status.SetAsPending()
		if err := r.Status().Patch(ctx, &backup, client.MergeFrom(origBackup)); err != nil {
			return nil, err
		}
		return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if err != nil {
		_ = resourcestatus.FlagBackupAsFailed(ctx, r.Client, &backup, &cluster, fmt.Errorf("while getting pod: %w", err))
		r.Recorder.Eventf(&backup, "Warning", "FindingPod", "Error getting target pod: %s",
			cluster.Status.TargetPrimary)
		return &ctrl.Result{}, reconcile.TerminalError(err)
	}

	pod := podStatus.Pod
	contextLogger.Debug("Found pod for backup", "pod", pod.Name)

	if !utils.IsPodReady(*pod) {
		contextLogger.Info("Backup target is not ready, will retry in 30 seconds", "target", pod.Name)
		backup.Status.SetAsPending()
		r.Recorder.Eventf(&backup, "Warning", "BackupPending", "Backup target pod not ready: %s",
			cluster.Status.TargetPrimary)
		if err := r.Status().Patch(ctx, &backup, client.MergeFrom(origBackup)); err != nil {
			return nil, err
		}
		return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	contextLogger.Info("Starting backup",
		"cluster", cluster.Name,
		"pod", pod.Name)

	r.Recorder.Eventf(&backup, "Normal", "Starting",
		"Starting backup for cluster %v", cluster.Name)

	// This backup can be started. The SessionID from podStatus is used to detect if the
	// instance manager was restarted during the backup, which would kill the backup process.
	if err := startInstanceManagerBackup(ctx, r.Client, &backup, pod, &cluster, podStatus.SessionID); err != nil {
		r.Recorder.Eventf(&backup, "Warning", "Error", "Backup exit with error %v", err)
		_ = resourcestatus.FlagBackupAsFailed(ctx, r.Client, &backup, &cluster,
			fmt.Errorf("encountered an error while taking the backup: %w", err))
		return &ctrl.Result{}, reconcile.TerminalError(err)
	}
	return nil, nil
}

func (r *BackupReconciler) isCurrentBackupRunning(
	ctx context.Context,
	backup apiv1.Backup,
	cluster apiv1.Cluster,
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	isRunning, err := r.isValidBackupRunning(ctx, &backup, &cluster)
	if err != nil {
		contextLogger.Error(err, "while running isValidBackupRunning")
		return false, err
	}
	if !isRunning {
		return false, nil
	}

	if backup.GetOnlineOrDefault(&cluster) {
		if err := r.ensureTargetPodHealthy(ctx, r.Client, &backup, &cluster); err != nil {
			contextLogger.Error(err, "while ensuring target pod is healthy")

			if flagErr := resourcestatus.FlagBackupAsFailed(ctx, r.Client, &backup, nil,
				fmt.Errorf("while ensuring target pod is healthy: %w", err)); flagErr != nil {
				contextLogger.Error(flagErr, "while flagging backup as failed, retrying...")
				return false, flagErr
			}

			r.Recorder.Eventf(&backup, "Warning", "TargetPodNotHealthy",
				"Error ensuring target pod is healthy: %s", err.Error())

			return false, fmt.Errorf("interrupting backup as target pod is not healthy: %w", err)
		}
	}

	return true, nil
}

// checkPrerequisites checks that the backup and cluster spec are FORMALLY valid and the kubernetes cluster supports
// the chosen backup method.
// These checks cannot be executed in the webhook given that we cannot fetch the cluster.
func (r *BackupReconciler) checkPrerequisites(
	ctx context.Context,
	backup apiv1.Backup,
	cluster apiv1.Cluster,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	flagMissingPrerequisite := func(message string, reason string) (*ctrl.Result, error) {
		contextLogger.Warning(message)
		r.Recorder.Event(&backup, "Warning", reason, message)
		err := errors.New(message)
		_ = resourcestatus.FlagBackupAsFailed(ctx, r.Client, &backup, &cluster, err)
		return &ctrl.Result{}, reconcile.TerminalError(err)
	}

	if hibernation := cluster.Annotations[utils.HibernationAnnotationName]; hibernation ==
		string(utils.HibernationAnnotationValueOn) {
		const message = "cannot backup a hibernated cluster"
		return flagMissingPrerequisite(message, "ClusterIsHibernated")
	}

	if backup.Spec.Method == apiv1.BackupMethodPlugin {
		if len(cluster.Spec.Plugins) == 0 {
			const message = "cannot proceed with the backup as the cluster has no plugin configured"
			return flagMissingPrerequisite(message, "ClusterHasNoBackupExecutorPlugin")
		}

		return nil, nil
	}

	if cluster.Spec.Backup == nil {
		const message = "cannot proceed with the backup as the cluster has no backup section"
		return flagMissingPrerequisite(message, "ClusterHasBackupConfigured")
	}

	if backup.Spec.Method == apiv1.BackupMethodVolumeSnapshot {
		// This check is still needed for when the backup resource creation is forced through the webhook
		if !utils.HaveVolumeSnapshot() {
			const message = "cannot proceed with the backup as the Kubernetes cluster has no VolumeSnapshot support"
			return flagMissingPrerequisite(message, "ClusterHasNoVolumeSnapshotCRD")
		}

		if cluster.Spec.Backup.VolumeSnapshot == nil {
			const message = "no volumeSnapshot section defined on the target cluster"
			return flagMissingPrerequisite(message, "ClusterHasNoVolumeSnapshotSection")
		}
	}

	if backup.Spec.Method == apiv1.BackupMethodBarmanObjectStore {
		if cluster.Spec.Backup.BarmanObjectStore == nil {
			const message = "no barmanObjectStore section defined on the target cluster"
			return flagMissingPrerequisite(message, "ClusterHasNoBarmanSection")
		}
	}

	return nil, nil
}

func (r *BackupReconciler) getCluster(
	ctx context.Context,
	backup *apiv1.Backup,
	cluster *apiv1.Cluster,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	clusterName := backup.Spec.Cluster.Name
	err := r.Get(ctx, client.ObjectKey{
		Namespace: backup.Namespace,
		Name:      clusterName,
	}, cluster)
	if err == nil {
		return nil, nil
	}

	if apierrs.IsNotFound(err) {
		r.Recorder.Eventf(backup, "Warning", "FindingCluster",
			"Unknown cluster %v, will retry in 30 seconds", clusterName)
		origBackup := backup.DeepCopy()
		backup.Status.SetAsPending()
		if patchErr := r.Status().Patch(ctx, backup, client.MergeFrom(origBackup)); patchErr != nil {
			contextLogger.Error(patchErr, "while setting backup as pending")
			return nil, patchErr
		}
		return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	contextLogger.Error(err, "error getting cluster, proceeding to flag backup as failed.")

	if flagErr := resourcestatus.FlagBackupAsFailed(ctx, r.Client, backup, nil,
		fmt.Errorf("while getting cluster %s: %w", clusterName, err)); flagErr != nil {
		contextLogger.Error(flagErr, "while flagging backup as failed, retrying...")
		return nil, flagErr
	}

	r.Recorder.Eventf(backup, "Warning", "FindingCluster",
		"Error getting cluster %v, will not retry: %s", clusterName, err.Error())

	return &ctrl.Result{}, reconcile.TerminalError(err)
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
			backup.Status.InstanceID.PodName,
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

	pgContainerStatus, err := getPostgresContainerStatus(&pod)
	if err != nil {
		contextLogger.Warning("Cannot get postgres container status, assuming container restarted",
			"error", err)
		return false, nil
	}

	containerIsNotRestarted := utils.PodHasContainerStatuses(pod) &&
		backup.Status.InstanceID.ContainerID == pgContainerStatus.ContainerID
	isPodActive := utils.IsPodActive(pod)

	// For instance-managed backups, check if the instance manager was restarted
	// by comparing the SessionID stored when the backup started with the current session ID.
	// If the session ID changed, the instance manager was restarted (e.g., during an operator
	// upgrade or container reboot), which means any running backup goroutine was killed.
	instanceManagerRestarted := false
	if backup.Spec.Method.IsManagedByInstance() && backup.Status.InstanceID.SessionID != "" {
		instanceManagerRestarted = r.isInstanceManagerRestarted(ctx, &pod, backup.Status.InstanceID.SessionID)
	}

	if isCorrectPodElected && containerIsNotRestarted && isPodActive && !instanceManagerRestarted {
		contextLogger.Info("Backup is already running on",
			"cluster", cluster.Name,
			"pod", pod.Name,
			"startedAt", backup.Status.StartedAt)

		// Nothing to do here
		return true, nil
	}

	// Instance manager was restarted/upgraded - the backup process is dead, mark as failed
	// This is a deterministic check that catches the case where the operator was upgraded
	// while a backup was in progress, killing the backup goroutine in the instance manager.
	if instanceManagerRestarted {
		failureReason := fmt.Errorf("instance manager was restarted during backup on pod %s", pod.Name)
		contextLogger.Info("Instance manager was restarted, marking backup as failed",
			"cluster", cluster.Name,
			"pod", pod.Name,
			"storedSessionID", backup.Status.InstanceID.SessionID)
		r.Recorder.Eventf(backup, "Warning", "InstanceManagerRestarted",
			"Backup failed: instance manager was restarted on pod %s", pod.Name)
		if err := resourcestatus.FlagBackupAsFailed(ctx, r.Client, backup, cluster, failureReason); err != nil {
			return false, fmt.Errorf("while marking backup as failed: %w", err)
		}
		return false, reconcile.TerminalError(errors.New("instance manager was restarted during backup"))
	}

	// For other cases (container restarted, pod inactive, wrong pod elected),
	// we restart the backup on another pod
	contextLogger.Info("restarting backup",
		"isCorrectPodElected", isCorrectPodElected,
		"containerNotRestarted", containerIsNotRestarted,
		"isPodActive", isPodActive,
		"target", backup.Spec.Target,
	)
	r.Recorder.Eventf(backup, "Normal", "ReStarting",
		"Restarted backup for cluster %v on instance %v", cluster.Name, pod.Name)

	return false, nil
}

// isInstanceManagerRestarted checks if the instance manager was restarted since
// the backup started. This is done by comparing the SessionID stored when the
// backup started with the current session ID from the instance manager.
// This check detects instance manager restarts including container reboots and
// binary upgrades that would kill any running backup goroutine.
func (r *BackupReconciler) isInstanceManagerRestarted(
	ctx context.Context,
	pod *corev1.Pod,
	storedSessionID string,
) bool {
	contextLogger := log.FromContext(ctx)

	// If no stored session ID, we can't make a comparison - assume backup is still running
	if storedSessionID == "" {
		return false
	}

	// Get the current status of the pod to retrieve the SessionID
	pods := corev1.PodList{Items: []corev1.Pod{*pod}}
	statusList := r.instanceStatusClient.GetStatusFromInstances(ctx, pods)

	if len(statusList.Items) == 0 {
		// If we can't get the status, we can't make a determination
		// Be conservative and assume the backup is still running
		contextLogger.Debug("Could not get instance status, assuming backup still running",
			"pod", pod.Name)
		return false
	}

	currentSessionID := statusList.Items[0].SessionID
	if currentSessionID == "" {
		// If the current session ID is empty, the instance manager may not have reported yet
		contextLogger.Debug("Current SessionID is empty, assuming backup still running",
			"pod", pod.Name)
		return false
	}

	if currentSessionID != storedSessionID {
		contextLogger.Info("Instance manager SessionID changed",
			"pod", pod.Name,
			"storedSessionID", storedSessionID,
			"currentSessionID", currentSessionID)
		return true
	}

	return false
}

func (r *BackupReconciler) reconcileSnapshotBackup(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	targetPod, err := r.getSnapshotTargetPod(ctx, cluster, backup)
	if apierrs.IsNotFound(err) || errors.Is(err, ErrPrimaryImageNeedsUpdate) {
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
		// TODO: shouldn't this be a failed backup?
		origBackup := backup.DeepCopy()
		backup.Status.SetAsPending()
		if err := r.Status().Patch(ctx, backup, client.MergeFrom(origBackup)); err != nil {
			return nil, err
		}

		return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if err != nil {
		messageErr := fmt.Errorf("while getting pod: %w", err)
		if flagErr := resourcestatus.FlagBackupAsFailed(ctx, r.Client, backup, cluster, messageErr); flagErr != nil {
			return nil, fmt.Errorf("while flagging backup as failed: %w", flagErr)
		}
		r.Recorder.Eventf(backup, "Warning", "FindingPod", "Error getting target pod: %s",
			cluster.Status.TargetPrimary)
		return &ctrl.Result{}, reconcile.TerminalError(err)
	}

	ctx = log.IntoContext(ctx, contextLogger.WithValues("targetPodName", targetPod.Name))

	if !utils.PodHasContainerStatuses(*targetPod) {
		return nil, fmt.Errorf("target pod lacks container statuses")
	}

	if len(backup.Status.Phase) == 0 || backup.Status.Phase == apiv1.BackupPhasePending {
		pgContainerStatus, err := getPostgresContainerStatus(targetPod)
		if err != nil {
			return nil, fmt.Errorf("cannot get postgres container status: %w", err)
		}

		// For volume snapshot backups, SessionID is not relevant since the backup
		// is managed by the operator, not the instance manager
		backup.Status.SetAsStarted(
			targetPod.Name,
			pgContainerStatus.ContainerID,
			"", // SessionID not used for operator-managed backups
			apiv1.BackupMethodVolumeSnapshot,
		)
		// given that we use only kubernetes resources we can use the backup name as ID
		backup.Status.BackupID = backup.Name
		backup.Status.BackupName = backup.Name
		backup.Status.StartedAt = backup.Status.ReconciliationStartedAt.DeepCopy()
		if err := postgres.PatchBackupStatusAndRetry(ctx, r.Client, backup); err != nil {
			return nil, err
		}
	}

	if errCond := resourcestatus.PatchConditionsWithOptimisticLock(
		ctx,
		r.Client,
		cluster,
		apiv1.BackupStartingCondition,
	); errCond != nil {
		contextLogger.Error(errCond, "Error while updating backup condition (backup starting)")
	}

	pvcs, err := persistentvolumeclaim.GetInstancePVCs(ctx, r.Client, targetPod.Name, cluster.Namespace)
	if err != nil {
		return nil, fmt.Errorf("cannot get PVCs: %w", err)
	}

	res, err := r.vsr.Reconcile(ctx, cluster, backup, targetPod, pvcs)
	if err != nil {
		// Volume Snapshot errors are not retryable, we need to set this backup as failed
		// and un-fence the Pod
		contextLogger.Error(err, "while executing snapshot backup")
		r.Recorder.Eventf(backup, "Warning", "Error", "snapshot backup failed: %v", err)
		_ = resourcestatus.FlagBackupAsFailed(ctx, r.Client, backup, cluster,
			fmt.Errorf("can't execute snapshot backup: %w", err))
		return nil, volumesnapshot.EnsurePodIsUnfenced(ctx, r.Client, r.Recorder, cluster, backup, targetPod)
	}

	if res != nil {
		return res, nil
	}

	if err := resourcestatus.PatchConditionsWithOptimisticLock(
		ctx,
		r.Client,
		cluster,
		apiv1.BackupSucceededCondition,
	); err != nil {
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
	// Note: we only need the Pod from the status for volume snapshot backups
	// (they are managed by the operator, not instance manager)
	podStatus, err := r.getBackupTargetPod(ctx, cluster, backup)
	if err != nil {
		return nil, err
	}
	contextLogger.Debug("Found pod for backup", "pod", podStatus.Pod.Name)

	return podStatus.Pod, nil
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

// getBackupTargetPod returns the PostgresqlStatus for the pod that should run the backup
// according to the current cluster's target policy. The status includes the Pod reference
// and the SessionID needed for tracking instance manager identity.
func (r *BackupReconciler) getBackupTargetPod(ctx context.Context, //nolint: gocognit
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) (*postgresStatus.PostgresqlStatus, error) {
	contextLogger := log.FromContext(ctx)

	podHasLatestMajorImage := func(pod *corev1.Pod) bool {
		// No backup should run during a major version upgrade
		if cluster.Status.PGDataImageInfo != nil &&
			cluster.Status.Image != cluster.Status.PGDataImageInfo.Image {
			return false
		}

		if len(pod.Spec.Containers) == 0 {
			contextLogger.Warning("Instance has no containers, discarded as target for backup")
			return false
		}

		pgContainer, err := getPostgresContainer(pod)
		if err != nil {
			contextLogger.Warning("Instance has no postgres container, discarded as target for backup")
			return false
		}

		if pgContainer.Image != cluster.Status.Image {
			contextLogger.Debug("Instance not having expected image, discarded as target for backup")
			return false
		}

		return true
	}

	// Step 1: Elect the target pod based on backup target policy
	var targetPod *corev1.Pod
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

	// Find eligible pod based on target policy
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !utils.IsPodReady(*pod) {
			contextLogger.Debug("Instance not ready, discarded as target for backup",
				"pod", pod.Name)
			continue
		}

		if !podHasLatestMajorImage(pod) {
			continue
		}

		// Check if this pod matches the backup target policy
		isPrimary := pod.Name == cluster.Status.TargetPrimary
		switch backupTarget {
		case apiv1.BackupTargetPrimary:
			if isPrimary {
				contextLogger.Debug("Primary Instance is elected as backup target",
					"instance", pod.Name)
				targetPod = pod
			}
		case apiv1.BackupTargetStandby, "":
			if !isPrimary {
				contextLogger.Debug("Standby Instance is elected as backup target",
					"instance", pod.Name)
				targetPod = pod
			}
		}
		if targetPod != nil {
			break
		}
	}

	// Fallback to primary if no suitable pod found
	if targetPod == nil {
		contextLogger.Debug("No ready instances found as target for backup, defaulting to primary")

		var pod corev1.Pod
		if err = r.Get(ctx, client.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Status.TargetPrimary,
		}, &pod); err != nil {
			return nil, err
		}

		if !podHasLatestMajorImage(&pod) {
			return nil, ErrPrimaryImageNeedsUpdate
		}
		targetPod = &pod
	}

	// Get status for the elected pod
	statusResult := r.instanceStatusClient.GetStatusFromInstances(ctx, corev1.PodList{Items: []corev1.Pod{*targetPod}})
	if len(statusResult.Items) == 0 {
		return nil, fmt.Errorf("could not get instance status for pod %s: %w", targetPod.Name, ErrInstanceStatusUnavailable)
	}

	return &statusResult.Items[0], nil
}

// getPostgresContainerStatus returns the container status for the postgres container in a pod
func getPostgresContainerStatus(pod *corev1.Pod) (*corev1.ContainerStatus, error) {
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == specs.PostgresContainerName {
			return &pod.Status.ContainerStatuses[i], nil
		}
	}
	return nil, fmt.Errorf("postgres container status not found in pod %s", pod.Name)
}

// getPostgresContainer returns the postgres container spec from a pod
func getPostgresContainer(pod *corev1.Pod) (*corev1.Container, error) {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == specs.PostgresContainerName {
			return &pod.Spec.Containers[i], nil
		}
	}
	return nil, fmt.Errorf("postgres container not found in pod %s", pod.Name)
}

// startInstanceManagerBackup request a backup in a Pod and marks the backup started
// or failed if needed. The sessionID parameter is used to track the instance manager
// identity, which allows detecting if the instance manager was restarted during the backup.
func startInstanceManagerBackup(
	ctx context.Context,
	client client.Client,
	backup *apiv1.Backup,
	pod *corev1.Pod,
	cluster *apiv1.Cluster,
	sessionID string,
) error {
	pgContainerStatus, err := getPostgresContainerStatus(pod)
	if err != nil {
		return fmt.Errorf("cannot get postgres container status: %w", err)
	}

	// This backup has been started
	status := backup.GetStatus()
	status.SetAsStarted(pod.Name, pgContainerStatus.ContainerID, sessionID, backup.Spec.Method)

	if err := postgres.PatchBackupStatusAndRetry(ctx, client, backup); err != nil {
		return err
	}
	config := ctrl.GetConfigOrDie()
	clientInterface := kubernetes.NewForConfigOrDie(config)

	var stdout, stderr string
	err = retry.OnError(retry.DefaultBackoff, func(error) bool { return true }, func() error {
		var execErr error
		stdout, stderr, execErr = utils.ExecCommand(
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
		return execErr
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "executing backup", "stdout", stdout, "stderr", stderr)
		setCommandErr := func(backup *apiv1.Backup) {
			backup.Status.CommandError = fmt.Sprintf("with stderr: %s, with stdout: %s", stderr, stdout)
		}
		return resourcestatus.FlagBackupAsFailed(ctx, client, backup, cluster, err, setCommandErr)
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
		clusterNameField, func(rawObj client.Object) []string {
			return []string{rawObj.(*apiv1.Backup).Spec.Cluster.Name}
		}); err != nil {
		return err
	}

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Backup{}).
		Named("backup").
		Watches(&apiv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClustersToBackup()),
			builder.WithPredicates(clustersWithBackupPredicate),
		)
	if utils.HaveVolumeSnapshot() {
		controllerBuilder = controllerBuilder.Watches(
			&volumesnapshotv1.VolumeSnapshot{},
			handler.EnqueueRequestsFromMapFunc(r.mapVolumeSnapshotsToBackups()),
			builder.WithPredicates(volumeSnapshotsPredicate),
		)
	}
	// TODO: allow concurrent reconciliations when the hot snapshot backup reconciler
	// will allow that
	controllerBuilder = controllerBuilder.WithOptions(controller.Options{MaxConcurrentReconciles: 1})
	return controllerBuilder.Complete(r)
}

func (r *BackupReconciler) ensureTargetPodHealthy(
	ctx context.Context,
	cli client.Client,
	backup *apiv1.Backup,
	cluster *apiv1.Cluster,
) error {
	if backup.Status.InstanceID == nil || len(backup.Status.InstanceID.PodName) == 0 {
		return fmt.Errorf("no target pod assigned for backup %s", backup.Name)
	}

	podName := backup.Status.InstanceID.PodName

	var pod corev1.Pod
	if err := cli.Get(ctx, client.ObjectKey{
		Namespace: backup.Namespace,
		Name:      podName,
	}, &pod); err != nil {
		if apierrs.IsNotFound(err) {
			return fmt.Errorf("target pod %s not found in namespace %s for backup %s", podName, backup.Namespace, backup.Name)
		}
		return fmt.Errorf(
			"error getting target pod %s in namespace %s for backup %s: %w", podName, backup.Namespace,
			backup.Name,
			err,
		)
	}

	// if the pod is present we evaluate its health status
	healthyPods, ok := cluster.Status.InstancesStatus[apiv1.PodHealthy]
	if !ok {
		return fmt.Errorf("no status found for target pod %s in cluster %s", podName, cluster.Name)
	}

	if !slices.Contains(healthyPods, podName) {
		return fmt.Errorf("target pod %s is not healthy for backup in cluster %s", podName, cluster.Name)
	}

	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("Target pod is healthy for backup",
		"podName", podName,
		"backupName", backup.Name,
	)
	return nil
}

func (r *BackupReconciler) waitIfOtherBackupsRunning(
	ctx context.Context,
	backup *apiv1.Backup,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	// Validate we don't have other running backups
	var clusterBackups apiv1.BackupList
	if err := r.List(
		ctx,
		&clusterBackups,
		client.InNamespace(backup.GetNamespace()),
		client.MatchingFields{clusterNameField: cluster.Name},
	); err != nil {
		return ctrl.Result{}, err
	}

	if !clusterBackups.CanExecuteBackup(backup.Name) {
		contextLogger.Info(
			"A backup is already in progress or waiting to be started, retrying",
			"targetBackup", backup.Name,
		)
		origBackup := backup.DeepCopy()
		backup.Status.SetAsPending()
		if patchErr := r.Status().Patch(ctx, backup, client.MergeFrom(origBackup)); patchErr != nil {
			contextLogger.Error(patchErr, "while setting backup as pending")
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *BackupReconciler) reconcileMajorVersion(
	ctx context.Context,
	backup *apiv1.Backup,
	cluster *apiv1.Cluster,
) error {
	majorVersion, err := cluster.GetPostgresqlMajorVersion()
	if err != nil {
		return fmt.Errorf("cannot get major version from cluster: %w", err)
	}

	if backup.Status.MajorVersion == majorVersion {
		return nil
	}

	backup.Status.MajorVersion = majorVersion
	return postgres.PatchBackupStatusAndRetry(ctx, r.Client, backup)
}
