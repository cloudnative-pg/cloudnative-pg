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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EnsureHealthyPVCsAnnotation", func() {
	var (
		ctx     context.Context
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		ctx = context.Background()
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Status: apiv1.ClusterStatus{
				HealthyPVC: []string{},
			},
		}
	})

	It("should mark healthy PVCs as ready", func() {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PVCStatusAnnotationName: StatusInitializing,
				},
			},
		}

		cluster.Status.HealthyPVC = []string{"test-pvc"}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc).
			Build()

		err := EnsureHealthyPVCsAnnotation(ctx, cli, cluster, []corev1.PersistentVolumeClaim{pvc})
		Expect(err).ToNot(HaveOccurred())

		// Verify the PVC was updated
		var updatedPVC corev1.PersistentVolumeClaim
		err = cli.Get(ctx, types.NamespacedName{Name: "test-pvc", Namespace: "default"}, &updatedPVC)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedPVC.Annotations[utils.PVCStatusAnnotationName]).To(Equal(StatusReady))
	})

	It("should skip PVCs already marked as ready", func() {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PVCStatusAnnotationName: StatusReady,
				},
			},
		}

		cluster.Status.HealthyPVC = []string{"test-pvc"}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc).
			Build()

		err := EnsureHealthyPVCsAnnotation(ctx, cli, cluster, []corev1.PersistentVolumeClaim{pvc})
		Expect(err).ToNot(HaveOccurred())
	})

	It("should return error if PVC is not found in the list", func() {
		cluster.Status.HealthyPVC = []string{"missing-pvc"}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			Build()

		err := EnsureHealthyPVCsAnnotation(ctx, cli, cluster, []corev1.PersistentVolumeClaim{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not find the pvc: missing-pvc"))
	})

	It("should handle multiple healthy PVCs", func() {
		pvc1 := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-1",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PVCStatusAnnotationName: StatusInitializing,
				},
			},
		}

		pvc2 := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-2",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PVCStatusAnnotationName: StatusDetached,
				},
			},
		}

		cluster.Status.HealthyPVC = []string{"test-pvc-1", "test-pvc-2"}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc1, &pvc2).
			Build()

		err := EnsureHealthyPVCsAnnotation(ctx, cli, cluster, []corev1.PersistentVolumeClaim{pvc1, pvc2})
		Expect(err).ToNot(HaveOccurred())

		// Verify both PVCs were updated
		var updatedPVC1 corev1.PersistentVolumeClaim
		err = cli.Get(ctx, types.NamespacedName{Name: "test-pvc-1", Namespace: "default"}, &updatedPVC1)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedPVC1.Annotations[utils.PVCStatusAnnotationName]).To(Equal(StatusReady))

		var updatedPVC2 corev1.PersistentVolumeClaim
		err = cli.Get(ctx, types.NamespacedName{Name: "test-pvc-2", Namespace: "default"}, &updatedPVC2)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedPVC2.Annotations[utils.PVCStatusAnnotationName]).To(Equal(StatusReady))
	})
})

var _ = Describe("MarkPVCReadyForCompletedJobs", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("should mark PVCs as ready when jobs are completed", func() {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PVCStatusAnnotationName: StatusInitializing,
				},
			},
		}

		job := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job",
				Namespace: "default",
			},
			Spec: batchv1.JobSpec{
				Completions: ptr.To(int32(1)),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							utils.JobRoleLabelName: "join",
						},
					},
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "pgdata",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "test-pvc",
									},
								},
							},
						},
					},
				},
			},
			Status: batchv1.JobStatus{
				Succeeded: 1,
			},
		}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc).
			Build()

		err := MarkPVCReadyForCompletedJobs(ctx, cli, []corev1.PersistentVolumeClaim{pvc}, []batchv1.Job{job})
		Expect(err).ToNot(HaveOccurred())

		// Verify the PVC was updated
		var updatedPVC corev1.PersistentVolumeClaim
		err = cli.Get(ctx, types.NamespacedName{Name: "test-pvc", Namespace: "default"}, &updatedPVC)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedPVC.Annotations[utils.PVCStatusAnnotationName]).To(Equal(StatusReady))
	})

	It("should skip PVCs already marked as ready", func() {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PVCStatusAnnotationName: StatusReady,
				},
			},
		}

		job := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job",
				Namespace: "default",
			},
			Spec: batchv1.JobSpec{
				Completions: ptr.To(int32(1)),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "pgdata",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "test-pvc",
									},
								},
							},
						},
					},
				},
			},
			Status: batchv1.JobStatus{
				Succeeded: 1,
			},
		}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc).
			Build()

		err := MarkPVCReadyForCompletedJobs(ctx, cli, []corev1.PersistentVolumeClaim{pvc}, []batchv1.Job{job})
		Expect(err).ToNot(HaveOccurred())
	})

	It("should do nothing when there are no completed jobs", func() {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PVCStatusAnnotationName: StatusInitializing,
				},
			},
		}

		job := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job",
				Namespace: "default",
			},
			Spec: batchv1.JobSpec{
				Completions: ptr.To(int32(1)),
			},
			Status: batchv1.JobStatus{
				Succeeded: 0,
			},
		}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc).
			Build()

		err := MarkPVCReadyForCompletedJobs(ctx, cli, []corev1.PersistentVolumeClaim{pvc}, []batchv1.Job{job})
		Expect(err).ToNot(HaveOccurred())

		// Verify the PVC was NOT updated
		var updatedPVC corev1.PersistentVolumeClaim
		err = cli.Get(ctx, types.NamespacedName{Name: "test-pvc", Namespace: "default"}, &updatedPVC)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedPVC.Annotations[utils.PVCStatusAnnotationName]).To(Equal(StatusInitializing))
	})

	It("should do nothing when jobs don't use the PVCs", func() {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PVCStatusAnnotationName: StatusInitializing,
				},
			},
		}

		job := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job",
				Namespace: "default",
			},
			Spec: batchv1.JobSpec{
				Completions: ptr.To(int32(1)),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "pgdata",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "other-pvc",
									},
								},
							},
						},
					},
				},
			},
			Status: batchv1.JobStatus{
				Succeeded: 1,
			},
		}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc).
			Build()

		err := MarkPVCReadyForCompletedJobs(ctx, cli, []corev1.PersistentVolumeClaim{pvc}, []batchv1.Job{job})
		Expect(err).ToNot(HaveOccurred())

		// Verify the PVC was NOT updated
		var updatedPVC corev1.PersistentVolumeClaim
		err = cli.Get(ctx, types.NamespacedName{Name: "test-pvc", Namespace: "default"}, &updatedPVC)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedPVC.Annotations[utils.PVCStatusAnnotationName]).To(Equal(StatusInitializing))
	})

	It("should handle multiple PVCs and jobs", func() {
		pvc1 := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-1",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PVCStatusAnnotationName: StatusInitializing,
				},
			},
		}

		pvc2 := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-2",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PVCStatusAnnotationName: StatusInitializing,
				},
			},
		}

		job1 := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job-1",
				Namespace: "default",
			},
			Spec: batchv1.JobSpec{
				Completions: ptr.To(int32(1)),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							utils.JobRoleLabelName: "join",
						},
					},
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "pgdata",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "test-pvc-1",
									},
								},
							},
						},
					},
				},
			},
			Status: batchv1.JobStatus{
				Succeeded: 1,
			},
		}

		job2 := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job-2",
				Namespace: "default",
			},
			Spec: batchv1.JobSpec{
				Completions: ptr.To(int32(1)),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							utils.JobRoleLabelName: "full-recovery",
						},
					},
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "pgdata",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "test-pvc-2",
									},
								},
							},
						},
					},
				},
			},
			Status: batchv1.JobStatus{
				Succeeded: 1,
			},
		}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc1, &pvc2).
			Build()

		err := MarkPVCReadyForCompletedJobs(
			ctx,
			cli,
			[]corev1.PersistentVolumeClaim{pvc1, pvc2},
			[]batchv1.Job{job1, job2},
		)
		Expect(err).ToNot(HaveOccurred())

		// Verify both PVCs were updated
		var updatedPVC1 corev1.PersistentVolumeClaim
		err = cli.Get(ctx, types.NamespacedName{Name: "test-pvc-1", Namespace: "default"}, &updatedPVC1)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedPVC1.Annotations[utils.PVCStatusAnnotationName]).To(Equal(StatusReady))

		var updatedPVC2 corev1.PersistentVolumeClaim
		err = cli.Get(ctx, types.NamespacedName{Name: "test-pvc-2", Namespace: "default"}, &updatedPVC2)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedPVC2.Annotations[utils.PVCStatusAnnotationName]).To(Equal(StatusReady))
	})
})
