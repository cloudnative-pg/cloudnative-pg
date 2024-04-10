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

package volumesnapshot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v7/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Reconciler is an object capable of executing a volume snapshot on a running cluster
type Reconciler struct {
	cli                  client.Client
	recorder             record.EventRecorder
	instanceStatusClient *instance.StatusClient
}

// ExecutorBuilder is a struct capable of creating a Reconciler
type ExecutorBuilder struct {
	executor Reconciler
}

// NewReconcilerBuilder instantiates a new ExecutorBuilder with the minimum required data
func NewReconcilerBuilder(
	cli client.Client,
	recorder record.EventRecorder,
) *ExecutorBuilder {
	return &ExecutorBuilder{
		executor: Reconciler{
			cli:                  cli,
			recorder:             recorder,
			instanceStatusClient: instance.NewStatusClient(),
		},
	}
}

// Build returns the Reconciler instance
func (e *ExecutorBuilder) Build() *Reconciler {
	return &e.executor
}

func (se *Reconciler) enrichSnapshot(
	ctx context.Context,
	vs *storagesnapshotv1.VolumeSnapshot,
	backup *apiv1.Backup,
	cluster *apiv1.Cluster,
	targetPod *corev1.Pod,
) error {
	contextLogger := log.FromContext(ctx)
	snapshotConfig := backup.GetVolumeSnapshotConfiguration(*cluster.Spec.Backup.VolumeSnapshot)

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
	if data, err := se.instanceStatusClient.GetPgControlDataFromInstance(ctx, targetPod); err == nil {
		vs.Annotations[utils.PgControldataAnnotationName] = data
		pgControlData := utils.ParsePgControldataOutput(data)
		timelineID, ok := pgControlData["Latest checkpoint's TimeLineID"]
		if ok {
			vs.Labels[utils.BackupTimelineLabelName] = timelineID
		}
		startWal, ok := pgControlData["Latest checkpoint's REDO WAL file"]
		if ok {
			vs.Annotations[utils.BackupStartWALAnnotationName] = startWal
			// TODO: once we have online volumesnapshot backups, this should change
			vs.Annotations[utils.BackupEndWALAnnotationName] = startWal
		}
	} else {
		contextLogger.Error(err, "while querying for pg_controldata")
	}

	vs.Labels[utils.BackupDateLabelName] = time.Now().Format("20060102")
	vs.Labels[utils.BackupMonthLabelName] = time.Now().Format("200601")
	vs.Labels[utils.BackupYearLabelName] = strconv.Itoa(time.Now().Year())
	vs.Annotations[utils.IsOnlineBackupLabelName] = strconv.FormatBool(backup.Status.GetOnline())

	rawCluster, err := json.Marshal(cluster)
	if err != nil {
		return err
	}

	vs.Annotations[utils.ClusterManifestAnnotationName] = string(rawCluster)

	return nil
}

type executor interface {
	prepare(
		ctx context.Context,
		cluster *apiv1.Cluster,
		backup *apiv1.Backup,
		targetPod *corev1.Pod,
	) (*ctrl.Result, error)

	finalize(ctx context.Context,
		cluster *apiv1.Cluster,
		backup *apiv1.Backup,
		targetPod *corev1.Pod,
	) (*ctrl.Result, error)
}

func (se *Reconciler) newExecutor(online bool) executor {
	if online {
		return newOnlineExecutor()
	}

	return newOfflineExecutor(se.cli, se.recorder)
}

// Reconcile the volume snapshot of the given cluster instance
func (se *Reconciler) Reconcile(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) (*ctrl.Result, error) {
	if cluster.Spec.Backup == nil || cluster.Spec.Backup.VolumeSnapshot == nil {
		return nil, fmt.Errorf("cannot execute a VolumeSnapshot on a cluster without configuration")
	}

	volumeSnapshots, err := getBackupVolumeSnapshots(ctx, se.cli, cluster.Namespace, backup.Name)
	if err != nil {
		return nil, err
	}
	volumeSnapshotConfig := backup.GetVolumeSnapshotConfiguration(*cluster.Spec.Backup.VolumeSnapshot)

	exec := se.newExecutor(volumeSnapshotConfig.GetOnline())

	// Step 1: backup preparation.
	// This will set PostgreSQL in backup mode for hot snapshots, or fence the Pods for cold snapshots.
	if len(volumeSnapshots) == 0 {
		if res, err := exec.prepare(ctx, cluster, backup, targetPod); res != nil || err != nil {
			return res, err
		}
	}

	// Step 2: create snapshot
	if len(volumeSnapshots) == 0 {
		// we execute the snapshots only if we don't find any
		if err := se.createSnapshotPVCGroupStep(ctx, cluster, pvcs, backup, targetPod); err != nil {
			return nil, err
		}

		// let's stop this reconciliation loop and wait for
		// the external snapshot controller to catch this new
		// request
		return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Step 3: wait for snapshots to be provisioned
	if res, err := se.waitSnapshotToBeProvisionedStep(ctx, volumeSnapshots); res != nil || err != nil {
		return res, err
	}

	// Step 4: backup finalization.
	// This will unset the PostgreSQL backup mode, and unfence the Pod
	if res, err := se.finalizeSnapshotBackupStep(
		ctx,
		exec,
		cluster,
		backup,
		targetPod,
	); res != nil || err != nil {
		return res, err
	}

	// Step 5: wait for snapshots to be ready to use
	if res, err := se.waitSnapshotToBeReadyStep(ctx, volumeSnapshots); res != nil || err != nil {
		return res, err
	}

	// Step 6: set backup as completed, adds remaining metadata
	return se.completeSnapshotBackupStep(
		ctx,
		backup,
	)
}

// finalizeSnapshotBackupStep is called once the snapshots have been provisioned
// and will unfence the Pod for cold snapshots, or unset PostgreSQL backup mode
func (se *Reconciler) finalizeSnapshotBackupStep(
	ctx context.Context,
	exec executor,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).WithValues("podName", targetPod.Name)
	volumeSnapshotConfig := cluster.Spec.Backup.VolumeSnapshot

	if res, err := exec.finalize(ctx, cluster, backup, targetPod); res != nil || err != nil {
		return res, err
	}

	backup.Status.SetAsFinalizing()
	backup.Status.Online = ptr.To(volumeSnapshotConfig.GetOnline())
	snapshots, err := getBackupVolumeSnapshots(ctx, se.cli, backup.Namespace, backup.Name)
	if err != nil {
		return nil, err
	}

	backup.Status.BackupSnapshotStatus.SetSnapshotElements(snapshots)
	if err := backupStatusFromSnapshots(snapshots, &backup.Status); err != nil {
		contextLogger.Error(err, "while enriching the backup status")
	}

	if err := postgres.PatchBackupStatusAndRetry(ctx, se.cli, backup); err != nil {
		contextLogger.Error(err, "while patching the backup status (finalized backup)")
		return nil, err
	}

	return nil, nil
}

// completeSnapshotBackupStep sets a backup as completed, and set the remaining metadata
// on it
func (se *Reconciler) completeSnapshotBackupStep(
	ctx context.Context,
	backup *apiv1.Backup,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)
	backup.Status.SetAsCompleted()
	snapshots, err := getBackupVolumeSnapshots(ctx, se.cli, backup.Namespace, backup.Name)
	if err != nil {
		return nil, err
	}

	if err := annotateSnapshotsWithBackupData(ctx, se.cli, snapshots, &backup.Status); err != nil {
		contextLogger.Error(err, "while enriching the snapshots's status")
		return &ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}
	return nil, postgres.PatchBackupStatusAndRetry(ctx, se.cli, backup)
}

// AnnotateSnapshots adds labels and annotations to the snapshots using the backup
// status to facilitate access
func annotateSnapshotsWithBackupData(
	ctx context.Context,
	cli client.Client,
	snapshots slice,
	backupStatus *apiv1.BackupStatus,
) error {
	contextLogger := log.FromContext(ctx)
	for idx := range snapshots {
		snapshot := &snapshots[idx]
		oldSnapshot := snapshot.DeepCopy()
		snapshot.Annotations[utils.BackupStartTimeAnnotationName] = backupStatus.StartedAt.Format(time.RFC3339)
		snapshot.Annotations[utils.BackupEndTimeAnnotationName] = backupStatus.StoppedAt.Format(time.RFC3339)

		if len(backupStatus.BackupLabelFile) > 0 {
			snapshot.Annotations[utils.BackupLabelFileAnnotationName] = base64.StdEncoding.EncodeToString(
				backupStatus.BackupLabelFile)
		}

		if len(backupStatus.TablespaceMapFile) > 0 {
			snapshot.Annotations[utils.BackupTablespaceMapFileAnnotationName] = base64.StdEncoding.EncodeToString(
				backupStatus.TablespaceMapFile)
		}

		if err := cli.Patch(ctx, snapshot, client.MergeFrom(oldSnapshot)); err != nil {
			contextLogger.Error(err, "while updating volume snapshot from backup object",
				"snapshot", snapshot.Name)
			return err
		}
	}
	return nil
}

// backupStatusFromSnapshots adds fields to the backup status based on the snapshots
func backupStatusFromSnapshots(
	snapshots slice,
	backupStatus *apiv1.BackupStatus,
) error {
	controldata, err := snapshots.getControldata()
	if err != nil {
		return err
	}
	pairs := utils.ParsePgControldataOutput(controldata)

	// TODO: calculate wal in case of online backup
	// the begin/end WAL and LSN are the same, since the instance was fenced
	// for the snapshot
	backupStatus.BeginWal = pairs["Latest checkpoint's REDO WAL file"]
	backupStatus.EndWal = pairs["Latest checkpoint's REDO WAL file"]

	if !backupStatus.GetOnline() {
		backupStatus.BeginLSN = pairs["Latest checkpoint's REDO location"]
		backupStatus.EndLSN = pairs["Latest checkpoint's REDO location"]
	}

	return nil
}

// EnsurePodIsUnfenced removes the fencing status from the cluster
func EnsurePodIsUnfenced(
	ctx context.Context,
	cli client.Client,
	recorder record.EventRecorder,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
) error {
	contextLogger := log.FromContext(ctx)

	if err := utils.NewFencingMetadataExecutor(cli).
		RemoveFencing().
		ForInstance(targetPod.Name).
		Execute(ctx, client.ObjectKeyFromObject(cluster), cluster); err != nil {
		return err
	}

	// The list of fenced instances is empty, so we need to request
	// fencing for the target pod
	contextLogger.Info("Unfencing Pod", "podName", targetPod.Name)
	recorder.Eventf(backup, "Normal", "UnfencePod",
		"Unfencing Pod %v", targetPod.Name)

	return nil
}

// snapshotPVCGroup creates a volumeSnapshot resource for every PVC
// used by the Pod
func (se *Reconciler) createSnapshotPVCGroupStep(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
) error {
	for i := range pvcs {
		se.recorder.Eventf(backup, "Normal", "CreateSnapshot",
			"Creating VolumeSnapshot for PVC %v", pvcs[i].Name)

		err := se.createSnapshot(ctx, cluster, backup, targetPod, &pvcs[i])
		if err != nil {
			return err
		}
	}

	return nil
}

// waitSnapshotToBeProvisionedStep waits for every PVC snapshot to be claimed
func (se *Reconciler) waitSnapshotToBeProvisionedStep(
	ctx context.Context,
	snapshots []storagesnapshotv1.VolumeSnapshot,
) (*ctrl.Result, error) {
	for i := range snapshots {
		if res, err := se.waitSnapshotToBeProvisionedAndAnnotate(ctx, &snapshots[i]); res != nil || err != nil {
			return res, err
		}
	}

	return nil, nil
}

// waitSnapshotToBeReadyStep waits for every PVC snapshot to be ready to use
func (se *Reconciler) waitSnapshotToBeReadyStep(
	ctx context.Context,
	snapshots []storagesnapshotv1.VolumeSnapshot,
) (*ctrl.Result, error) {
	for i := range snapshots {
		if res, err := se.waitSnapshotToBeReady(ctx, &snapshots[i]); res != nil || err != nil {
			return res, err
		}
	}

	return nil, nil
}

// createSnapshot creates a VolumeSnapshot resource for the given PVC and
// add it to the command status
func (se *Reconciler) createSnapshot(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
	pvc *corev1.PersistentVolumeClaim,
) error {
	pvcCalculator, err := persistentvolumeclaim.GetExpectedObjectCalculator(pvc.GetLabels())
	if err != nil {
		return err
	}

	snapshotConfig := backup.GetVolumeSnapshotConfiguration(*cluster.Spec.Backup.VolumeSnapshot)
	snapshotClassName := pvcCalculator.GetVolumeSnapshotClass(&snapshotConfig)

	if pvc.Annotations == nil {
		pvc.Annotations = map[string]string{}
	}
	if pvc.Labels == nil {
		pvc.Labels = map[string]string{}
	}

	labels := pvc.Labels
	utils.MergeMap(labels, snapshotConfig.Labels)
	annotations := pvc.Annotations
	utils.MergeMap(annotations, snapshotConfig.Annotations)
	transferLabelsToAnnotations(labels, annotations)

	snapshot := storagesnapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pvcCalculator.GetSnapshotName(backup.Name),
			Namespace:   pvc.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: storagesnapshotv1.VolumeSnapshotSpec{
			Source: storagesnapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: &pvc.Name,
			},
			VolumeSnapshotClassName: snapshotClassName,
		},
	}
	if snapshot.Labels == nil {
		snapshot.Labels = map[string]string{}
	}
	if snapshot.Annotations == nil {
		snapshot.Annotations = map[string]string{}
	}

	if err := se.enrichSnapshot(ctx, &snapshot, backup, cluster, targetPod); err != nil {
		return err
	}

	if err := se.cli.Create(ctx, &snapshot); err != nil {
		return fmt.Errorf("while creating VolumeSnapshot %s: %w", snapshot.Name, err)
	}

	return nil
}

// transferLabelsToAnnotations transfers specified labels to annotations.
// This ensures non-required labels are removed from the upcoming PersistentVolumeClaim.
func transferLabelsToAnnotations(labels map[string]string, annotations map[string]string) {
	if labels == nil || annotations == nil {
		return
	}

	labelsToBeTransferred := []string{
		utils.InstanceNameLabelName,
		utils.ClusterInstanceRoleLabelName,
		utils.ClusterRoleLabelName,
		utils.PvcRoleLabelName,
	}

	for _, key := range labelsToBeTransferred {
		value, ok := labels[key]
		if !ok {
			continue
		}
		annotations[key] = value
		delete(labels, key)
	}
}

// waitSnapshotToBeProvisionedAndAnnotate waits for a certain snapshot to be claimed.
// Once the snapshot have been cut, it annotates the snapshot with
// SnapshotStartTimeAnnotationName and SnapshotEndTimeAnnotationName.
func (se *Reconciler) waitSnapshotToBeProvisionedAndAnnotate(
	ctx context.Context,
	snapshot *storagesnapshotv1.VolumeSnapshot,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	info := parseVolumeSnapshotInfo(snapshot)
	if info.error != nil {
		if info.error.isRetryable() {
			contextLogger.Error(info.error,
				"Retryable snapshot provisioning error, trying again",
				"volumeSnapshotName", snapshot.Name)
			return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		return nil, info.error
	}
	if !info.provisioned {
		contextLogger.Info(
			"Waiting for VolumeSnapshot to be provisioned",
			"volumeSnapshotName", snapshot.Name)
		return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	_, hasTimeAnnotation := snapshot.Annotations[utils.SnapshotEndTimeAnnotationName]
	if !hasTimeAnnotation {
		oldSnapshot := snapshot.DeepCopy()
		// as soon as the volume snapshot has stopped running, we should update its
		// snapshotEndTime annotation
		snapshot.Annotations[utils.SnapshotEndTimeAnnotationName] = metav1.Now().Format(time.RFC3339)
		snapshot.Annotations[utils.SnapshotStartTimeAnnotationName] = snapshot.Status.CreationTime.Format(time.RFC3339)
		if err := se.cli.Patch(ctx, snapshot, client.MergeFrom(oldSnapshot)); err != nil {
			contextLogger.Error(err, "while adding time annotations to volume snapshot",
				"snapshot", snapshot.Name)
			return nil, err
		}
	}

	return nil, nil
}

// waitSnapshotToBeReady waits for a certain snapshot to be ready to use. Once ready it annotates the snapshot with
// SnapshotStartTimeAnnotationName and SnapshotEndTimeAnnotationName.
func (se *Reconciler) waitSnapshotToBeReady(
	ctx context.Context,
	snapshot *storagesnapshotv1.VolumeSnapshot,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	info := parseVolumeSnapshotInfo(snapshot)
	if info.error != nil {
		if info.error.isRetryable() {
			contextLogger.Error(info.error,
				"Retryable snapshot provisioning error, trying again",
				"volumeSnapshotName", snapshot.Name)
			return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		return nil, info.error
	}
	if !info.ready {
		contextLogger.Info(
			"Waiting for VolumeSnapshot to be ready to use",
			"volumeSnapshotName", snapshot.Name,
			"boundVolumeSnapshotContentName", snapshot.Status.BoundVolumeSnapshotContentName,
			"readyToUse", snapshot.Status.ReadyToUse)
		return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return nil, nil
}
