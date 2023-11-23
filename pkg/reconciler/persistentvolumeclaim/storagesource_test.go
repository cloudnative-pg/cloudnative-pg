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
								Type: string(utils.PVCRoleValueData),
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
						Type: string(utils.PVCRoleValueData),
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
						Type: string(utils.PVCRoleValueData),
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

		source := getCandidateSourceFromBackupList(ctx, backupList)
		Expect(source).ToNot(BeNil())
		Expect(source.DataSource.Name).To(Equal("completed-backup"))
	})
})
