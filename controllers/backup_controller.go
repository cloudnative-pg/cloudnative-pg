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

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/conditions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/snapshot"
)

// backupPhase indicates the path inside the Backup kind
// where the phase can be located
const backupPhase = ".status.phase"

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	instanceStatusClient *instanceStatusClient
}

// NewBackupReconciler properly initializes the BackupReconciler
func NewBackupReconciler(mgr manager.Manager) *BackupReconciler {
	return &BackupReconciler{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		Recorder:             mgr.GetEventRecorderFor("cloudnative-pg-backup"),
		instanceStatusClient: newInstanceStatusClient(),
	}
}

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;create;watch;list
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

	if cluster.Spec.Backup == nil {
		message := fmt.Sprintf(
			"cannot proceed with the backup because cluster '%s' has no backup section defined",
			clusterName)
		contextLogger.Warning(message)
		r.Recorder.Event(&backup, "Warning", "ClusterHasNoBackupConfig", message)
		tryFlagBackupAsFailed(ctx, r.Client, &backup, errors.New(message))
		return ctrl.Result{}, nil
	}

	contextLogger.Debug("Found cluster for backup", "cluster", clusterName)

	isRunning, err := r.isValidBackupRunning(ctx, &backup, &cluster)
	if err != nil {
		contextLogger.Error(err, "while running isValidBackupRunning")
		return ctrl.Result{}, err
	}
	if isRunning {
		return ctrl.Result{}, nil
	}

	origBackup := backup.DeepCopy()
	// If no good running backups are found we elect a pod for the backup
	pod, err := r.getBackupTargetPod(ctx, &cluster, &backup)
	if err != nil {
		if apierrs.IsNotFound(err) {
			r.Recorder.Eventf(&backup, "Warning", "FindingPod",
				"Couldn't find target pod %s, will retry in 30 seconds", cluster.Status.TargetPrimary)
			contextLogger.Info("Couldn't find target pod, will retry in 30 seconds", "target",
				cluster.Status.TargetPrimary)
			backup.Status.Phase = apiv1.BackupPhasePending
			return ctrl.Result{RequeueAfter: 30 * time.Second}, r.Status().Patch(ctx, &backup, client.MergeFrom(origBackup))
		}
		tryFlagBackupAsFailed(ctx, r.Client, &backup, fmt.Errorf("while getting pod: %w", err))
		r.Recorder.Eventf(&backup, "Warning", "FindingPod", "Error getting target pod: %s",
			cluster.Status.TargetPrimary)
		return ctrl.Result{}, nil
	}
	contextLogger.Debug("Found pod for backup", "pod", pod.Name)

	if !utils.IsPodReady(*pod) {
		contextLogger.Info("Not ready backup target, will retry in 30 seconds", "target", pod.Name)
		backup.Status.Phase = apiv1.BackupPhasePending
		r.Recorder.Eventf(&backup, "Warning", "BackupPending", "Backup target pod not ready: %s",
			cluster.Status.TargetPrimary)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.Status().Patch(ctx, &backup, client.MergeFrom(origBackup))
	}

	contextLogger.Info("Starting backup",
		"cluster", cluster.Name,
		"pod", pod.Name)

	switch backup.Spec.Method {
	case apiv1.BackupMethodBarmanObjectStore:
		if cluster.Spec.Backup.BarmanObjectStore == nil {
			tryFlagBackupAsFailed(ctx, r.Client, &backup,
				errors.New("no barmanObjectStore section defined on the target cluster"))
			return ctrl.Result{}, nil
		}
		// This backup has been started
		if err := startBarmanBackup(ctx, r.Client, &backup, pod, &cluster); err != nil {
			r.Recorder.Eventf(&backup, "Warning", "Error", "Backup exit with error %v", err)
			tryFlagBackupAsFailed(ctx, r.Client, &backup, fmt.Errorf("encountered an error while taking the backup: %w", err))
			return ctrl.Result{}, nil
		}
	case apiv1.BackupMethodVolumeSnapshot:
		if cluster.Spec.Backup.VolumeSnapshot == nil {
			tryFlagBackupAsFailed(ctx, r.Client, &backup,
				errors.New("no volumeSnapshot section defined on the target cluster"))
			return ctrl.Result{}, nil
		}
		if err := r.startSnapshotBackup(ctx, pod, &cluster, &backup); err != nil {
			r.Recorder.Eventf(&backup, "Warning", "Error", "snapshot backup failed: %v", err)
			tryFlagBackupAsFailed(ctx, r.Client, &backup,
				fmt.Errorf("encountered an error while taking the snapshot backup: %w", err))
			return ctrl.Result{}, nil
		}
	default:
		return ctrl.Result{}, fmt.Errorf("unrecognized method: %s", backup.Spec.Method)
	}

	contextLogger.Debug(fmt.Sprintf("object %#q has been reconciled", req.NamespacedName))
	return ctrl.Result{}, nil
}

func (r *BackupReconciler) isValidBackupRunning(
	ctx context.Context,
	backup *apiv1.Backup,
	cluster *apiv1.Cluster,
) (bool, error) {
	contextLogger := log.FromContext(ctx)
	if backup.Status.Phase == "" || backup.Status.InstanceID == nil {
		// We need to start a backup
		r.Recorder.Eventf(backup, "Normal", "Starting",
			"Starting backup for cluster %v", cluster.Name)
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

	containerIsNotRestarted := backup.Status.InstanceID.ContainerID == pod.Status.ContainerStatuses[0].ContainerID
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

func (r *BackupReconciler) startSnapshotBackup(
	ctx context.Context,
	targetPod *corev1.Pod,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) error {
	contextLogger := log.FromContext(ctx)

	// given that we use only kubernetes resources we can use the backup name as ID
	backup.Status.BackupID = backup.Name

	backup.Status.SetAsStarted(targetPod, apiv1.BackupMethodVolumeSnapshot)
	if err := postgres.PatchBackupStatusAndRetry(ctx, r.Client, backup); err != nil {
		return err
	}

	if errCond := conditions.Patch(ctx, r.Client, cluster, apiv1.BackupStartingCondition); errCond != nil {
		log.FromContext(ctx).Error(errCond, "Error while updating backup condition (backup starting)")
	}

	pvcs, err := persistentvolumeclaim.GetInstancePVCs(ctx, r.Client, targetPod.Name, cluster.Namespace)
	if err != nil {
		return fmt.Errorf("cannot get PVCs: %w", err)
	}

	snapshotConfig := *cluster.Spec.Backup.VolumeSnapshot

	rawCluster, err := json.Marshal(cluster)
	if err != nil {
		return err
	}

	snapshotEnrich := func(vs *storagesnapshotv1.VolumeSnapshot) {
		vs.Labels[utils.BackupNameLabelName] = backup.Name

		switch snapshotConfig.SnapshotOwnerReference {
		case apiv1.SnapshotOwnerReferenceCluster:
			cluster.SetInheritedDataAndOwnership(&vs.ObjectMeta)
		case apiv1.SnapshotOwnerReferenceBackup:
			utils.SetAsOwnedBy(&vs.ObjectMeta, backup.ObjectMeta, backup.TypeMeta)
		default:
			break
		}

		// we grab the pg_controldata just before creating the snapshot
		if result := r.instanceStatusClient.getPgControlDataFromInstance(ctx, targetPod); result.Error == nil {
			vs.Annotations[utils.PgControldataAnnotationName] = result.Data
		} else {
			contextLogger.Error(result.Error, "while querying for pg_controldata")
		}

		vs.Annotations[utils.ClusterManifestAnnotationName] = string(rawCluster)
	}

	executor := snapshot.
		NewExecutorBuilder(r.Client, snapshotConfig).
		FenceInstance(true).
		WithSnapshotEnrich(snapshotEnrich).
		Build()

	snapshots, err := executor.Execute(ctx, cluster, targetPod, pvcs)
	if err != nil {
		contextLogger.Error(err, "while executing snapshot backup")
		backup.Status.SetAsFailed(fmt.Errorf("can't execute snapshot backup: %w", err))

		// Update backup status in cluster conditions
		if errCond := conditions.Patch(ctx, r.Client, cluster, apiv1.BuildClusterBackupFailedCondition(err)); errCond != nil {
			log.FromContext(ctx).Error(errCond, "Error while updating backup condition (backup snapshot failed)")
		}
		return postgres.PatchBackupStatusAndRetry(ctx, r.Client, backup)
	}

	if err := conditions.Patch(ctx, r.Client, cluster, apiv1.BackupSucceededCondition); err != nil {
		contextLogger.Error(err, "Can't update the cluster with the completed snapshot backup data")
	}

	backup.Status.SetAsCompleted()
	backup.Status.BackupSnapshotStatus.SetSnapshotList(snapshots)

	return postgres.PatchBackupStatusAndRetry(ctx, r.Client, backup)
}

// getBackupTargetPod returns the correct pod that should run the backup according to the current
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
	postgresqlStatusList := r.instanceStatusClient.getStatusFromInstances(ctx, pods)
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

// startBarmanBackup request a backup in a Pod and marks the backup started
// or failed if needed
func startBarmanBackup(
	ctx context.Context,
	client client.Client,
	backup *apiv1.Backup,
	pod *corev1.Pod,
	cluster *apiv1.Cluster,
) error {
	// This backup has been started
	status := backup.GetStatus()
	status.SetAsStarted(pod, apiv1.BackupMethodBarmanObjectStore)

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
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Backup{}).
		Watches(&apiv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClustersToBackup()),
			builder.WithPredicates(clustersWithBackupPredicate),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5}).
		Complete(r)
}

func (r *BackupReconciler) mapClustersToBackup() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cluster, ok := obj.(*apiv1.Cluster)
		if !ok {
			return nil
		}
		var backups apiv1.BackupList
		err := r.Client.List(ctx, &backups,
			client.MatchingFields{
				backupPhase: apiv1.BackupPhaseRunning,
			},
			client.InNamespace(cluster.GetNamespace()))
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting running backups for cluster", "cluster", cluster.GetName())
		}
		var requests []reconcile.Request
		for _, backup := range backups.Items {
			if backup.Spec.Cluster.Name == cluster.Name {
				continue
			}
			requests = append(requests,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      backup.Name,
						Namespace: backup.Namespace,
					},
				},
			)
		}
		return requests
	}
}

var clustersWithBackupPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		cluster, ok := e.Object.(*apiv1.Cluster)
		if !ok {
			return false
		}
		return cluster.Spec.Backup != nil
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		cluster, ok := e.Object.(*apiv1.Cluster)
		if !ok {
			return false
		}
		return cluster.Spec.Backup != nil
	},
	GenericFunc: func(e event.GenericEvent) bool {
		cluster, ok := e.Object.(*apiv1.Cluster)
		if !ok {
			return false
		}
		return cluster.Spec.Backup != nil
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		cluster, ok := e.ObjectNew.(*apiv1.Cluster)
		if !ok {
			return false
		}
		return cluster.Spec.Backup != nil
	},
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
