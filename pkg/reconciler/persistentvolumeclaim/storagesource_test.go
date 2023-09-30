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
		configuration, err := getStorageConfiguration(cluster, utils.PVCRolePgData)
		Expect(configuration).ToNot(BeNil())
		Expect(err).ToNot(HaveOccurred())

		configuration, err = getStorageConfiguration(cluster, utils.PVCRolePgWal)
		Expect(configuration).ToNot(BeNil())
		Expect(err).ToNot(HaveOccurred())
	})

	It("fail if we look for the wrong role", func() {
		configuration, err := getStorageConfiguration(cluster, "NoRol")
		Expect(err).To(HaveOccurred())
		Expect(configuration.StorageClass).To(BeNil())
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
							Kind:     "VolumeSnapshot",
							APIGroup: ptr.To("snapshot.storage.k8s.io"),
						},
						WalStorage: &corev1.TypedLocalObjectReference{
							Name:     pgWalSnapshotVolumeName,
							Kind:     "VolumeSnapshot",
							APIGroup: ptr.To("snapshot.storage.k8s.io"),
						},
					},
				},
			},
		},
	}

	clusterEmpty := &apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			StorageConfiguration: apiv1.StorageConfiguration{},
			WalStorage:           &apiv1.StorageConfiguration{},
		},
	}

	When("bootstrapping from a VolumeSnapshot", func() {
		It("should fail when looking for a wrong role", func() {
			_, err := GetCandidateStorageSource(clusterWithBootstrapSnapshot, apiv1.BackupList{}).ForRole("NoRol")
			Expect(err).To(HaveOccurred())
		})

		It("should return the correct source when chosing pgdata", func() {
			source, err := GetCandidateStorageSource(
				clusterWithBootstrapSnapshot, apiv1.BackupList{}).ForRole(utils.PVCRolePgData)
			Expect(err).ToNot(HaveOccurred())
			Expect(source).ToNot(BeNil())
			Expect(source.Name).To(Equal(pgDataSnapshotVolumeName))
		})

		It("should return the correct source when chosing pgwal", func() {
			source, err := GetCandidateStorageSource(
				clusterWithBootstrapSnapshot, apiv1.BackupList{}).ForRole(utils.PVCRolePgWal)
			Expect(err).ToNot(HaveOccurred())
			Expect(source).ToNot(BeNil())
			Expect(source.Name).To(Equal(pgWalSnapshotVolumeName))
		})
	})

	When("not bootstrapping from a VolumeSnapshot with no backups", func() {
		It("should return an empty storage source", func() {
			source, err := GetCandidateStorageSource(clusterEmpty, apiv1.BackupList{}).ForRole(utils.PVCRolePgData)
			Expect(err).ToNot(HaveOccurred())
			Expect(source).To(BeNil())
		})
	})

	When("not bootstrapping from a VolumeSnapshot with backups", func() {
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
					},
				},
			},
		}

		It("should return the backup as storage source", func() {
			source, err := GetCandidateStorageSource(clusterEmpty, backupList).ForRole(utils.PVCRolePgData)
			Expect(err).ToNot(HaveOccurred())
			Expect(source.Name).To(Equal("completed-backup"))
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

	It("considers a volumesnapshot completed backup as candidate", func() {
		Expect(isBackupCandidate(&completedBackup)).To(BeTrue())
	})

	It("considers a objectStore completed backup as not candidate", func() {
		Expect(isBackupCandidate(&objectStoreBackup)).To(BeFalse())
	})

	It("considers a backup that is not completed as not candidate", func() {
		Expect(isBackupCandidate(&nonCompletedBackup)).To(BeFalse())
	})

	It("takes the most recent candidate backup as source", func() {
		backupList := apiv1.BackupList{
			Items: []apiv1.Backup{
				objectStoreBackup,
				nonCompletedBackup,
				oldCompletedBackup,
				completedBackup,
			},
		}
		backupList.SortByReverseCreationTime()

		source := getCandidateSourceFromBackupList(backupList)
		Expect(source).ToNot(BeNil())
		Expect(source.DataSource.Name).To(Equal("completed-backup"))
	})
})
