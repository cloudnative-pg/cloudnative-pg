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

package persistentvolumeclaim

import (
	"context"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const storageSourceTestNamespace = "default"

func fakeClientWithSnapshots(names ...string) client.Client {
	objs := make([]client.Object, len(names))
	for i, name := range names {
		objs[i] = &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: storageSourceTestNamespace,
			},
		}
	}
	return fake.NewClientBuilder().
		WithScheme(scheme.BuildWithAllKnownScheme()).
		WithObjects(objs...).
		Build()
}

var _ = Describe("Storage configuration", func() {
	cluster := &apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			StorageConfiguration: apiv1.StorageConfiguration{},
			WalStorage:           &apiv1.StorageConfiguration{},
		},
	}

	It("Should not fail when the roles are correct", func() {
		configuration, err := NewPgDataCalculator().GetStorageConfiguration(cluster)
		Expect(configuration).ToNot(BeNil())
		Expect(err).ToNot(HaveOccurred())

		configuration, err = NewPgWalCalculator().GetStorageConfiguration(cluster)
		Expect(configuration).ToNot(BeNil())
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("Storage source", func() {
	pgDataSnapshotVolumeName := "pgdata-snapshot"
	pgWalSnapshotVolumeName := "pgwal-snapshot"

	clusterWithBootstrapSnapshot := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: storageSourceTestNamespace,
		},
		Spec: apiv1.ClusterSpec{
			StorageConfiguration: apiv1.StorageConfiguration{},
			WalStorage:           &apiv1.StorageConfiguration{},
			Bootstrap: &apiv1.BootstrapConfiguration{
				Recovery: &apiv1.BootstrapRecovery{
					VolumeSnapshots: &apiv1.DataSource{
						Storage: corev1.TypedLocalObjectReference{
							Name:     pgDataSnapshotVolumeName,
							Kind:     apiv1.VolumeSnapshotKind,
							APIGroup: ptr.To("snapshot.storage.k8s.io"),
						},
						WalStorage: &corev1.TypedLocalObjectReference{
							Name:     pgWalSnapshotVolumeName,
							Kind:     apiv1.VolumeSnapshotKind,
							APIGroup: ptr.To("snapshot.storage.k8s.io"),
						},
					},
				},
			},
			Backup: &apiv1.BackupConfiguration{
				BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
					DestinationPath: "s3://test",
				},
			},
		},
	}

	clusterWithBackupSection := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: storageSourceTestNamespace,
		},
		Spec: apiv1.ClusterSpec{
			StorageConfiguration: apiv1.StorageConfiguration{},
			WalStorage:           &apiv1.StorageConfiguration{},
			Backup: &apiv1.BackupConfiguration{
				BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
					DestinationPath: "s3://test",
				},
			},
		},
	}

	clusterWithPluginOnly := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: storageSourceTestNamespace,
		},
		Spec: apiv1.ClusterSpec{
			StorageConfiguration: apiv1.StorageConfiguration{},
			WalStorage:           &apiv1.StorageConfiguration{},
			Backup:               nil,
			Plugins: []apiv1.PluginConfiguration{
				{
					Name:          "test-wal-archiver",
					IsWALArchiver: ptr.To(true),
				},
			},
		},
	}

	backupList := apiv1.BackupList{
		Items: []apiv1.Backup{
			{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(time.Now()),
					Name:              "completed-backup",
				},
				Spec: apiv1.BackupSpec{
					Method: apiv1.BackupMethodVolumeSnapshot,
				},
				Status: apiv1.BackupStatus{
					Phase: apiv1.BackupPhaseCompleted,
					BackupSnapshotStatus: apiv1.BackupSnapshotStatus{
						Elements: []apiv1.BackupSnapshotElementStatus{
							{
								Name: "completed-backup",
								Type: string(utils.PVCRolePgData),
							},
						},
					},
				},
			},
		},
	}

	When("bootstrapping from a VolumeSnapshot", func() {
		When("we don't have backups", func() {
			When("there's no source WAL archive", func() {
				It("should return the correct source when choosing pgdata", func(ctx context.Context) {
					c := fakeClientWithSnapshots(pgDataSnapshotVolumeName, pgWalSnapshotVolumeName)
					source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
						ctx, c, clusterWithBootstrapSnapshot, apiv1.BackupList{}))
					Expect(err).ToNot(HaveOccurred())
					Expect(source).ToNot(BeNil())
					Expect(source.Name).To(Equal(pgDataSnapshotVolumeName))
				})

				It("should return the correct source when choosing pgwal", func(ctx context.Context) {
					c := fakeClientWithSnapshots(pgDataSnapshotVolumeName, pgWalSnapshotVolumeName)
					source, err := NewPgWalCalculator().GetSource(GetCandidateStorageSourceForReplica(
						ctx, c, clusterWithBootstrapSnapshot, apiv1.BackupList{}))
					Expect(err).ToNot(HaveOccurred())
					Expect(source).ToNot(BeNil())
					Expect(source.Name).To(Equal(pgWalSnapshotVolumeName))
				})
			})

			When("there's a source WAL archive", func() {
				It("should return an empty storage source", func(ctx context.Context) {
					c := fakeClientWithSnapshots(pgDataSnapshotVolumeName, pgWalSnapshotVolumeName)
					clusterSourceWALArchive := clusterWithBootstrapSnapshot.DeepCopy()
					clusterSourceWALArchive.Spec.Bootstrap.Recovery.Source = "test"
					source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
						ctx,
						c,
						clusterSourceWALArchive,
						apiv1.BackupList{},
					))
					Expect(err).ToNot(HaveOccurred())
					Expect(source).To(BeNil())
				})
			})

			When("the bootstrap VolumeSnapshot has been deleted", func() {
				It("should fall back to nil when the snapshot no longer exists", func(ctx context.Context) {
					c := fakeClientWithSnapshots() // no snapshots exist
					source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
						ctx, c, clusterWithBootstrapSnapshot, apiv1.BackupList{}))
					Expect(err).ToNot(HaveOccurred())
					Expect(source).To(BeNil())
				})
			})
		})

		When("we have backups", func() {
			It("should return the correct backup", func(ctx context.Context) {
				c := fakeClientWithSnapshots("completed-backup")
				source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
					ctx,
					c,
					clusterWithBootstrapSnapshot,
					backupList,
				))
				Expect(err).ToNot(HaveOccurred())
				Expect(source).ToNot(BeNil())
				Expect(source.Name).To(Equal("completed-backup"))
			})

			It("should skip backup when its snapshot has been deleted", func(ctx context.Context) {
				// No snapshots exist - should fall back to bootstrap snapshot check,
				// which also won't find snapshots, so returns nil
				c := fakeClientWithSnapshots()
				source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
					ctx,
					c,
					clusterWithBootstrapSnapshot,
					backupList,
				))
				Expect(err).ToNot(HaveOccurred())
				Expect(source).To(BeNil())
			})
		})
	})

	When("not bootstrapping from a VolumeSnapshot with no backups", func() {
		It("should return an empty storage source", func(ctx context.Context) {
			c := fakeClientWithSnapshots()
			source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
				ctx,
				c,
				clusterWithBackupSection,
				apiv1.BackupList{},
			))
			Expect(err).ToNot(HaveOccurred())
			Expect(source).To(BeNil())
		})
	})

	When("not bootstrapping from a VolumeSnapshot with backups", func() {
		It("should return the backup as storage source", func(ctx context.Context) {
			c := fakeClientWithSnapshots("completed-backup")
			source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
				ctx,
				c,
				clusterWithBackupSection,
				backupList,
			))
			Expect(err).ToNot(HaveOccurred())
			Expect(source).ToNot(BeNil())
			Expect(source.Name).To(Equal("completed-backup"))
		})

		It("should return the backup as storage source when WAL archiving is via plugin only", func(ctx context.Context) {
			c := fakeClientWithSnapshots("completed-backup")
			source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
				ctx,
				c,
				clusterWithPluginOnly,
				backupList,
			))
			Expect(err).ToNot(HaveOccurred())
			Expect(source).ToNot(BeNil())
			Expect(source.Name).To(Equal("completed-backup"))
		})
	})

	When("there's no WAL archiving", func() {
		It("should return an empty storage source", func(ctx context.Context) {
			c := fakeClientWithSnapshots()
			clusterNoWalArchiving := clusterWithBackupSection.DeepCopy()
			clusterNoWalArchiving.Spec.Backup = nil

			source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
				ctx,
				c,
				clusterNoWalArchiving,
				backupList,
			))
			Expect(err).ToNot(HaveOccurred())
			Expect(source).To(BeNil())
		})
	})
})

var _ = Describe("candidate backups", func() {
	now := time.Now()
	completedBackup := apiv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(now),
			Name:              "completed-backup",
		},
		Spec: apiv1.BackupSpec{
			Method: apiv1.BackupMethodVolumeSnapshot,
		},
		Status: apiv1.BackupStatus{
			Phase: apiv1.BackupPhaseCompleted,
			BackupSnapshotStatus: apiv1.BackupSnapshotStatus{
				Elements: []apiv1.BackupSnapshotElementStatus{
					{
						Name: "completed-backup",
						Type: string(utils.PVCRolePgData),
					},
				},
			},
		},
	}

	oldCompletedBackup := apiv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Hour)),
			Name:              "old-backup",
		},
		Spec: apiv1.BackupSpec{
			Method: apiv1.BackupMethodVolumeSnapshot,
		},
		Status: apiv1.BackupStatus{
			Phase: apiv1.BackupPhaseCompleted,
			BackupSnapshotStatus: apiv1.BackupSnapshotStatus{
				Elements: []apiv1.BackupSnapshotElementStatus{
					{
						Name: "bad-name",
						Type: string(utils.PVCRolePgData),
					},
				},
			},
		},
	}

	nonCompletedBackup := apiv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(now),
			Name:              "non-completed-backup",
		},
		Spec: apiv1.BackupSpec{
			Method: apiv1.BackupMethodVolumeSnapshot,
		},
		Status: apiv1.BackupStatus{},
	}

	objectStoreBackup := apiv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(now),
			Name:              "object-store-backup",
		},
		Spec: apiv1.BackupSpec{
			Method: apiv1.BackupMethodBarmanObjectStore,
		},
		Status: apiv1.BackupStatus{
			Phase: apiv1.BackupPhaseCompleted,
		},
	}

	It("takes the most recent candidate backup as source", func(ctx context.Context) {
		c := fakeClientWithSnapshots("completed-backup", "bad-name")
		backupList := apiv1.BackupList{
			Items: []apiv1.Backup{
				objectStoreBackup,
				nonCompletedBackup,
				oldCompletedBackup,
				completedBackup,
			},
		}
		backupList.SortByReverseCreationTime()

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:         storageSourceTestNamespace,
				CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
			},
		}
		source := getCandidateSourceFromBackupList(ctx, c, cluster, backupList)
		Expect(source).ToNot(BeNil())
		Expect(source.DataSource.Name).To(Equal("completed-backup"))
	})

	It("will refuse to use automatically use snapshots if they are older than the Cluster", func(ctx context.Context) {
		c := fakeClientWithSnapshots("completed-backup", "bad-name")
		backupList := apiv1.BackupList{
			Items: []apiv1.Backup{
				objectStoreBackup,
				nonCompletedBackup,
				oldCompletedBackup,
				completedBackup,
			},
		}
		backupList.SortByReverseCreationTime()

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:         storageSourceTestNamespace,
				CreationTimestamp: metav1.NewTime(time.Now().Add(1 * time.Hour)),
			},
		}
		source := getCandidateSourceFromBackupList(ctx, c, cluster, backupList)
		Expect(source).To(BeNil())
	})

	It("falls back to older backup when most recent snapshot is deleted", func(ctx context.Context) {
		// Only "bad-name" snapshot exists (from oldCompletedBackup), "completed-backup" is deleted
		c := fakeClientWithSnapshots("bad-name")
		backupList := apiv1.BackupList{
			Items: []apiv1.Backup{
				objectStoreBackup,
				nonCompletedBackup,
				oldCompletedBackup,
				completedBackup,
			},
		}
		backupList.SortByReverseCreationTime()

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:         storageSourceTestNamespace,
				CreationTimestamp: metav1.NewTime(time.Now().Add(-6 * time.Hour)),
			},
		}
		source := getCandidateSourceFromBackupList(ctx, c, cluster, backupList)
		Expect(source).ToNot(BeNil())
		Expect(source.DataSource.Name).To(Equal("bad-name"))
	})
})
