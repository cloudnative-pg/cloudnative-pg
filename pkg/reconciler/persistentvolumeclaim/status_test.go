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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVC detection", func() {
	It("will list PVCs with Jobs or Pods or which are Ready", func(ctx SpecContext) {
		clusterName := "myCluster"
		makeClusterPVC := func(serial string, isResizing bool) corev1.PersistentVolumeClaim {
			return makePVC(clusterName, serial, serial, NewPgDataCalculator(), isResizing)
		}
		pvcs := []corev1.PersistentVolumeClaim{
			makeClusterPVC("1", false), // has a Pod
			makeClusterPVC("2", false), // has a Job
			makeClusterPVC("3", true),  // resizing, has a Pod so stays "resizing" (not affected by #9786 fix)
			makeClusterPVC("4", false), // dangling
		}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		}
		EnrichStatus(
			ctx,
			cluster,
			[]corev1.Pod{
				makePod(clusterName, "1", specs.ClusterRoleLabelPrimary),
				makePod(clusterName, "3", specs.ClusterRoleLabelReplica),
			},
			[]batchv1.Job{makeJob(clusterName, "2")},
			pvcs,
		)

		Expect(cluster.Status.PVCCount).Should(BeEquivalentTo(4))
		Expect(cluster.Status.InstanceNames).Should(Equal([]string{
			clusterName + "-1",
			clusterName + "-2",
			clusterName + "-3",
			clusterName + "-4",
		}))
		Expect(cluster.Status.InitializingPVC).Should(Equal([]string{
			clusterName + "-2",
		}))
		Expect(cluster.Status.ResizingPVC).Should(Equal([]string{
			clusterName + "-3",
		}))
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{
			clusterName + "-4",
		}))
		Expect(cluster.Status.HealthyPVC).Should(Equal([]string{
			clusterName + "-1",
		}))
		Expect(cluster.Status.UnusablePVC).Should(BeEmpty())
	})
})

var _ = Describe("PVC classification with resizing PVCs", func() {
	clusterName := "myCluster"
	makeCluster := func() *apiv1.Cluster {
		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		}
	}

	It("classifies resizing PVC without pod and with FileSystemResizePending as dangling", func(ctx SpecContext) {
		pvc := makePVC(clusterName, "1", "1", NewPgDataCalculator(), true)
		pvc.Status.Conditions = append(pvc.Status.Conditions, corev1.PersistentVolumeClaimCondition{
			Type: corev1.PersistentVolumeClaimFileSystemResizePending, Status: corev1.ConditionTrue,
		})
		cluster := makeCluster()
		EnrichStatus(ctx, cluster, []corev1.Pod{}, []batchv1.Job{}, []corev1.PersistentVolumeClaim{pvc})
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.ResizingPVC).Should(BeEmpty())
	})

	It("classifies resizing PVC without pod and without FileSystemResizePending as dangling", func(ctx SpecContext) {
		pvc := makePVC(clusterName, "1", "1", NewPgDataCalculator(), true)
		cluster := makeCluster()
		EnrichStatus(ctx, cluster, []corev1.Pod{}, []batchv1.Job{}, []corev1.PersistentVolumeClaim{pvc})
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.ResizingPVC).Should(BeEmpty())
	})

	// isResizing check takes precedence over hasJob: a resizing PVC with
	// a Job but no Pod is classified as dangling, not initializing.
	It("classifies resizing PVC with a job but no pod as dangling", func(ctx SpecContext) {
		pvc := makePVC(clusterName, "1", "1", NewPgDataCalculator(), true)
		cluster := makeCluster()
		EnrichStatus(
			ctx,
			cluster,
			[]corev1.Pod{},
			[]batchv1.Job{makeJob(clusterName, "1")},
			[]corev1.PersistentVolumeClaim{pvc},
		)
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.ResizingPVC).Should(BeEmpty())
		Expect(cluster.Status.InitializingPVC).Should(BeEmpty())
	})

	It("classifies resizing PVC as dangling when pod was deleted during rollout (#9786)", func(ctx SpecContext) {
		// Simulate simultaneous storage + resource change:
		// instance-1 has a running pod (primary), instance-2's pod was deleted
		// for a rolling update while both PVCs are resizing.
		pvc1 := makePVC(clusterName, "1", "1", NewPgDataCalculator(), true) // resizing, has pod
		pvc2 := makePVC(clusterName, "2", "2", NewPgDataCalculator(), true) // resizing, pod deleted
		cluster := makeCluster()
		EnrichStatus(
			ctx,
			cluster,
			[]corev1.Pod{makePod(clusterName, "1", specs.ClusterRoleLabelPrimary)},
			[]batchv1.Job{},
			[]corev1.PersistentVolumeClaim{pvc1, pvc2},
		)
		Expect(cluster.Status.ResizingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{clusterName + "-2"}))
		Expect(cluster.Status.Instances).Should(BeEquivalentTo(2))
	})
})

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
