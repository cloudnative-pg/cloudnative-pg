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
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup_controller barmanObjectStore unit tests", func() {
	var env *testingEnvironment
	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	Context("isValidBackupRunning works correctly", func() {
		const (
			clusterPrimary = "cluster-example-1"
			containerID    = "container-id"
		)

		var cluster *apiv1.Cluster
		var backup *apiv1.Backup
		var pod *corev1.Pod

		BeforeEach(func(ctx context.Context) {
			namespace := newFakeNamespace(env.client)

			cluster = &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Status: apiv1.ClusterStatus{
					TargetPrimary: clusterPrimary,
				},
			}
			err := env.backupReconciler.Create(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

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
			err = env.backupReconciler.Create(ctx, backup)
			Expect(err).ToNot(HaveOccurred())

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
			err = env.backupReconciler.Create(ctx, pod)
			Expect(err).ToNot(HaveOccurred())

			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:        "postgres",
						ContainerID: containerID,
					},
				},
			}
			err = env.backupReconciler.Status().Update(ctx, pod)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returning true when a backup is in running phase (primary)", func(ctx context.Context) {
			backup.Spec.Target = apiv1.BackupTargetPrimary
			res, err := env.backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeTrue())
		})

		It("returning true when a backup is in running phase (standby)", func(ctx context.Context) {
			backup.Spec.Target = apiv1.BackupTargetStandby
			res, err := env.backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeTrue())
		})

		It("returning false when a backup has no Phase or InstanceID", func(ctx context.Context) {
			backup.Status.Phase = ""
			backup.Status.InstanceID = nil
			res, err := env.backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeFalse())
		})

		It("returning false if the elected backup pod is inactive", func(ctx context.Context) {
			pod.Status.Phase = corev1.PodFailed
			err := env.backupReconciler.Status().Update(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			res, err := env.backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeFalse())
		})

		It("returning false if the elected backup pod has been restarted", func(ctx context.Context) {
			backup.Status.InstanceID.ContainerID = containerID + "-new"
			res, err := env.backupReconciler.isValidBackupRunning(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeFalse())
		})

		It("returning an error when the backup target is wrong", func(ctx context.Context) {
			backup.Spec.Target = "fakeTarget"
			res, err := env.backupReconciler.isValidBackupRunning(ctx, backup, cluster)
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

var _ = Describe("update snapshot backup metadata", func() {
	var (
		env           *testingEnvironment
		snapshots     volumesnapshotv1.VolumeSnapshotList
		cluster       *apiv1.Cluster
		now           = metav1.NewTime(time.Now().Local().Truncate(time.Second))
		oneHourAgo    = metav1.NewTime(now.Add(-1 * time.Hour))
		twoHoursAgo   = metav1.NewTime(now.Add(-2 * time.Hour))
		threeHoursAgo = metav1.NewTime(now.Add(-3 * time.Hour))
	)

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace := newFakeNamespace(env.client)
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: namespace,
			},
			Status: apiv1.ClusterStatus{
				TargetPrimary: "cluster-example-2",
			},
		}
		snapshots = volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{ObjectMeta: metav1.ObjectMeta{
					Name:      "snapshot-0",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.BackupEndTimeAnnotationName: threeHoursAgo.Format(time.RFC3339),
						utils.PvcRoleLabelName:            string(utils.PVCRolePgData),
					},
					Labels: map[string]string{
						utils.ClusterLabelName: "DIFFERENT-CLUSTER",
					},
				}},
				{ObjectMeta: metav1.ObjectMeta{
					Name:      "snapshot-01",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.BackupEndTimeAnnotationName: threeHoursAgo.Format(time.RFC3339),
						utils.PvcRoleLabelName:            string(utils.PVCRolePgWal),
					},
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
					},
				}},
				{ObjectMeta: metav1.ObjectMeta{
					Name:      "snapshot-1",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.BackupEndTimeAnnotationName: twoHoursAgo.Format(time.RFC3339),
						utils.PvcRoleLabelName:            string(utils.PVCRolePgData),
					},
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
					},
				}},
				{ObjectMeta: metav1.ObjectMeta{
					Name:      "snapshot-2",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.BackupEndTimeAnnotationName: oneHourAgo.Format(time.RFC3339),
						utils.PvcRoleLabelName:            string(utils.PVCRolePgData),
					},
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
					},
				}},
			},
		}
	})

	It("should update cluster with no metadata", func(ctx context.Context) {
		//nolint:staticcheck
		Expect(cluster.Status.FirstRecoverabilityPoint).To(BeEmpty())
		//nolint:staticcheck
		Expect(cluster.Status.FirstRecoverabilityPointByMethod).To(BeEmpty())
		//nolint:staticcheck
		Expect(cluster.Status.LastSuccessfulBackup).To(BeEmpty())
		//nolint:staticcheck
		Expect(cluster.Status.LastSuccessfulBackupByMethod).To(BeEmpty())
		fakeClient := fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			WithLists(&snapshots).Build()

		err := updateClusterWithSnapshotsBackupTimes(ctx, fakeClient, cluster.Namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())

		var updatedCluster apiv1.Cluster
		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		}, &updatedCluster)
		Expect(err).ToNot(HaveOccurred())
		//nolint:staticcheck
		Expect(updatedCluster.Status.FirstRecoverabilityPoint).To(Equal(twoHoursAgo.Format(time.RFC3339)))
		//nolint:staticcheck
		Expect(updatedCluster.Status.FirstRecoverabilityPointByMethod).
			ToNot(HaveKey(apiv1.BackupMethodBarmanObjectStore))
		//nolint:staticcheck
		Expect(updatedCluster.Status.FirstRecoverabilityPointByMethod[apiv1.BackupMethodVolumeSnapshot]).
			To(Equal(twoHoursAgo))
		//nolint:staticcheck
		Expect(updatedCluster.Status.LastSuccessfulBackup).To(Equal(oneHourAgo.Format(time.RFC3339)))
		//nolint:staticcheck
		Expect(updatedCluster.Status.LastSuccessfulBackupByMethod).
			ToNot(HaveKey(apiv1.BackupMethodBarmanObjectStore))
		//nolint:staticcheck
		Expect(updatedCluster.Status.LastSuccessfulBackupByMethod[apiv1.BackupMethodVolumeSnapshot]).
			To(Equal(oneHourAgo))
	})

	It("should consider other methods when update the metadata", func(ctx context.Context) {
		//nolint:staticcheck
		cluster.Status.FirstRecoverabilityPoint = threeHoursAgo.Format(time.RFC3339)
		//nolint:staticcheck
		cluster.Status.FirstRecoverabilityPointByMethod = map[apiv1.BackupMethod]metav1.Time{
			apiv1.BackupMethodBarmanObjectStore: threeHoursAgo,
		}
		//nolint:staticcheck
		cluster.Status.LastSuccessfulBackup = now.Format(time.RFC3339)
		//nolint:staticcheck
		cluster.Status.LastSuccessfulBackupByMethod = map[apiv1.BackupMethod]metav1.Time{
			apiv1.BackupMethodBarmanObjectStore: now,
		}
		fakeClient := fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			WithLists(&snapshots).Build()

		err := updateClusterWithSnapshotsBackupTimes(ctx, fakeClient, cluster.Namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())

		var updatedCluster apiv1.Cluster
		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		}, &updatedCluster)
		Expect(err).ToNot(HaveOccurred())
		//nolint:staticcheck
		Expect(updatedCluster.Status.FirstRecoverabilityPoint).To(Equal(threeHoursAgo.Format(time.RFC3339)))
		//nolint:staticcheck
		Expect(updatedCluster.Status.FirstRecoverabilityPointByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(threeHoursAgo))
		//nolint:staticcheck
		Expect(updatedCluster.Status.FirstRecoverabilityPointByMethod[apiv1.BackupMethodVolumeSnapshot]).
			To(Equal(twoHoursAgo))
		//nolint:staticcheck
		Expect(updatedCluster.Status.LastSuccessfulBackup).To(Equal(now.Format(time.RFC3339)))
		//nolint:staticcheck
		Expect(updatedCluster.Status.LastSuccessfulBackupByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(now))
		//nolint:staticcheck
		Expect(updatedCluster.Status.LastSuccessfulBackupByMethod[apiv1.BackupMethodVolumeSnapshot]).
			To(Equal(oneHourAgo))
	})

	It("should override other method metadata when appropriate", func(ctx context.Context) {
		//nolint:staticcheck
		cluster.Status.FirstRecoverabilityPoint = oneHourAgo.Format(time.RFC3339)
		//nolint:staticcheck
		cluster.Status.FirstRecoverabilityPointByMethod = map[apiv1.BackupMethod]metav1.Time{
			apiv1.BackupMethodBarmanObjectStore: oneHourAgo,
			apiv1.BackupMethodVolumeSnapshot:    now,
		}
		//nolint:staticcheck
		cluster.Status.LastSuccessfulBackup = oneHourAgo.Format(time.RFC3339)
		//nolint:staticcheck
		cluster.Status.LastSuccessfulBackupByMethod = map[apiv1.BackupMethod]metav1.Time{
			apiv1.BackupMethodBarmanObjectStore: twoHoursAgo,
			apiv1.BackupMethodVolumeSnapshot:    threeHoursAgo,
		}
		fakeClient := fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			WithLists(&snapshots).Build()

		err := updateClusterWithSnapshotsBackupTimes(ctx, fakeClient, cluster.Namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())

		var updatedCluster apiv1.Cluster
		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		}, &updatedCluster)
		Expect(err).ToNot(HaveOccurred())
		//nolint:staticcheck
		Expect(updatedCluster.Status.FirstRecoverabilityPoint).To(Equal(twoHoursAgo.Format(time.RFC3339)))
		//nolint:staticcheck
		Expect(updatedCluster.Status.FirstRecoverabilityPointByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(oneHourAgo))
		//nolint:staticcheck
		Expect(updatedCluster.Status.FirstRecoverabilityPointByMethod[apiv1.BackupMethodVolumeSnapshot]).
			To(Equal(twoHoursAgo))
		//nolint:staticcheck
		Expect(updatedCluster.Status.LastSuccessfulBackup).To(Equal(oneHourAgo.Format(time.RFC3339)))
		//nolint:staticcheck
		Expect(updatedCluster.Status.LastSuccessfulBackupByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(twoHoursAgo))
		//nolint:staticcheck
		Expect(updatedCluster.Status.LastSuccessfulBackupByMethod[apiv1.BackupMethodVolumeSnapshot]).
			To(Equal(oneHourAgo))
	})
})

var _ = Describe("checkPrerequisites for plugin backups", func() {
	var env *testingEnvironment
	BeforeEach(func() { env = buildTestEnvironment() })

	It("allows plugin backups without cluster.spec.backup when a plugin is configured", func(ctx context.Context) {
		ns := newFakeNamespace(env.client)

		cluster := newFakeCNPGCluster(env.client, ns, func(c *apiv1.Cluster) {
			c.Spec.Backup = nil
			c.Spec.Plugins = []apiv1.PluginConfiguration{{
				Name:       "test",
				Enabled:    ptr.To(true),
				Parameters: map[string]string{"key": "value"},
			}}
		})

		backup := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{Name: "test-plugin-backup", Namespace: ns},
			Spec: apiv1.BackupSpec{
				Cluster: apiv1.LocalObjectReference{Name: cluster.Name},
				Method:  apiv1.BackupMethodPlugin,
			},
		}
		// Create the backup so that status updates in prerequisites can patch it if needed
		expectErr := env.client.Create(ctx, backup)
		Expect(expectErr).ToNot(HaveOccurred())

		res, err := env.backupReconciler.checkPrerequisites(ctx, *backup, *cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeNil())

		// Ensure backup was not marked as failed
		var stored apiv1.Backup
		expectErr = env.client.Get(ctx, client.ObjectKeyFromObject(backup), &stored)
		Expect(expectErr).ToNot(HaveOccurred())
		Expect(stored.Status.Phase).To(BeEmpty())
	})

	It("fails plugin backups when no plugin is configured on the cluster", func(ctx context.Context) {
		ns := newFakeNamespace(env.client)

		cluster := newFakeCNPGCluster(env.client, ns, func(c *apiv1.Cluster) {
			c.Spec.Backup = nil
			c.Spec.Plugins = nil
		})

		backup := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{Name: "test-plugin-backup-missing", Namespace: ns},
			Spec: apiv1.BackupSpec{
				Cluster: apiv1.LocalObjectReference{Name: cluster.Name},
				Method:  apiv1.BackupMethodPlugin,
			},
		}
		expectErr := env.client.Create(ctx, backup)
		Expect(expectErr).ToNot(HaveOccurred())

		res, err := env.backupReconciler.checkPrerequisites(ctx, *backup, *cluster)
		// We expect the reconciler to flag failure and return a non-nil result without bubbling an error
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())

		var stored apiv1.Backup
		expectErr = env.client.Get(ctx, client.ObjectKeyFromObject(backup), &stored)
		Expect(expectErr).ToNot(HaveOccurred())
		Expect(stored.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseFailed))
		Expect(stored.Status.Method).To(BeEquivalentTo(apiv1.BackupMethodPlugin))
	})
})
