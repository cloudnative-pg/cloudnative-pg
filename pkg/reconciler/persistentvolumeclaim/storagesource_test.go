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

var _ = Describe("replica backup method preference", func() {
	now := time.Now()
	ctx := context.Background()

	volumeSnapshotBackup := apiv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(now),
			Name:              "volume-snapshot-backup",
		},
		Spec: apiv1.BackupSpec{
			Method: apiv1.BackupMethodVolumeSnapshot,
		},
		Status: apiv1.BackupStatus{
			Phase: apiv1.BackupPhaseCompleted,
			BackupSnapshotStatus: apiv1.BackupSnapshotStatus{
				Elements: []apiv1.BackupSnapshotElementStatus{
					{
						Name: "volume-snapshot-backup",
						Type: string(utils.PVCRolePgData),
					},
				},
			},
		},
	}

	barmanBackup := apiv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(now.Add(1 * time.Hour)),
			Name:              "barman-backup",
		},
		Spec: apiv1.BackupSpec{
			Method: apiv1.BackupMethodBarmanObjectStore,
		},
		Status: apiv1.BackupStatus{
			Phase: apiv1.BackupPhaseCompleted,
		},
	}

	clusterWithBackupConfig := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(now.Add(-1 * time.Hour)),
		},
		Spec: apiv1.ClusterSpec{
			Backup: &apiv1.BackupConfiguration{
				BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{},
			},
		},
	}

	When("preference is set to volumeSnapshot", func() {
		It("should prefer volume snapshots over barman backups", func() {
			cluster := clusterWithBackupConfig.DeepCopy()
			cluster.Spec.Backup.ReplicaMethodPreference = apiv1.ReplicaBackupMethodPreferenceVolumeSnapshot

			backupList := apiv1.BackupList{
				Items: []apiv1.Backup{volumeSnapshotBackup, barmanBackup},
			}

			source := GetCandidateStorageSourceForReplica(ctx, cluster, backupList)
			Expect(source).ToNot(BeNil())
			Expect(source.DataSource.Name).To(Equal("volume-snapshot-backup"))
		})

		It("should fall back to barman backups if no volume snapshots", func() {
			cluster := clusterWithBackupConfig.DeepCopy()
			cluster.Spec.Backup.ReplicaMethodPreference = apiv1.ReplicaBackupMethodPreferenceVolumeSnapshot

			backupList := apiv1.BackupList{
				Items: []apiv1.Backup{barmanBackup},
			}

			source := GetCandidateStorageSourceForReplica(ctx, cluster, backupList)
			Expect(source).ToNot(BeNil())
			Expect(source.DataSource.Kind).To(Equal("BarmanBackup"))
			Expect(source.DataSource.Name).To(Equal("barman-backup"))
		})
	})

	When("preference is set to barmanObjectStore", func() {
		It("should prefer barman backups over volume snapshots", func() {
			cluster := clusterWithBackupConfig.DeepCopy()
			cluster.Spec.Backup.ReplicaMethodPreference = apiv1.ReplicaBackupMethodPreferenceBarmanObjectStore

			backupList := apiv1.BackupList{
				Items: []apiv1.Backup{volumeSnapshotBackup, barmanBackup},
			}

			source := GetCandidateStorageSourceForReplica(ctx, cluster, backupList)
			Expect(source).ToNot(BeNil())
			Expect(source.DataSource.Kind).To(Equal("BarmanBackup"))
			Expect(source.DataSource.Name).To(Equal("barman-backup"))
		})

		It("should fall back to volume snapshots if no barman backups", func() {
			cluster := clusterWithBackupConfig.DeepCopy()
			cluster.Spec.Backup.ReplicaMethodPreference = apiv1.ReplicaBackupMethodPreferenceBarmanObjectStore

			backupList := apiv1.BackupList{
				Items: []apiv1.Backup{volumeSnapshotBackup},
			}

			source := GetCandidateStorageSourceForReplica(ctx, cluster, backupList)
			Expect(source).ToNot(BeNil())
			Expect(source.DataSource.Name).To(Equal("volume-snapshot-backup"))
		})
	})

	When("preference is not set", func() {
		It("should default to volumeSnapshot behavior", func() {
			cluster := clusterWithBackupConfig.DeepCopy()
			// Don't set ReplicaMethodPreference, should default to volumeSnapshot

			backupList := apiv1.BackupList{
				Items: []apiv1.Backup{volumeSnapshotBackup, barmanBackup},
			}

			source := GetCandidateStorageSourceForReplica(ctx, cluster, backupList)
			Expect(source).ToNot(BeNil())
			Expect(source.DataSource.Name).To(Equal("volume-snapshot-backup"))
		})
	})
})

var _ = Describe("getReplicaBackupMethodPreference", func() {
	It("should return volumeSnapshot when backup is nil", func() {
		cluster := &apiv1.Cluster{}
		preference := getReplicaBackupMethodPreference(cluster)
		Expect(preference).To(Equal(apiv1.ReplicaBackupMethodPreferenceVolumeSnapshot))
	})

	It("should return volumeSnapshot when ReplicaMethodPreference is empty", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{},
			},
		}
		preference := getReplicaBackupMethodPreference(cluster)
		Expect(preference).To(Equal(apiv1.ReplicaBackupMethodPreferenceVolumeSnapshot))
	})

	It("should return the configured preference", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					ReplicaMethodPreference: apiv1.ReplicaBackupMethodPreferenceBarmanObjectStore,
				},
			},
		}
		preference := getReplicaBackupMethodPreference(cluster)
		Expect(preference).To(Equal(apiv1.ReplicaBackupMethodPreferenceBarmanObjectStore))
	})
})

var _ = Describe("getCandidateSourceFromVolumeSnapshotBackups", func() {
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

	It("should return a backup when creation time is valid", func(ctx context.Context) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
			},
		}
		source := getCandidateSourceFromVolumeSnapshotBackups(ctx, cluster, backupList)
		Expect(source).ToNot(BeNil())
		Expect(source.DataSource.Name).To(Equal("completed-backup"))
	})

	It("should return nil when backup is created before cluster", func(ctx context.Context) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now().Add(1 * time.Hour)),
			},
		}
		source := getCandidateSourceFromVolumeSnapshotBackups(ctx, cluster, backupList)
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

		source := getCandidateSourceFromVolumeSnapshotBackups(ctx, cluster, backupList)
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

		source := getCandidateSourceFromVolumeSnapshotBackups(ctx, cluster, backupList)
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

		source := getCandidateSourceFromVolumeSnapshotBackups(ctx, cluster, backupList)
		Expect(source).To(BeNil())
	})
})
