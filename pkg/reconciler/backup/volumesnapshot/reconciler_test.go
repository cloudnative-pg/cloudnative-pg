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

package volumesnapshot

import (
	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Volumesnapshot reconciler", func() {
	const (
		namespace   = "test-namespace"
		clusterName = "clusterName"
		backupName  = "theBakcup"
	)
	var (
		cluster   *apiv1.Cluster
		targetPod *v1.Pod
		pvcs      []v1.PersistentVolumeClaim
		backup    *apiv1.Backup
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      clusterName,
			},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					VolumeSnapshot: &apiv1.VolumeSnapshotConfiguration{
						ClassName: "csi-hostpath-snapclass",
					},
				},
			},
		}
		targetPod = &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      clusterName + "-2",
			},
		}
		pvcs = []v1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-2",
					Namespace: namespace,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-2-wal",
					Namespace: namespace,
				},
			},
		}
		backup = &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      backupName,
			},
		}
	})

	It("should fence the target pod when there are no volumesnapshots", func(ctx SpecContext) {
		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, targetPod).
			Build()
		fakeRecorder := record.NewFakeRecorder(3)

		executor := NewExecutorBuilder(mockClient, fakeRecorder).
			FenceInstance(true).
			Build()

		result, err := executor.Execute(ctx, cluster, backup, targetPod, pvcs)
		Expect(err).ToNot(HaveOccurred())
		// we should have requested creation of snapshots, and so we'd return to
		// wait for them to be ready
		Expect(result).ToNot(BeNil())
		hasEvent := false
		var events string
		go func() {
			evts := <-fakeRecorder.Events
			hasEvent = true
			events = evts
		}()
		Eventually(func() bool {
			return hasEvent
		}).Should(BeTrue())
		Eventually(func() string {
			return events
		}).Should(ContainSubstring("Requesting fencing for Pod " + targetPod.Name))
	})

	It("should not fence the target pod when there are existing volumesnapshots", func(ctx SpecContext) {
		snapshots := storagesnapshotv1.VolumeSnapshotList{
			Items: []storagesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      clusterName + "-2",
						Labels: map[string]string{
							utils.BackupNameLabelName: backupName,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      clusterName + "-2-wal",
						Labels: map[string]string{
							utils.BackupNameLabelName: backupName,
						},
					},
				},
			},
		}

		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, targetPod).
			WithLists(&snapshots).
			Build()
		fakeRecorder := record.NewFakeRecorder(3)

		executor := NewExecutorBuilder(mockClient, fakeRecorder).
			FenceInstance(true).
			Build()

		result, err := executor.Execute(ctx, cluster, backup, targetPod, pvcs)
		Expect(err).ToNot(HaveOccurred())
		// we should have found snapshots that are not ready, and so we'd return to
		// wait for them to be ready
		Expect(result).ToNot(BeNil())
		hasEvent := false
		var events string
		go func() {
			evts := <-fakeRecorder.Events
			hasEvent = true
			events = evts
		}()
		Consistently(func() bool {
			return hasEvent
		}).Should(BeFalse())
		Consistently(func() string {
			return events
		}).Should(BeEmpty())
	})

	It("should unfence the target pod when the snapshots are ready", func(ctx SpecContext) {
		snapshots := storagesnapshotv1.VolumeSnapshotList{
			Items: []storagesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      clusterName + "-2",
						Labels: map[string]string{
							utils.BackupNameLabelName: backupName,
						},
					},
					Status: &storagesnapshotv1.VolumeSnapshotStatus{
						ReadyToUse: ptr.To(true),
						Error:      nil,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      clusterName + "-2-wal",
						Labels: map[string]string{
							utils.BackupNameLabelName: backupName,
						},
					},
					Status: &storagesnapshotv1.VolumeSnapshotStatus{
						ReadyToUse: ptr.To(true),
						Error:      nil,
					},
				},
			},
		}

		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, targetPod).
			WithLists(&snapshots).
			Build()
		fakeRecorder := record.NewFakeRecorder(3)

		executor := NewExecutorBuilder(mockClient, fakeRecorder).
			FenceInstance(true).
			Build()

		result, err := executor.Execute(ctx, cluster, backup, targetPod, pvcs)
		Expect(err).ToNot(HaveOccurred())
		// we should have found snapshots that are ready, and so the result
		// should be nil
		Expect(result).To(BeNil())
		hasEvent := false
		var events string
		go func() {
			evts := <-fakeRecorder.Events
			hasEvent = true
			events = evts
		}()
		Eventually(func() bool {
			return hasEvent
		}).Should(BeTrue())
		Eventually(func() string {
			return events
		}).Should(ContainSubstring("Un-fencing Pod " + targetPod.Name))
	})
})
