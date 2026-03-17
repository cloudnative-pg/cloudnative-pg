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

package majorupgrade

import (
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Major upgrade job status reconciliation", func() {
	It("deletes the replica PVCs when and makes the cluster use the new image", func(ctx SpecContext) {
		job := buildCompletedUpgradeJob()
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-example",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:16",
			},
			Status: apiv1.ClusterStatus{
				TimelineID: 5, // Simulating pre-upgrade timeline
			},
		}
		pvcs := []corev1.PersistentVolumeClaim{
			buildPrimaryPVC(1),
			buildReplicaPVC(2),
			buildReplicaPVC(3),
		}

		objects := make([]runtime.Object, 0, 2+len(pvcs))
		objects = append(objects,
			job,
			cluster,
		)
		for i := range pvcs {
			objects = append(objects, &pvcs[i])
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithRuntimeObjects(objects...).
			WithStatusSubresource(cluster).
			Build()

		result, err := majorVersionUpgradeHandleCompletion(ctx, fakeClient, cluster, job, pvcs)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(*result).To(Equal(ctrl.Result{Requeue: true}))

		// the replica PVCs have been deleted
		for i := range pvcs {
			if !specs.IsPrimary(pvcs[i].ObjectMeta) {
				var pvc corev1.PersistentVolumeClaim
				err := fakeClient.Get(ctx, client.ObjectKeyFromObject(&pvcs[i]), &pvc)
				Expect(err).To(MatchError(errors.IsNotFound, "is not found"))
			}
		}

		// the upgrade has been marked as done
		Expect(cluster.Status.PGDataImageInfo.Image).To(Equal("postgres:16"))
		Expect(cluster.Status.PGDataImageInfo.MajorVersion).To(Equal(16))

		// the timeline ID has been reset to 1 to match pg_upgrade behavior
		Expect(cluster.Status.TimelineID).To(Equal(1))

		// the job has been deleted
		var tempJob batchv1.Job
		err = fakeClient.Get(ctx, client.ObjectKeyFromObject(job), &tempJob)
		Expect(err).To(MatchError(errors.IsNotFound, "is not found"))
	})
})

var _ = Describe("Major upgrade rollback handling", func() {
	DescribeTable("deletes the job and resets the image when the user rolls back",
		func(
			ctx SpecContext, specImage, statusImage, pgDataImage string, pgDataMajor int, expectedImage string,
		) {
			job := buildFailedUpgradeJob()
			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-example",
				},
				Spec: apiv1.ClusterSpec{
					ImageName: specImage,
				},
				Status: apiv1.ClusterStatus{
					Image: statusImage,
					PGDataImageInfo: &apiv1.ImageInfo{
						Image:        pgDataImage,
						MajorVersion: pgDataMajor,
					},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
				WithRuntimeObjects(job, cluster).
				WithStatusSubresource(cluster).
				Build()

			result, err := handleRollbackIfNeeded(ctx, fakeClient, record.NewFakeRecorder(10), cluster, job)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(ctrl.Result{RequeueAfter: 10 * time.Second}))

			// the job has been deleted
			var tempJob batchv1.Job
			err = fakeClient.Get(ctx, client.ObjectKeyFromObject(job), &tempJob)
			Expect(err).To(MatchError(errors.IsNotFound, "is not found"))

			// Status.Image has been reset to the pre-upgrade image
			var updatedCluster apiv1.Cluster
			err = fakeClient.Get(ctx, client.ObjectKeyFromObject(cluster), &updatedCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Image).To(Equal(expectedImage))
		},
		Entry("reverted to previous major version",
			"postgres:15", "postgres:16", "postgres:15", 15, "postgres:15"),
		Entry("changed to same-major image during upgrade",
			"postgres:16.1", "postgres:17", "postgres:16.0", 16, "postgres:16.0"),
	)

	It("does nothing when the requested version is still higher", func(ctx SpecContext) {
		job := buildFailedUpgradeJob()
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-example",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:16",
			},
			Status: apiv1.ClusterStatus{
				PGDataImageInfo: &apiv1.ImageInfo{
					Image:        "postgres:15",
					MajorVersion: 15,
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithRuntimeObjects(job, cluster).
			WithStatusSubresource(cluster).
			Build()

		result, err := handleRollbackIfNeeded(ctx, fakeClient, record.NewFakeRecorder(10), cluster, job)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeNil())

		// the job is still there
		var tempJob batchv1.Job
		err = fakeClient.Get(ctx, client.ObjectKeyFromObject(job), &tempJob)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("deleteAllPodsInMajorUpgradePreparation", func() {
	It("deletes pods and requeues when pods have no deletion timestamp", func(ctx SpecContext) {
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example-1",
				Namespace: "default",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithRuntimeObjects(&pod).
			Build()

		result, err := deleteAllPodsInMajorUpgradePreparation(ctx, fakeClient, []corev1.Pod{pod}, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(*result).To(Equal(ctrl.Result{RequeueAfter: 10 * time.Second}))
	})

	It("requeues when pods are still terminating", func(ctx SpecContext) {
		now := metav1.Now()
		terminatingPod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "cluster-example-1",
				Namespace:         "default",
				DeletionTimestamp: &now,
				Finalizers:        []string{"test-finalizer"},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			Build()

		result, err := deleteAllPodsInMajorUpgradePreparation(ctx, fakeClient, []corev1.Pod{terminatingPod}, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(*result).To(Equal(ctrl.Result{RequeueAfter: 10 * time.Second}))
	})

	It("returns nil when no pods or jobs exist", func(ctx SpecContext) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			Build()

		result, err := deleteAllPodsInMajorUpgradePreparation(ctx, fakeClient, nil, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeNil())
	})
})

var _ = Describe("Major upgrade job definition", func() {
	It("sets BackoffLimit to 0", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:16",
			},
			Status: apiv1.ClusterStatus{
				PGDataImageInfo: &apiv1.ImageInfo{
					Image:        "postgres:15",
					MajorVersion: 15,
				},
			},
		}
		job := createMajorUpgradeJobDefinition(cluster, 1)
		Expect(job.Spec.BackoffLimit).ToNot(BeNil())
		Expect(*job.Spec.BackoffLimit).To(Equal(int32(0)))
	})
})

var _ = Describe("Major upgrade job decoding", func() {
	It("is able to find the target image", func() {
		job := buildCompletedUpgradeJob()
		imageName, ok := getTargetImageFromMajorUpgradeJob(job)
		Expect(ok).To(BeTrue())
		Expect(imageName).To(Equal("postgres:16"))
	})
})

var _ = Describe("PVC metadata decoding", func() {
	It("is able to find the serial number of the primary server", func() {
		pvcs := []corev1.PersistentVolumeClaim{
			buildReplicaPVC(1),
			buildPrimaryPVC(2),
		}

		Expect(getPrimarySerial(pvcs)).To(Equal(2))
	})

	It("raises an error if no primary PVC is found", func() {
		pvcs := []corev1.PersistentVolumeClaim{
			buildReplicaPVC(1),
			buildReplicaPVC(2),
		}

		Expect(getPrimarySerial(pvcs)).Error().To(BeEquivalentTo(ErrNoPrimaryPVCFound))
	})

	It("raises an error if the primary PVC has an invalid serial", func() {
		pvcs := []corev1.PersistentVolumeClaim{
			buildReplicaPVC(1),
			buildInvalidPrimaryPVC(2),
		}

		Expect(getPrimarySerial(pvcs)).Error().To(HaveOccurred())
	})
})

func buildPrimaryPVC(serial int) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("cluster-example-%d", serial),
			Labels: map[string]string{
				utils.ClusterInstanceRoleLabelName: specs.ClusterRoleLabelPrimary,
			},
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: fmt.Sprintf("%v", serial),
			},
		},
	}
}

func buildInvalidPrimaryPVC(serial int) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("cluster-example-%d", serial),
			Labels: map[string]string{
				utils.ClusterInstanceRoleLabelName: specs.ClusterRoleLabelPrimary,
			},
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: fmt.Sprintf("%v - this is a test", serial),
			},
		},
	}
}

func buildReplicaPVC(serial int) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("cluster-example-%d", serial),
			Labels: map[string]string{
				utils.ClusterInstanceRoleLabelName: specs.ClusterRoleLabelReplica,
			},
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: fmt.Sprintf("%v", serial),
			},
		},
	}
}

func buildCompletedUpgradeJob() *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-example-major-upgrade",
			Labels: map[string]string{
				utils.JobRoleLabelName: jobMajorUpgrade,
			},
		},
		Spec: batchv1.JobSpec{
			Completions: ptr.To[int32](1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  jobMajorUpgrade,
							Image: "postgres:16",
						},
					},
				},
			},
		},
		Status: batchv1.JobStatus{
			Succeeded: 1,
		},
	}
}

func buildFailedUpgradeJob() *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-example-major-upgrade",
			Labels: map[string]string{
				utils.JobRoleLabelName: jobMajorUpgrade,
			},
		},
		Spec: batchv1.JobSpec{
			Completions:  ptr.To[int32](1),
			BackoffLimit: ptr.To[int32](0),
		},
		Status: batchv1.JobStatus{
			Succeeded: 0,
			Failed:    1,
		},
	}
}
