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

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func fakeClientWithObjects(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme.BuildWithAllKnownScheme()).
		WithObjects(objs...).
		Build()
}

func makePendingPVCWithSnapshotDataSource(name, snapshotName string) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: storageSourceTestNamespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshotv1.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     snapshotName,
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
		},
	}
}

func makeBoundPVCWithSnapshotDataSource(name, snapshotName string) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: storageSourceTestNamespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshotv1.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     snapshotName,
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}
}

func makeJobUsingPVC(name, pvcName string) batchv1.Job {
	return batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: storageSourceTestNamespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						},
					},
				},
			},
		},
	}
}

var _ = Describe("DeletePVCsWithMissingVolumeSnapshots", func() {
	When("a Pending PVC references a VolumeSnapshot that no longer exists", func() {
		It("should delete the PVC", func(ctx context.Context) {
			pvc := makePendingPVCWithSnapshotDataSource("test-pvc-1", "deleted-snapshot")
			c := fakeClientWithObjects(&pvc)

			result, err := DeletePVCsWithMissingVolumeSnapshots(
				ctx, c, []corev1.PersistentVolumeClaim{pvc}, nil,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeFalse())

			var remaining corev1.PersistentVolumeClaimList
			Expect(c.List(ctx, &remaining, client.InNamespace(storageSourceTestNamespace))).To(Succeed())
			Expect(remaining.Items).To(BeEmpty())
		})
	})

	When("a Pending PVC references a VolumeSnapshot that still exists", func() {
		It("should not delete the PVC", func(ctx context.Context) {
			pvc := makePendingPVCWithSnapshotDataSource("test-pvc-1", "existing-snapshot")
			snapshot := &volumesnapshotv1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-snapshot",
					Namespace: storageSourceTestNamespace,
				},
			}
			c := fakeClientWithObjects(&pvc, snapshot)

			result, err := DeletePVCsWithMissingVolumeSnapshots(
				ctx, c, []corev1.PersistentVolumeClaim{pvc}, nil,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())

			var remaining corev1.PersistentVolumeClaimList
			Expect(c.List(ctx, &remaining, client.InNamespace(storageSourceTestNamespace))).To(Succeed())
			Expect(remaining.Items).To(HaveLen(1))
		})
	})

	When("a Bound PVC references a VolumeSnapshot that no longer exists", func() {
		It("should not delete the PVC", func(ctx context.Context) {
			pvc := makeBoundPVCWithSnapshotDataSource("test-pvc-1", "deleted-snapshot")
			c := fakeClientWithObjects(&pvc)

			result, err := DeletePVCsWithMissingVolumeSnapshots(
				ctx, c, []corev1.PersistentVolumeClaim{pvc}, nil,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())

			var remaining corev1.PersistentVolumeClaimList
			Expect(c.List(ctx, &remaining, client.InNamespace(storageSourceTestNamespace))).To(Succeed())
			Expect(remaining.Items).To(HaveLen(1))
		})
	})

	When("a Pending PVC has no DataSource", func() {
		It("should not delete the PVC", func(ctx context.Context) {
			pvc := corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc-1",
					Namespace: storageSourceTestNamespace,
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase: corev1.ClaimPending,
				},
			}
			c := fakeClientWithObjects(&pvc)

			result, err := DeletePVCsWithMissingVolumeSnapshots(
				ctx, c, []corev1.PersistentVolumeClaim{pvc}, nil,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
		})
	})

	When("a Pending PVC references a deleted snapshot and has an associated Job", func() {
		It("should delete both the PVC and the Job", func(ctx context.Context) {
			pvc := makePendingPVCWithSnapshotDataSource("test-pvc-1", "deleted-snapshot")
			job := makeJobUsingPVC("restore-job", "test-pvc-1")
			c := fakeClientWithObjects(&pvc, &job)

			result, err := DeletePVCsWithMissingVolumeSnapshots(
				ctx, c, []corev1.PersistentVolumeClaim{pvc}, []batchv1.Job{job},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeFalse())

			var remainingPVCs corev1.PersistentVolumeClaimList
			Expect(c.List(ctx, &remainingPVCs, client.InNamespace(storageSourceTestNamespace))).To(Succeed())
			Expect(remainingPVCs.Items).To(BeEmpty())

			var remainingJobs batchv1.JobList
			Expect(c.List(ctx, &remainingJobs, client.InNamespace(storageSourceTestNamespace))).To(Succeed())
			Expect(remainingJobs.Items).To(BeEmpty())
		})
	})

	When("there are multiple PVCs and only one has a missing snapshot", func() {
		It("should delete only the stuck PVC and requeue", func(ctx context.Context) {
			snapshot := &volumesnapshotv1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-snapshot",
					Namespace: storageSourceTestNamespace,
				},
			}
			stuckPVC := makePendingPVCWithSnapshotDataSource("stuck-pvc", "deleted-snapshot")
			healthyPVC := makePendingPVCWithSnapshotDataSource("healthy-pvc", "existing-snapshot")
			c := fakeClientWithObjects(&stuckPVC, &healthyPVC, snapshot)

			result, err := DeletePVCsWithMissingVolumeSnapshots(
				ctx, c, []corev1.PersistentVolumeClaim{stuckPVC, healthyPVC}, nil,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeFalse())

			var remaining corev1.PersistentVolumeClaimList
			Expect(c.List(ctx, &remaining, client.InNamespace(storageSourceTestNamespace))).To(Succeed())
			Expect(remaining.Items).To(HaveLen(1))
			Expect(remaining.Items[0].Name).To(Equal("healthy-pvc"))
		})
	})
})
