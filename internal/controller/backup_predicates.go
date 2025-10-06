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

	"github.com/cloudnative-pg/machinery/pkg/log"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

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

func (r *BackupReconciler) mapClustersToBackup() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cluster, ok := obj.(*apiv1.Cluster)
		if !ok {
			return nil
		}
		var backups apiv1.BackupList
		err := r.List(ctx, &backups,
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

var volumeSnapshotsPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		volumeSnapshot, ok := e.Object.(*volumesnapshotv1.VolumeSnapshot)
		if !ok {
			return false
		}

		return volumeSnapshotHasBackuplabel(volumeSnapshot)
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		volumeSnapshot, ok := e.Object.(*volumesnapshotv1.VolumeSnapshot)
		if !ok {
			return false
		}
		return volumeSnapshotHasBackuplabel(volumeSnapshot)
	},
	GenericFunc: func(e event.GenericEvent) bool {
		volumeSnapshot, ok := e.Object.(*volumesnapshotv1.VolumeSnapshot)
		if !ok {
			return false
		}
		return volumeSnapshotHasBackuplabel(volumeSnapshot)
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		volumeSnapshot, ok := e.ObjectNew.(*volumesnapshotv1.VolumeSnapshot)
		if !ok {
			return false
		}
		return volumeSnapshotHasBackuplabel(volumeSnapshot)
	},
}

func (r *BackupReconciler) mapVolumeSnapshotsToBackups() handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		volumeSnapshot, ok := obj.(*volumesnapshotv1.VolumeSnapshot)
		if !ok {
			return nil
		}

		var requests []reconcile.Request
		if backupName, ok := volumeSnapshot.Labels[utils.BackupNameLabelName]; ok {
			requests = append(requests,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      backupName,
						Namespace: volumeSnapshot.Namespace,
					},
				},
			)
		}
		return requests
	}
}

func volumeSnapshotHasBackuplabel(volumeSnapshot *volumesnapshotv1.VolumeSnapshot) bool {
	_, ok := volumeSnapshot.Labels[utils.BackupNameLabelName]
	return ok
}
