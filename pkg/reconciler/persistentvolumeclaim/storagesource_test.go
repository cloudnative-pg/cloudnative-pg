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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
					source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
						ctx, clusterWithBootstrapSnapshot, apiv1.BackupList{}))
					Expect(err).ToNot(HaveOccurred())
					Expect(source).ToNot(BeNil())
					Expect(source.Name).To(Equal(pgDataSnapshotVolumeName))
				})

				It("should return the correct source when choosing pgwal", func(ctx context.Context) {
					source, err := NewPgWalCalculator().GetSource(GetCandidateStorageSourceForReplica(
						ctx, clusterWithBootstrapSnapshot, apiv1.BackupList{}))
					Expect(err).ToNot(HaveOccurred())
					Expect(source).ToNot(BeNil())
					Expect(source.Name).To(Equal(pgWalSnapshotVolumeName))
				})
			})

			When("there's a source WAL archive", func() {
				It("should return an empty storage source", func(ctx context.Context) {
					clusterSourceWALArchive := clusterWithBootstrapSnapshot.DeepCopy()
					clusterSourceWALArchive.Spec.Bootstrap.Recovery.Source = "test"
					source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
						ctx,
						clusterSourceWALArchive,
						apiv1.BackupList{},
					))
					Expect(err).ToNot(HaveOccurred())
					Expect(source).To(BeNil())
				})
			})
		})

		When("we have backups", func() {
			It("should return the correct backup", func(ctx context.Context) {
				source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
					ctx,
					clusterWithBootstrapSnapshot,
					backupList,
				))
				Expect(err).ToNot(HaveOccurred())
				Expect(source).ToNot(BeNil())
				Expect(source.Name).To(Equal("completed-backup"))
			})
		})
	})

	When("not bootstrapping from a VolumeSnapshot with no backups", func() {
		It("should return an empty storage source", func(ctx context.Context) {
			source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
				ctx,
				clusterWithBackupSection,
				apiv1.BackupList{},
			))
			Expect(err).ToNot(HaveOccurred())
			Expect(source).To(BeNil())
		})
	})

	When("not bootstrapping from a VolumeSnapshot with backups", func() {
		It("should return the backup as storage source", func(ctx context.Context) {
			source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
				ctx,
				clusterWithBackupSection,
				backupList,
			))
			Expect(err).ToNot(HaveOccurred())
			Expect(source).ToNot(BeNil())
			Expect(source.Name).To(Equal("completed-backup"))
		})

		It("should return the backup as storage source when WAL archiving is via plugin only", func(ctx context.Context) {
			source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
				ctx,
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
			clusterNoWalArchiving := clusterWithBackupSection.DeepCopy()
			clusterNoWalArchiving.Spec.Backup = nil

			source, err := NewPgDataCalculator().GetSource(GetCandidateStorageSourceForReplica(
				ctx,
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
				CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
			},
		}
		source := getCandidateSourceFromBackupList(ctx, cluster, backupList)
		Expect(source).ToNot(BeNil())
		Expect(source.DataSource.Name).To(Equal("completed-backup"))
	})

	It("will refuse to use automatically use snapshots if they are older than the Cluster", func(ctx context.Context) {
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
				CreationTimestamp: metav1.NewTime(time.Now().Add(1 * time.Hour)),
			},
		}
		source := getCandidateSourceFromBackupList(ctx, cluster, backupList)
		Expect(source).To(BeNil())
	})
})

var _ = Describe("major version filtering in candidate backup selection", func() {
	makeCompletedSnapshotBackup := func(name string) apiv1.Backup {
		return apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now()),
				Name:              name,
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
	}

	It("does not filter by major version when PGDataImageInfo is nil", func(ctx context.Context) {
		backup := makeCompletedSnapshotBackup("no-major-version")
		// Explicitly leave MajorVersion=0 and no annotation to simulate missing version
		backupList := apiv1.BackupList{Items: []apiv1.Backup{backup}}

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
			},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						DestinationPath: "s3://test",
					},
				},
			},
			// Status.PGDataImageInfo is nil here on purpose
		}

		source := getCandidateSourceFromBackupList(ctx, cluster, backupList)
		Expect(source).ToNot(BeNil())
		Expect(source.DataSource.Name).To(Equal("completed-backup"))
	})

	It("skips backup when PGDataImageInfo is set and backup major version is missing", func(ctx context.Context) {
		backup := makeCompletedSnapshotBackup("missing-major-version")
		// MajorVersion=0 and no annotation, will be rejected when PGDataImageInfo != nil
		backupList := apiv1.BackupList{Items: []apiv1.Backup{backup}}

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
			},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						DestinationPath: "s3://test",
					},
				},
			},
			Status: apiv1.ClusterStatus{
				PGDataImageInfo: &apiv1.ImageInfo{},
			},
		}

		source := getCandidateSourceFromBackupList(ctx, cluster, backupList)
		Expect(source).To(BeNil())
	})

	It("skips backup when PGDataImageInfo is set and major version mismatches", func(ctx context.Context) {
		backup := makeCompletedSnapshotBackup("mismatching-major-version")
		backup.Status.MajorVersion = 99 // intentionally mismatching
		backupList := apiv1.BackupList{Items: []apiv1.Backup{backup}}

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
			},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						DestinationPath: "s3://test",
					},
				},
			},
			Status: apiv1.ClusterStatus{
				PGDataImageInfo: &apiv1.ImageInfo{},
			},
		}

		source := getCandidateSourceFromBackupList(ctx, cluster, backupList)
		Expect(source).To(BeNil())
	})
})

var _ = Describe("failed snapshot exclusion in storage source selection", func() {
	newCompletedSnapshotBackup := func(name string, cluster *apiv1.Cluster, snapshotName string) apiv1.Backup {
		return apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				// Ensure the backup timestamp is after the cluster's creation
				CreationTimestamp: metav1.NewTime(cluster.CreationTimestamp.Add(1 * time.Hour)),
				Name:              name,
			},
			Spec: apiv1.BackupSpec{
				Method: apiv1.BackupMethodVolumeSnapshot,
			},
			Status: apiv1.BackupStatus{
				Phase: apiv1.BackupPhaseCompleted,
				BackupSnapshotStatus: apiv1.BackupSnapshotStatus{
					Elements: []apiv1.BackupSnapshotElementStatus{
						{
							Name: snapshotName,
							Type: string(utils.PVCRolePgData),
						},
					},
				},
			},
		}
	}

	It("should skip a backup whose snapshot is in ExcludedSnapshots", func(ctx context.Context) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
			},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{},
				},
			},
			Status: apiv1.ClusterStatus{
				ExcludedSnapshots: []string{"snap-1"},
			},
		}

		backup1 := newCompletedSnapshotBackup("backup-1", cluster, "snap-1")
		backup2 := newCompletedSnapshotBackup("backup-2", cluster, "snap-2")
		// backup1 is more recent, but its snapshot has previously failed
		backup1.CreationTimestamp = metav1.NewTime(cluster.CreationTimestamp.Add(2 * time.Hour))
		backup2.CreationTimestamp = metav1.NewTime(cluster.CreationTimestamp.Add(1 * time.Hour))

		backupList := apiv1.BackupList{Items: []apiv1.Backup{backup1, backup2}}

		source := GetCandidateStorageSourceForReplica(ctx, cluster, backupList)
		Expect(source).ToNot(BeNil())
		Expect(source.DataSource.Name).To(Equal("snap-2"))
	})

	It("should return nil when all snapshots have failed", func(ctx context.Context) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
			},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{},
				},
			},
			Status: apiv1.ClusterStatus{
				ExcludedSnapshots: []string{"snap-1"},
			},
		}

		backup1 := newCompletedSnapshotBackup("backup-1", cluster, "snap-1")
		backupList := apiv1.BackupList{Items: []apiv1.Backup{backup1}}

		source := GetCandidateStorageSourceForReplica(ctx, cluster, backupList)
		Expect(source).To(BeNil())
	})
})
