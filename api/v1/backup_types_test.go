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
		status.BackupSnapshotStatus.SetSnapshotList([]*volumesnapshot.VolumeSnapshot{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-example-snapshot-1",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-example-snapshot-2",
				},
			},
		})

		Expect(status.BackupSnapshotStatus.Snapshots).To(HaveLen(2))
		Expect(status.BackupSnapshotStatus.Snapshots).To(ConsistOf(
			"cluster-example-snapshot-1",
			"cluster-example-snapshot-2"))
	})
})
