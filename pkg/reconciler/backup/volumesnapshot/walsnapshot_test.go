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

package volumesnapshot

import (
	"context"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeFinalizeExecutor completes an online backup
type fakeFinalizeExecutor struct{}

func (f fakeFinalizeExecutor) prepare(
	context.Context, *apiv1.Cluster, *apiv1.Backup, *corev1.Pod,
) (*ctrl.Result, error) {
	return nil, nil
}

func (f fakeFinalizeExecutor) finalize(
	context.Context, *apiv1.Cluster, *apiv1.Backup, *corev1.Pod,
) (*ctrl.Result, error) {
	return nil, nil
}

var _ = Describe("WAL snapshot skipping", func() {
	const (
		namespace   = "test-namespace"
		clusterName = "cluster-example"
		backupName  = "backup-example"
	)
	var (
		cluster   *apiv1.Cluster
		backup    *apiv1.Backup
		targetPod *corev1.Pod
		pvcs      []corev1.PersistentVolumeClaim
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Backup: &apiv1.BackupConfiguration{
					VolumeSnapshot: &apiv1.VolumeSnapshotConfiguration{
						ClassName: "csi-hostpath-snapclass",
						Online:    ptr.To(true),
					},
				},
				Plugins: []apiv1.PluginConfiguration{
					{Name: "barman-cloud.cloudnative-pg.io", IsWALArchiver: ptr.To(true)},
				},
			},
		}
		backup = &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: backupName},
			Spec:       apiv1.BackupSpec{Method: apiv1.BackupMethodVolumeSnapshot},
			Status:     apiv1.BackupStatus{MajorVersion: 18},
		}
		targetPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName + "-1"},
		}
		pvcs = []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      clusterName + "-1",
					Labels:    map[string]string{utils.PvcRoleLabelName: string(utils.PVCRolePgData)},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      clusterName + "-1-wal",
					Labels:    map[string]string{utils.PvcRoleLabelName: string(utils.PVCRolePgWal)},
				},
			},
		}
	})

	It("skips the WAL snapshot of an online backup", func() {
		Expect(isWalSnapshotSkipped(cluster, backup)).To(BeTrue())
	})

	DescribeTable("keeps the WAL snapshot",
		func(mutate func(*apiv1.Cluster, *apiv1.Backup)) {
			mutate(cluster, backup)
			Expect(isWalSnapshotSkipped(cluster, backup)).To(BeFalse())
		},
		Entry("for a cold backup", func(_ *apiv1.Cluster, b *apiv1.Backup) {
			b.Spec.Online = ptr.To(false)
		}),
		Entry("when pg_backup_stop doesn't wait for the archive", func(c *apiv1.Cluster, _ *apiv1.Backup) {
			c.Spec.Backup.VolumeSnapshot.OnlineConfiguration.WaitForArchive = ptr.To(false)
		}),
		Entry("without a WAL archiver plugin", func(c *apiv1.Cluster, _ *apiv1.Backup) {
			c.Spec.Plugins[0].IsWALArchiver = ptr.To(false)
		}),
		Entry("when the WAL archiver plugin is disabled", func(c *apiv1.Cluster, _ *apiv1.Backup) {
			c.Spec.Plugins[0].Enabled = ptr.To(false)
		}),
		Entry("when the Backup doesn't wait for the archive", func(_ *apiv1.Cluster, b *apiv1.Backup) {
			b.Spec.OnlineConfiguration = &apiv1.OnlineConfiguration{WaitForArchive: ptr.To(false)}
		}),
	)

	newExecutor := func(stopWaitsForArchive bool) *Reconciler {
		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(backup, cluster, targetPod).
			Build()
		executor := NewReconcilerBuilder(mockClient, record.NewFakeRecorder(3)).Build()
		executor.backupClient = &fakeBackupClient{response: &webserver.Response[webserver.BackupResultData]{
			Data: &webserver.BackupResultData{
				BackupName:          backupName,
				Phase:               webserver.Started,
				StopWaitsForArchive: stopWaitsForArchive,
			},
		}}
		return executor
	}

	It("creates and marks only the PGDATA snapshot", func(ctx SpecContext) {
		executor := newExecutor(true)

		Expect(executor.createSnapshotPVCGroupStep(ctx, cluster, pvcs, backup, targetPod)).To(Succeed())

		var snapshotList volumesnapshotv1.VolumeSnapshotList
		Expect(executor.cli.List(ctx, &snapshotList)).To(Succeed())
		Expect(snapshotList.Items).To(HaveLen(1))
		Expect(snapshotList.Items[0].Name).To(Equal(backupName))
		Expect(snapshotList.Items[0].Annotations).To(
			HaveKeyWithValue(utils.WalSnapshotSkippedAnnotationName, "true"))
	})

	It("doesn't mark the snapshot of an instance without a WAL PVC", func(ctx SpecContext) {
		executor := newExecutor(true)

		Expect(executor.createSnapshotPVCGroupStep(ctx, cluster, pvcs[:1], backup, targetPod)).To(Succeed())

		var snapshotList volumesnapshotv1.VolumeSnapshotList
		Expect(executor.cli.List(ctx, &snapshotList)).To(Succeed())
		Expect(snapshotList.Items).To(HaveLen(1))
		Expect(snapshotList.Items[0].Annotations).ToNot(HaveKey(utils.WalSnapshotSkippedAnnotationName))
	})

	It("creates unmarked snapshots of every PVC when pg_backup_stop doesn't wait for the archiver",
		func(ctx SpecContext) {
			executor := newExecutor(false)

			Expect(executor.createSnapshotPVCGroupStep(ctx, cluster, pvcs, backup, targetPod)).To(Succeed())

			var snapshotList volumesnapshotv1.VolumeSnapshotList
			Expect(executor.cli.List(ctx, &snapshotList)).To(Succeed())
			Expect(snapshotList.Items).To(HaveLen(2))
			for _, snapshot := range snapshotList.Items {
				Expect(snapshot.Annotations).ToNot(HaveKey(utils.WalSnapshotSkippedAnnotationName))
			}
		})

	Context("when finalizing", func() {
		var executor *Reconciler

		BeforeEach(func() {
			snapshot := volumesnapshotv1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      backupName,
					Labels:    map[string]string{utils.BackupNameLabelName: backupName},
					Annotations: map[string]string{
						utils.PvcRoleLabelName:                 string(utils.PVCRolePgData),
						utils.WalSnapshotSkippedAnnotationName: "true",
					},
				},
			}
			mockClient := fake.NewClientBuilder().
				WithScheme(scheme.BuildWithAllKnownScheme()).
				WithObjects(backup, cluster, targetPod, &snapshot).
				WithStatusSubresource(backup).
				Build()
			executor = NewReconcilerBuilder(mockClient, record.NewFakeRecorder(3)).Build()
		})

		It("marks the backup status", func(ctx SpecContext) {
			res, err := executor.finalizeSnapshotBackupStep(ctx, fakeFinalizeExecutor{}, cluster, backup, targetPod)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeNil())

			var latestBackup apiv1.Backup
			Expect(executor.cli.Get(ctx, types.NamespacedName{Namespace: namespace, Name: backupName},
				&latestBackup)).To(Succeed())
			Expect(latestBackup.Status.BackupSnapshotStatus.WalSnapshotSkipped).To(BeTrue())
		})

		It("fails when the WAL archiver plugin was removed during the backup", func(ctx SpecContext) {
			cluster.Spec.Plugins = nil
			_, err := executor.finalizeSnapshotBackupStep(ctx, fakeFinalizeExecutor{}, cluster, backup, targetPod)
			Expect(err).To(MatchError(ContainSubstring("WAL archiver plugin was removed")))
		})
	})
})
