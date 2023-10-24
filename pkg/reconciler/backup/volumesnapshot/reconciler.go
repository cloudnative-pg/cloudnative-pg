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
	"errors"
	"fmt"
	"strconv"
	"time"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Reconciler is an object capable of executing a volume snapshot on a running cluster
type Reconciler struct {
	cli                  client.Client
	recorder             record.EventRecorder
	instanceStatusClient *instance.StatusClient
	backupClient         *webserver.BackupClient
}

// ExecutorBuilder is a struct capable of creating a Reconciler
type ExecutorBuilder struct {
	executor Reconciler
}

// NewExecutorBuilder instantiates a new ExecutorBuilder with the minimum required data
func NewExecutorBuilder(
	cli client.Client,
	recorder record.EventRecorder,
) *ExecutorBuilder {
	return &ExecutorBuilder{
		executor: Reconciler{
			cli:                  cli,
			recorder:             recorder,
			instanceStatusClient: instance.NewStatusClient(),
			backupClient:         webserver.NewBackupClient(),
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
	snapshotConfig := *cluster.Spec.Backup.VolumeSnapshot

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

// Execute the volume snapshot of the given cluster instance
// nolint: gocognit
func (se *Reconciler) Execute(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) (*ctrl.Result, error) {
	if cluster.Spec.Backup == nil || cluster.Spec.Backup.VolumeSnapshot == nil {
		return nil, fmt.Errorf("cannot execute a VolumeSnapshot on a cluster without configuration")
	}

	contextLogger := log.FromContext(ctx).WithValues("podName", targetPod.Name)

	volumeSnapshotConfig := cluster.Spec.Backup.VolumeSnapshot
	// Step 0: check if the snapshots have been created already
	volumeSnapshots, err := getBackupVolumeSnapshots(ctx, se.cli, cluster.Namespace, backup.Name)
	if err != nil {
		return nil, err
	}

	// Step 1: preparation of snapshot backup
	if len(volumeSnapshots) == 0 && !volumeSnapshotConfig.GetOnline() {
		contextLogger.Debug("Checking pre-requisites")
		if err := se.ensurePodIsFenced(ctx, cluster, backup, targetPod.Name); err != nil {
			return nil, err
		}

		if res, err := se.waitForPodToBeFenced(ctx, targetPod); res != nil || err != nil {
			return res, err
		}
	}
	if len(volumeSnapshots) == 0 && volumeSnapshotConfig.GetOnline() {
		status, err := se.backupClient.Status(ctx, targetPod.Status.PodIP)
		if err != nil {
			return nil, fmt.Errorf("while getting status: %w", err)
		}

		switch status.Phase {
		case webserver.Starting:
			return &ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		case "":
			req := webserver.StartBackupRequest{
				ImmediateCheckpoint: volumeSnapshotConfig.OnlineConfiguration.ImmediateCheckpoint,
				WaitForArchive:      volumeSnapshotConfig.OnlineConfiguration.WaitForArchive,
				BackupName:          backup.Name,
				Force:               true,
			}
			if _, err := se.backupClient.Start(ctx, targetPod.Status.PodIP, req); err != nil {
				return nil, fmt.Errorf("while trying to start the backup: %w", err)
			}
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

	// Step 3: wait for snapshots to be ready
	if res, err := se.waitSnapshotToBeReadyStep(ctx, volumeSnapshots); res != nil || err != nil {
		return res, err
	}

	if err := se.EnsurePodIsUnfenced(ctx, cluster, backup, targetPod); err != nil {
		return nil, err
	}

	// nolint: nestif
	if volumeSnapshotConfig.GetOnline() {
		status, err := se.backupClient.Status(ctx, targetPod.Status.PodIP)
		if err != nil {
			return nil, fmt.Errorf("while getting status: %w", err)
		}

		if status.Phase == webserver.Started {
			if err := se.backupClient.Stop(ctx, targetPod.Status.PodIP); err != nil {
				return nil, fmt.Errorf("while stopping the backup client: %w", err)
			}
			return &ctrl.Result{RequeueAfter: time.Second * 5}, nil
		}

		if status.Phase != webserver.Completed {
			return &ctrl.Result{RequeueAfter: time.Second}, nil
		}

		backup.Status.BeginLSN = string(status.BeginLSN)
		backup.Status.EndLSN = string(status.EndLSN)
		backup.Status.TablespaceMapFile = status.SpcmapFile
		backup.Status.BackupLabelFile = status.LabelFile
	}

	backup.Status.SetAsCompleted()
	backup.Status.Online = ptr.To(volumeSnapshotConfig.GetOnline())
	snapshots, err := getBackupVolumeSnapshots(ctx, se.cli, backup.Namespace, backup.Name)
	if err != nil {
		return nil, err
	}

	backup.Status.BackupSnapshotStatus.SetSnapshotElements(snapshots)
	if err := backupStatusFromSnapshots(snapshots, &backup.Status); err != nil {
		contextLogger.Error(err, "while enriching the backup status")
	}

	if err := annotateSnapshotsWithBackupData(ctx, se.cli, snapshots, &backup.Status); err != nil {
		contextLogger.Error(err, "while enriching the snapshots's status")
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

// ensurePodIsFenced checks if the preconditions for the execution of this step are
// met or not. If they are not met, it will return an error
func (se *Reconciler) ensurePodIsFenced(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPodName string,
) error {
	contextLogger := log.FromContext(ctx)

	fencedInstances, err := utils.GetFencedInstances(cluster.Annotations)
	if err != nil {
		return fmt.Errorf("could not check if cluster is fenced: %v", err)
	}

	if slices.Equal(fencedInstances.ToList(), []string{targetPodName}) {
		// We already requested the target Pod to be fenced
		return nil
	}

	if fencedInstances.Len() != 0 {
		return errors.New("cannot execute volume snapshot on a cluster that has fenced instances")
	}

	if targetPodName == cluster.Status.CurrentPrimary || targetPodName == cluster.Status.TargetPrimary {
		contextLogger.Warning(
			"Cold Snapshot Backup targets the primary. Primary will be fenced",
			"targetBackup", backup.Name, "targetPod", targetPodName,
		)
	}

	err = resources.ApplyFenceFunc(
		ctx,
		se.cli,
		cluster.Name,
		cluster.Namespace,
		targetPodName,
		utils.AddFencedInstance,
	)
	if errors.Is(err, utils.ErrorServerAlreadyFenced) {
		return nil
	}
	if err != nil {
		return err
	}

	// The list of fenced instances is empty, so we need to request
	// fencing for the target pod
	contextLogger.Info("Fencing Pod", "podName", targetPodName)
	se.recorder.Eventf(backup, "Normal", "FencePod",
		"Fencing Pod %v", targetPodName)

	return nil
}

// EnsurePodIsUnfenced removes the fencing status from the cluster
func (se *Reconciler) EnsurePodIsUnfenced(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
) error {
	contextLogger := log.FromContext(ctx)

	err := resources.ApplyFenceFunc(
		ctx,
		se.cli,
		cluster.Name,
		cluster.Namespace,
		targetPod.Name,
		utils.RemoveFencedInstance,
	)
	if errors.Is(err, utils.ErrorServerAlreadyUnfenced) {
		return nil
	}
	if err != nil {
		return err
	}

	// The list of fenced instances is empty, so we need to request
	// fencing for the target pod
	contextLogger.Info("Unfencing Pod", "podName", targetPod.Name)
	se.recorder.Eventf(backup, "Normal", "UnfencePod",
		"Unfencing Pod %v", targetPod.Name)

	return nil
}

// waitForPodToBeFenced waits for the target Pod to be shut down
func (se *Reconciler) waitForPodToBeFenced(
	ctx context.Context,
	targetPod *corev1.Pod,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	var pod corev1.Pod
	err := se.cli.Get(ctx, types.NamespacedName{Name: targetPod.Name, Namespace: targetPod.Namespace}, &pod)
	if err != nil {
		return nil, err
	}
	ready := utils.IsPodReady(pod)
	if ready {
		contextLogger.Info("Waiting for target Pod to not be ready, retrying", "podName", targetPod.Name)
		return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return nil, nil
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

// waitSnapshotToBeReadyStep waits for every PVC snapshot to be ready to use
func (se *Reconciler) waitSnapshotToBeReadyStep(
	ctx context.Context,
	snapshots []storagesnapshotv1.VolumeSnapshot,
) (*ctrl.Result, error) {
	for i := range snapshots {
		if res, err := se.waitSnapshotAndAnnotate(ctx, &snapshots[i]); res != nil || err != nil {
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
	role := utils.PVCRole(pvc.Labels[utils.PvcRoleLabelName])
	name, err := getSnapshotName(backup.Name, role)
	if err != nil {
		return err
	}

	snapshotConfig := *cluster.Spec.Backup.VolumeSnapshot
	var snapshotClassName *string
	if role == utils.PVCRolePgWal && snapshotConfig.WalClassName != "" {
		snapshotClassName = &snapshotConfig.WalClassName
	}

	// this is the default value if nothing else was assigned
	if snapshotClassName == nil && snapshotConfig.ClassName != "" {
		snapshotClassName = &snapshotConfig.ClassName
	}

	labels := pvc.Labels
	utils.MergeMap(labels, snapshotConfig.Labels)
	annotations := pvc.Annotations
	utils.MergeMap(annotations, snapshotConfig.Annotations)
	transferLabelsToAnnotations(labels, annotations)

	snapshot := storagesnapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
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

// waitSnapshotAndAnnotate waits for a certain snapshot to be ready to use. Once ready it annotates the snapshot with
// SnapshotStartTimeAnnotationName and SnapshotEndTimeAnnotationName.
func (se *Reconciler) waitSnapshotAndAnnotate(
	ctx context.Context,
	snapshot *storagesnapshotv1.VolumeSnapshot,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	info := parseVolumeSnapshotInfo(snapshot)
	if info.Error != nil {
		return nil, info.Error
	}
	if info.Running {
		contextLogger.Info(
			"Waiting for VolumeSnapshot to be ready to use",
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

// getSnapshotName gets the snapshot name for a certain PVC
func getSnapshotName(backupName string, role utils.PVCRole) (string, error) {
	switch role {
	case utils.PVCRolePgData, "":
		return backupName, nil
	case utils.PVCRolePgWal:
		return fmt.Sprintf("%s-wal", backupName), nil
	default:
		return "", fmt.Errorf("unhandled PVCRole type: %s", role)
	}
}
