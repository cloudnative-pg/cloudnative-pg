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

package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup_controller barmanObjectStore unit tests", func() {
	Context("isValidBackupRunning works correctly", func() {
		const (
			clusterPrimary = "cluster-example-1"
			containerID    = "container-id"
		)

		var cluster *apiv1.Cluster
		var backup *apiv1.Backup
		var pod *corev1.Pod

		BeforeEach(func(ctx context.Context) {
			namespace := newFakeNamespace()

			cluster = &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Status: apiv1.ClusterStatus{
					TargetPrimary: clusterPrimary,
				},
			}

			backup = &apiv1.Backup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				},
				Spec: apiv1.BackupSpec{
					Cluster: apiv1.LocalObjectReference{
						Name: cluster.Name,
					},
				},
				Status: apiv1.BackupStatus{
					Phase: apiv1.BackupPhaseRunning,
					InstanceID: &apiv1.InstanceID{
						PodName:     clusterPrimary,
						ContainerID: containerID,
					},
				},
			}

			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterPrimary,
					Namespace: cluster.Namespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "postgres",
							Image: "postgres",
						},
					},
				},
			}
			err := backupReconciler.Create(ctx, pod)
			Expect(err).ToNot(HaveOccurred())

			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						ContainerID: containerID,
					},
				},
			}
			err = backupReconciler.Status().Update(ctx, pod)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returning true when a backup is in running phase (primary)", func(ctx context.Context) {
			backup.Spec.Target = apiv1.BackupTargetPrimary
			res, err := backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeTrue())
		})

		It("returning true when a backup is in running phase (standby)", func(ctx context.Context) {
			backup.Spec.Target = apiv1.BackupTargetStandby
			res, err := backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeTrue())
		})

		It("returning false when a backup has no Phase or InstanceID", func(ctx context.Context) {
			backup.Status.Phase = ""
			backup.Status.InstanceID = nil
			res, err := backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeFalse())
		})

		It("returning false if the elected backup pod is inactive", func(ctx context.Context) {
			pod.Status.Phase = corev1.PodFailed
			err := backupReconciler.Status().Update(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			res, err := backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeFalse())
		})

		It("returning false if the elected backup pod has been restarted", func(ctx context.Context) {
			backup.Status.InstanceID.ContainerID = containerID + "-new"
			res, err := backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeFalse())
		})

		It("returning an error when the backup target is wrong", func(ctx context.Context) {
			backup.Spec.Target = "fakeTarget"
			res, err := backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).To(HaveOccurred())
			Expect(res).To(BeFalse())
		})
	})
})

var _ = Describe("backup_controller volumeSnapshot unit tests", func() {
	When("there's a running backup", func() {
		It("prevents concurrent backups", func() {
			backupList := apiv1.BackupList{
				Items: []apiv1.Backup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-2",
						},
						Status: apiv1.BackupStatus{
							Phase: apiv1.BackupPhaseRunning,
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
			backupList := apiv1.BackupList{
				Items: []apiv1.Backup{
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
			backupList := apiv1.BackupList{
				Items: []apiv1.Backup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-2",
						},
						Status: apiv1.BackupStatus{
							Phase: apiv1.BackupPhaseRunning,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backup-3",
						},
						Status: apiv1.BackupStatus{
							Phase: apiv1.BackupPhaseRunning,
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
