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

package volumesnapshot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Reconciler is an object capable of executing a volume snapshot on a running cluster
type Reconciler struct {
	cli                  client.Client
	recorder             record.EventRecorder
	instanceStatusClient remote.InstanceClient
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
			instanceStatusClient: remote.NewClient().Instance(),
		},
	}
}

// Build returns the Reconciler instance
func (e *ExecutorBuilder) Build() *Reconciler {
	return &e.executor
}

func (se *Reconciler) enrichSnapshot(
	ctx context.Context,
	vs *volumesnapshotv1.VolumeSnapshot,
	backup *apiv1.Backup,
	cluster *apiv1.Cluster,
	targetPod *corev1.Pod,
) error {
	contextLogger := log.FromContext(ctx)
	snapshotConfig := backup.GetVolumeSnapshotConfiguration(*cluster.Spec.Backup.VolumeSnapshot)

	vs.Labels[utils.BackupNameLabelName] = backup.Name
	vs.Labels[utils.MajorVersionLabelName] = strconv.Itoa(backup.Status.MajorVersion)

	// Common labels
	vs.Labels[utils.KubernetesAppManagedByLabelName] = utils.ManagerName
	vs.Labels[utils.KubernetesAppLabelName] = utils.AppName
	vs.Labels[utils.KubernetesAppInstanceLabelName] = cluster.Name
	vs.Labels[utils.KubernetesAppVersionLabelName] = fmt.Sprint(backup.Status.MajorVersion)
	vs.Labels[utils.KubernetesAppComponentLabelName] = utils.DatabaseComponentName

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
		timelineID, ok := pgControlData.TryGetLatestCheckpointTimelineID()
		if ok {
			vs.Labels[utils.BackupTimelineLabelName] = timelineID
		}
		startWal, ok := pgControlData.TryGetREDOWALFile()
		if ok {
			vs.Annotations[utils.BackupStartWALAnnotationName] = startWal
			// TODO: once we have online volumesnapshot backups, this should change
			vs.Annotations[utils.BackupEndWALAnnotationName] = startWal
		}
	} else {
		contextLogger.Error(err, "while querying for pg_controldata")
	}

	now := time.Now()

	vs.Labels[utils.BackupDateLabelName] = now.Format("20060102")
	vs.Labels[utils.BackupMonthLabelName] = now.Format("200601")
	vs.Labels[utils.BackupYearLabelName] = strconv.Itoa(now.Year())
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
	contextLogger := log.FromContext(ctx).WithName("volumesnapshot_reconciler")

	res, err := se.internalReconcile(ctx, cluster, backup, targetPod, pvcs)
	if isNetworkErrorRetryable(err) {
		contextLogger.Error(err, "detected retryable error while executing snapshot backup, retrying...")
		return &ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	return res, err
}

func (se *Reconciler) internalReconcile(
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
	if res, err := se.waitSnapshotToBeProvisionedStep(ctx, backup, volumeSnapshots); res != nil || err != nil {
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
	if res, err := se.waitSnapshotToBeReadyStep(ctx, backup, volumeSnapshots); res != nil || err != nil {
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
	backup.Status.StoppedAt = backup.Status.ReconciliationTerminatedAt.DeepCopy()

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
	backup *apiv1.Backup,
	snapshots []volumesnapshotv1.VolumeSnapshot,
) (*ctrl.Result, error) {
	for i := range snapshots {
		if res, err := se.waitSnapshotToBeProvisionedAndAnnotate(ctx, backup, &snapshots[i]); res != nil || err != nil {
			return res, err
		}
	}

	return nil, nil
}

// waitSnapshotToBeReadyStep waits for every PVC snapshot to be ready to use
func (se *Reconciler) waitSnapshotToBeReadyStep(
	ctx context.Context,
	backup *apiv1.Backup,
	snapshots []volumesnapshotv1.VolumeSnapshot,
) (*ctrl.Result, error) {
	for i := range snapshots {
		if res, err := se.waitSnapshotToBeReady(ctx, backup, &snapshots[i]); res != nil || err != nil {
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

	labels := maps.Clone(pvc.Labels)
	maps.Copy(labels, snapshotConfig.Labels)
	annotations := maps.Clone(pvc.Annotations)
	maps.Copy(annotations, snapshotConfig.Annotations)
	transferLabelsToAnnotations(labels, annotations)

	snapshot := volumesnapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pvcCalculator.GetSnapshotName(backup.Name),
			Namespace:   pvc.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: volumesnapshotv1.VolumeSnapshotSpec{
			Source: volumesnapshotv1.VolumeSnapshotSource{
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
		// If the creation fails, we check if the object already exists.
		// This is done to handle the case where the OCI CSI driver retries the creation
		// of the VolumeSnapshot, but the first request succeeded in the background.
		// In this case, the second request fails with a 409 Conflict error.
		// If the object exists and has a UID, we consider it created.
		//
		// We are not checking if the error is a collision or something else because
		// we want to be robust against any kind of error that might happen during
		// the creation of the object.
		//
		// In case of a collision, the object will be fetched and we will check if
		// it is the one we want.
		//
		// In case of a generic error, we will check if the object was created
		// anyway.
		var existingSnapshot volumesnapshotv1.VolumeSnapshot
		if errGet := se.cli.Get(ctx, client.ObjectKeyFromObject(&snapshot), &existingSnapshot); errGet != nil {
			if apierrs.IsNotFound(errGet) {
				return fmt.Errorf("while creating VolumeSnapshot %s: %w", snapshot.Name, err)
			}
			return fmt.Errorf("while creating VolumeSnapshot %s: %w (and while getting it: %v)", snapshot.Name, err, errGet)
		}

		if len(existingSnapshot.UID) == 0 {
			return fmt.Errorf("while creating VolumeSnapshot %s: %w (snapshot exists but has no UID)", snapshot.Name, err)
		}

		// The snapshot exists and has a UID, so we consider it created
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
		//nolint:staticcheck // still in use for backward compatibility
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
	backup *apiv1.Backup,
	snapshot *volumesnapshotv1.VolumeSnapshot,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	info := parseVolumeSnapshotInfo(snapshot)
	if info.error != nil {
		return se.handleSnapshotErrors(ctx, backup, info.error)
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
	backup *apiv1.Backup,
	snapshot *volumesnapshotv1.VolumeSnapshot,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	info := parseVolumeSnapshotInfo(snapshot)
	if info.error != nil {
		return se.handleSnapshotErrors(ctx, backup, info.error)
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

func (se *Reconciler) handleSnapshotErrors(
	ctx context.Context,
	backup *apiv1.Backup,
	snapshotErr *volumeSnapshotError,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).
		WithName("handle_snapshot_errors")

	if !snapshotErr.isRetryable() {
		return nil, snapshotErr
	}

	if err := addDeadlineStatus(ctx, se.cli, backup); err != nil {
		return nil, fmt.Errorf("while adding deadline status: %w", err)
	}

	exceeded, err := isDeadlineExceeded(backup)
	if err != nil {
		return nil, fmt.Errorf("while checking if deadline was exceeded: %w", err)
	}
	if exceeded {
		return nil, fmt.Errorf("deadline exceeded for error %w", snapshotErr)
	}

	contextLogger.Error(snapshotErr,
		"Retryable snapshot provisioning error, trying again",
	)
	return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func isDeadlineExceeded(backup *apiv1.Backup) (bool, error) {
	if backup.Status.PluginMetadata[pluginName] == "" {
		return false, fmt.Errorf("no plugin metadata found in backup status")
	}

	data, err := unmarshalMetadata(backup.Status.PluginMetadata[pluginName])
	if err != nil {
		return false, fmt.Errorf("while unmarshalling plugin metadata: %w", err)
	}

	// if the deadline have passed since firstFailureTime we need to consider the deadline exceeded
	deadline := int64(backup.GetVolumeSnapshotDeadline().Seconds())
	return time.Now().Unix()-data.VolumeSnapshotFirstDetectedFailure > deadline, nil
}

type metadata struct {
	// VolumeSnapshotFirstDetectedFailure is UNIX the timestamp when the first volume snapshot failure was detected
	VolumeSnapshotFirstDetectedFailure int64 `json:"volumeSnapshotFirstFailure,omitempty"`
}

func unmarshalMetadata(rawData string) (*metadata, error) {
	var data metadata
	if err := json.Unmarshal([]byte(rawData), &data); err != nil {
		return nil, fmt.Errorf("while unmarshalling metadata: %w", err)
	}

	if data.VolumeSnapshotFirstDetectedFailure == 0 {
		return nil, fmt.Errorf("no volumeSnapshotFirstFailure found in plugin metadata: %s", pluginName)
	}

	return &data, nil
}

func addDeadlineStatus(ctx context.Context, cli client.Client, backup *apiv1.Backup) error {
	if value, ok := backup.Status.PluginMetadata[pluginName]; ok {
		if _, err := unmarshalMetadata(value); err == nil {
			return nil
		}
	}

	data := &metadata{VolumeSnapshotFirstDetectedFailure: time.Now().Unix()}
	rawData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if backup.Status.PluginMetadata == nil {
		backup.Status.PluginMetadata = map[string]string{}
	}

	origBackup := backup.DeepCopy()
	backup.Status.PluginMetadata[pluginName] = string(rawData)

	return cli.Status().Patch(ctx, backup, client.MergeFrom(origBackup))
}
