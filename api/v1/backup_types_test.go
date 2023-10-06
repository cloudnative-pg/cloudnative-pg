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

package v1

import (
	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BackupStatus structure", func() {
	It("can be set as started", func() {
		status := BackupStatus{}
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-example-1",
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						ContainerID: "container-id",
					},
				},
			},
		}

		status.SetAsStarted(&pod, BackupMethodBarmanObjectStore)
		Expect(status.Phase).To(BeEquivalentTo(BackupPhaseStarted))
		Expect(status.InstanceID).ToNot(BeNil())
		Expect(status.InstanceID.PodName).To(Equal("cluster-example-1"))
		Expect(status.InstanceID.ContainerID).To(Equal("container-id"))
		Expect(status.IsDone()).To(BeFalse())
	})

	It("can be set to contain a snapshot list", func() {
		status := BackupStatus{}
		status.BackupSnapshotStatus.SetSnapshotElements([]volumesnapshot.VolumeSnapshot{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-example-snapshot-1",
					Labels: map[string]string{
						utils.PvcRoleLabelName: string(utils.PVCRolePgData),
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-example-snapshot-2",
					Labels: map[string]string{
						utils.PvcRoleLabelName: string(utils.PVCRolePgWal),
					},
				},
			},
		})
		Expect(status.BackupSnapshotStatus.Elements).To(HaveLen(2))
		Expect(status.BackupSnapshotStatus.Elements).To(ContainElement(
			BackupSnapshotElementStatus{Name: "cluster-example-snapshot-1", Type: string(utils.PVCRolePgData)}))
		Expect(status.BackupSnapshotStatus.Elements).To(ContainElement(
			BackupSnapshotElementStatus{Name: "cluster-example-snapshot-2", Type: string(utils.PVCRolePgWal)}))
	})

	Context("backup phases", func() {
		When("the backup phase is `running`", func() {
			It("can tell if a backup is in progress or done", func() {
				b := BackupStatus{
					Phase: BackupPhaseRunning,
				}
				Expect(b.IsInProgress()).To(BeTrue())
				Expect(b.IsDone()).To(BeFalse())
			})
		})

		When("the backup phase is `pending`", func() {
			It("can tell if a backup is in progress or done", func() {
				b := BackupStatus{
					Phase: BackupPhasePending,
				}
				Expect(b.IsInProgress()).To(BeTrue())
				Expect(b.IsDone()).To(BeFalse())
			})
		})

		When("the backup phase is `completed`", func() {
			It("can tell if a backup is in progress or done", func() {
				b := BackupStatus{
					Phase: BackupPhaseCompleted,
				}
				Expect(b.IsInProgress()).To(BeFalse())
				Expect(b.IsDone()).To(BeTrue())
			})
		})

		When("the backup phase is `failed`", func() {
			It("can tell if a backup is in progress or done", func() {
				b := BackupStatus{
					Phase: BackupPhaseFailed,
				}
				Expect(b.IsInProgress()).To(BeFalse())
				Expect(b.IsDone()).To(BeTrue())
			})
		})
	})
})

var _ = Describe("BackupList structure", func() {
	It("can be sorted by name", func() {
		backupList := BackupList{
			Items: []Backup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "backup-3",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "backup-2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "backup-1",
					},
				},
			},
		}
		backupList.SortByName()

		Expect(backupList.Items).To(HaveLen(3))
		Expect(backupList.Items[0].Name).To(Equal("backup-1"))
		Expect(backupList.Items[1].Name).To(Equal("backup-2"))
		Expect(backupList.Items[2].Name).To(Equal("backup-3"))
	})

	It("can isolate pending backups", func() {
		backupList := BackupList{
			Items: []Backup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "backup-3",
					},
					Status: BackupStatus{
						Phase: BackupPhaseRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "backup-2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "backup-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "backup-5",
					},
					Status: BackupStatus{
						Phase: BackupPhaseCompleted,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "backup-6",
					},
					Status: BackupStatus{
						Phase: BackupPhaseFailed,
					},
				},
			},
		}
		backupList.SortByName()

		pendingBackups := backupList.GetPendingBackupNames()
		Expect(pendingBackups).To(ConsistOf("backup-1", "backup-2"))
	})
})

var _ = Describe("backup_controller volumeSnapshot unit tests", func() {
	When("there's a running backup", func() {
		It("prevents concurrent backups", func() {
			backupList := BackupList{
				Items: []Backup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-2",
						},
						Status: BackupStatus{
							Phase: BackupPhaseRunning,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-3",
						},
					},
				},
			}

			// The currently running backup can be executed
			Expect(backupList.CanExecuteBackup("backup-1")).To(BeFalse())
			Expect(backupList.CanExecuteBackup("backup-2")).To(BeTrue())
			Expect(backupList.CanExecuteBackup("backup-3")).To(BeFalse())
		})
	})

	When("there are no running backups", func() {
		It("prevents concurrent backups", func() {
			backupList := BackupList{
				Items: []Backup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-2",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-3",
						},
					},
				},
			}

			// The currently running backup can be executed
			Expect(backupList.CanExecuteBackup("backup-1")).To(BeTrue())
			Expect(backupList.CanExecuteBackup("backup-2")).To(BeFalse())
			Expect(backupList.CanExecuteBackup("backup-3")).To(BeFalse())
		})
	})

	When("there are multiple running backups", func() {
		It("prevents concurrent backups", func() {
			// This could happen if there is a race condition, and in this case we use a
			// tie-breaker algorithm
			backupList := BackupList{
				Items: []Backup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-2",
						},
						Status: BackupStatus{
							Phase: BackupPhaseRunning,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-3",
						},
						Status: BackupStatus{
							Phase: BackupPhaseRunning,
						},
					},
				},
			}

			// The currently running backup can be executed
			Expect(backupList.CanExecuteBackup("backup-1")).To(BeFalse())
			Expect(backupList.CanExecuteBackup("backup-2")).To(BeTrue())
			Expect(backupList.CanExecuteBackup("backup-3")).To(BeFalse())
		})
	})

	When("there is a complete backup", func() {
		It("prevents concurrent backups", func() {
			backupList := BackupList{
				Items: []Backup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-1",
						},
						Status: BackupStatus{
							Phase: BackupPhaseCompleted,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-2",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-3",
						},
					},
				},
			}

			// The currently running backup can be executed
			Expect(backupList.CanExecuteBackup("backup-1")).To(BeFalse())
			Expect(backupList.CanExecuteBackup("backup-2")).To(BeTrue())
			Expect(backupList.CanExecuteBackup("backup-3")).To(BeFalse())
		})
	})
})
