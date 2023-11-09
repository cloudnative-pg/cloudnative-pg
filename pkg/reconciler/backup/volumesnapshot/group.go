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
	"fmt"

	storagegroupsnapshotv1beta1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// createVolumeGroupSnapshot creates a volume group snapshot for a given cluster
func (se *Reconciler) createVolumeGroupSnapshot(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	snapshotConfig := backup.GetVolumeSnapshotConfiguration(*cluster.Spec.Backup.VolumeSnapshot)

	var snapshotClassName *string
	if len(snapshotConfig.ClassName) > 0 {
		snapshotClassName = &snapshotConfig.ClassName
	}

	snapshot := storagegroupsnapshotv1beta1.VolumeGroupSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backup.Name,
			Namespace: backup.Namespace,
		},
		Spec: storagegroupsnapshotv1beta1.VolumeGroupSnapshotSpec{
			Source: storagegroupsnapshotv1beta1.VolumeGroupSnapshotSource{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						utils.InstanceNameLabelName: targetPod.Name,
					},
				},
			},
			VolumeGroupSnapshotClassName: snapshotClassName,
		},
	}
	if snapshot.Labels == nil {
		snapshot.Labels = map[string]string{}
	}
	if snapshot.Annotations == nil {
		snapshot.Annotations = map[string]string{}
	}
	if err := se.enrichSnapshot(ctx, &snapshot.ObjectMeta, backup, cluster, targetPod); err != nil {
		return err
	}

	if err := se.cli.Create(ctx, &snapshot); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return fmt.Errorf("while creating VolumeGroupSnapshot %s: %w", snapshot.Name, err)
		}

		return se.enrichVolumeGroupSnapshot(ctx, cluster, backup, pvcs)
	}

	return nil
}

// enrichVolumeGroupSnapshot enriches the VolumeSnapshots resources
// created by the VolumeGroupSnapshot object with all the required
// metadata
func (se *Reconciler) enrichVolumeGroupSnapshot(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	var groupSnapshot storagegroupsnapshotv1beta1.VolumeGroupSnapshot
	if err := se.cli.Get(
		ctx,
		client.ObjectKey{Namespace: backup.Namespace, Name: backup.Name},
		&groupSnapshot,
	); err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}

		return err
	}

	// The volume group snapshot is still not bound
	if groupSnapshot.Status.BoundVolumeGroupSnapshotContentName == nil ||
		len(*groupSnapshot.Status.BoundVolumeGroupSnapshotContentName) == 0 {
		return nil
	}

	// Find the snapshot members using the ownership
	memberSnapshots, err := se.findGroupSnapshotMembers(ctx, &groupSnapshot)
	if err != nil {
		return err
	}

	// Wait for the common snapshot controller to create the volume
	// snapshots members
	if len(memberSnapshots) != len(pvcs) {
		return nil
	}

	// Enrich the volume snapshots
	for i := range memberSnapshots {
		if err := se.enrichVolumeGroupSnapshotMember(
			ctx,
			cluster,
			backup,
			&groupSnapshot,
			&memberSnapshots[i],
		); err != nil {
			return err
		}
	}

	return nil
}

// findGroupSnapshotMembers looks up for the member snapshots the passed
// snapshot group
func (se *Reconciler) findGroupSnapshotMembers(
	ctx context.Context,
	vgs *storagegroupsnapshotv1beta1.VolumeGroupSnapshot,
) ([]storagesnapshotv1.VolumeSnapshot, error) {
	var snapshotList storagesnapshotv1.VolumeSnapshotList

	if err := se.cli.List(ctx, &snapshotList, client.InNamespace(vgs.Namespace)); err != nil {
		return nil, err
	}

	result := make([]storagesnapshotv1.VolumeSnapshot, 0, len(snapshotList.Items))
	for idx := range snapshotList.Items {
		snapshot := &snapshotList.Items[idx]
		if len(snapshot.ObjectMeta.OwnerReferences) != 1 {
			continue
		}

		owner := snapshot.ObjectMeta.OwnerReferences[0]
		if owner.Kind != "VolumeGroupSnapshot" {
			continue
		}

		if owner.Name != vgs.Name {
			continue
		}

		result = append(result, *snapshot)
	}

	return result, nil
}

// enrichVolumeSnapshot enriches a Volume Snapshot created by a VolumeGroupSnapshot
func (se *Reconciler) enrichVolumeGroupSnapshotMember(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	groupSnapshot *storagegroupsnapshotv1beta1.VolumeGroupSnapshot,
	snapshot *storagesnapshotv1.VolumeSnapshot,
) error {
	var pvc corev1.PersistentVolumeClaim

	if snapshot.Spec.Source.PersistentVolumeClaimName == nil {
		return fmt.Errorf("missing persistent volume claim name in snapshot %q", snapshot.Name)
	}

	if err := se.cli.Get(
		ctx,
		client.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      *snapshot.Spec.Source.PersistentVolumeClaimName,
		},
		&pvc,
	); err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return err
	}

	snapshotConfig := backup.GetVolumeSnapshotConfiguration(*cluster.Spec.Backup.VolumeSnapshot)

	if snapshot.Labels == nil {
		snapshot.Labels = make(map[string]string)
	}
	if snapshot.Annotations == nil {
		snapshot.Annotations = make(map[string]string)
	}

	origSnapshot := snapshot.DeepCopy()

	utils.MergeMap(snapshot.Labels, groupSnapshot.Labels)
	utils.MergeMap(snapshot.Labels, pvc.Labels)
	utils.MergeMap(snapshot.Labels, snapshotConfig.Labels)
	utils.MergeMap(snapshot.Annotations, groupSnapshot.Annotations)
	utils.MergeMap(snapshot.Annotations, pvc.Annotations)
	utils.MergeMap(snapshot.Annotations, snapshotConfig.Annotations)
	transferLabelsToAnnotations(snapshot.Labels, snapshot.Annotations)

	return se.cli.Patch(ctx, snapshot, client.MergeFrom(origSnapshot))
}
