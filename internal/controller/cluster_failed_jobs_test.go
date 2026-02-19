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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("handleFailedJobs", func() {
	var (
		r        ClusterReconciler
		scheme   *runtime.Scheme
		recorder *record.FakeRecorder
	)

	BeforeEach(func() {
		scheme = schemeBuilder.BuildWithAllKnownScheme()
		recorder = record.NewFakeRecorder(10)
	})

	It("should emit an event for a failed job and leave it in place", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}

		failedJob := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job-1",
				Namespace: "default",
				Labels: map[string]string{
					utils.JobRoleLabelName:      "join",
					utils.InstanceNameLabelName: "test-cluster-2",
				},
			},
			Spec: batchv1.JobSpec{
				Completions: ptr.To(int32(1)),
			},
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{
						Type:    batchv1.JobFailed,
						Status:  corev1.ConditionTrue,
						Reason:  "BackoffLimitExceeded",
						Message: "Job has reached the specified backoff limit",
					},
				},
			},
		}

		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, &failedJob).
			WithStatusSubresource(cluster).
			Build()
		r = ClusterReconciler{
			Client:   cli,
			Scheme:   scheme,
			Recorder: recorder,
		}

		resources := &managedResources{
			jobs: batchv1.JobList{Items: []batchv1.Job{failedJob}},
			pvcs: corev1.PersistentVolumeClaimList{},
		}

		err := r.handleFailedJobs(ctx, cluster, resources)
		Expect(err).ToNot(HaveOccurred())

		// The failed job should still exist (not deleted)
		err = cli.Get(ctx, client.ObjectKeyFromObject(&failedJob), &batchv1.Job{})
		Expect(err).ToNot(HaveOccurred())

		// An event should have been emitted
		Expect(recorder.Events).To(HaveLen(1))
		var event string
		Eventually(recorder.Events).Should(Receive(&event))
		Expect(event).To(ContainSubstring("FailedJob"))
		Expect(event).To(ContainSubstring("test-job-1"))

		// No snapshots should be recorded
		Expect(cluster.Status.ExcludedSnapshots).To(BeEmpty())
	})

	It("should record the snapshot name for a failed snapshot-recovery job", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}

		failedJob := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-snapshot-recovery-job",
				Namespace: "default",
				Labels: map[string]string{
					utils.JobRoleLabelName:      "snapshot-recovery",
					utils.InstanceNameLabelName: "test-cluster-1",
				},
			},
			Spec: batchv1.JobSpec{
				Completions: ptr.To(int32(1)),
			},
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{
						Type:   batchv1.JobFailed,
						Status: corev1.ConditionTrue,
						Reason: "BackoffLimitExceeded",
					},
				},
			},
		}

		pgdataPVC := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-1",
				Namespace: "default",
				Labels: map[string]string{
					utils.InstanceNameLabelName: "test-cluster-1",
					utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				DataSource: &corev1.TypedLocalObjectReference{
					Kind: "VolumeSnapshot",
					Name: "snapshot-abc",
				},
			},
		}

		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, &failedJob, &pgdataPVC).
			WithStatusSubresource(cluster).
			Build()
		r = ClusterReconciler{
			Client:   cli,
			Scheme:   scheme,
			Recorder: recorder,
		}

		resources := &managedResources{
			jobs: batchv1.JobList{Items: []batchv1.Job{failedJob}},
			pvcs: corev1.PersistentVolumeClaimList{Items: []corev1.PersistentVolumeClaim{pgdataPVC}},
		}

		err := r.handleFailedJobs(ctx, cluster, resources)
		Expect(err).ToNot(HaveOccurred())

		// The failed job should still exist (not deleted)
		err = cli.Get(ctx, client.ObjectKeyFromObject(&failedJob), &batchv1.Job{})
		Expect(err).ToNot(HaveOccurred())

		// The snapshot name should be recorded in ExcludedSnapshots
		Expect(cluster.Status.ExcludedSnapshots).To(ContainElement("snapshot-abc"))
	})

	It("should not duplicate snapshot names", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Status: apiv1.ClusterStatus{
				ExcludedSnapshots: []string{"snapshot-already-there"},
			},
		}

		failedJob := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-snapshot-recovery-job-dup",
				Namespace: "default",
				Labels: map[string]string{
					utils.JobRoleLabelName:      "snapshot-recovery",
					utils.InstanceNameLabelName: "test-cluster-1",
				},
			},
			Spec: batchv1.JobSpec{
				Completions: ptr.To(int32(1)),
			},
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{
						Type:   batchv1.JobFailed,
						Status: corev1.ConditionTrue,
						Reason: "BackoffLimitExceeded",
					},
				},
			},
		}

		pgdataPVC := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-1",
				Namespace: "default",
				Labels: map[string]string{
					utils.InstanceNameLabelName: "test-cluster-1",
					utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				DataSource: &corev1.TypedLocalObjectReference{
					Kind: "VolumeSnapshot",
					Name: "snapshot-already-there",
				},
			},
		}

		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, &failedJob, &pgdataPVC).
			WithStatusSubresource(cluster).
			Build()
		r = ClusterReconciler{
			Client:   cli,
			Scheme:   scheme,
			Recorder: recorder,
		}

		resources := &managedResources{
			jobs: batchv1.JobList{Items: []batchv1.Job{failedJob}},
			pvcs: corev1.PersistentVolumeClaimList{Items: []corev1.PersistentVolumeClaim{pgdataPVC}},
		}

		err := r.handleFailedJobs(ctx, cluster, resources)
		Expect(err).ToNot(HaveOccurred())

		// The failed job should still exist (not deleted)
		err = cli.Get(ctx, client.ObjectKeyFromObject(&failedJob), &batchv1.Job{})
		Expect(err).ToNot(HaveOccurred())

		// The snapshot name should NOT be duplicated
		Expect(cluster.Status.ExcludedSnapshots).To(Equal([]string{"snapshot-already-there"}))
	})

	It("should skip already-completed jobs", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}

		completedJob := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-completed-job",
				Namespace: "default",
				Labels: map[string]string{
					utils.JobRoleLabelName:      "join",
					utils.InstanceNameLabelName: "test-cluster-3",
				},
			},
			Spec: batchv1.JobSpec{
				Completions: ptr.To(int32(1)),
			},
			Status: batchv1.JobStatus{
				Succeeded: 1,
				Conditions: []batchv1.JobCondition{
					{
						Type:   batchv1.JobComplete,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, &completedJob).
			WithStatusSubresource(cluster).
			Build()
		r = ClusterReconciler{
			Client:   cli,
			Scheme:   scheme,
			Recorder: recorder,
		}

		resources := &managedResources{
			jobs: batchv1.JobList{Items: []batchv1.Job{completedJob}},
			pvcs: corev1.PersistentVolumeClaimList{},
		}

		err := r.handleFailedJobs(ctx, cluster, resources)
		Expect(err).ToNot(HaveOccurred())

		// The completed job should NOT be deleted
		err = cli.Get(ctx, client.ObjectKeyFromObject(&completedJob), &batchv1.Job{})
		Expect(err).ToNot(HaveOccurred())

		// No events should be emitted
		Expect(recorder.Events).To(BeEmpty())
	})
})

var _ = Describe("getJobFailureReason", func() {
	It("should return the reason from a failed condition", func() {
		job := batchv1.Job{
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{
						Type:    batchv1.JobFailed,
						Status:  corev1.ConditionTrue,
						Reason:  "BackoffLimitExceeded",
						Message: "Job has reached the specified backoff limit",
					},
				},
			},
		}
		Expect(getJobFailureReason(job)).To(Equal("BackoffLimitExceeded"))
	})

	It("should return the message when reason is empty", func() {
		job := batchv1.Job{
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{
						Type:    batchv1.JobFailed,
						Status:  corev1.ConditionTrue,
						Message: "Some failure message",
					},
				},
			},
		}
		Expect(getJobFailureReason(job)).To(Equal("Some failure message"))
	})

	It("should return unknown when no failed condition", func() {
		job := batchv1.Job{
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{
						Type:   batchv1.JobComplete,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		Expect(getJobFailureReason(job)).To(Equal("unknown"))
	})
})

var _ = Describe("getSnapshotNameFromPVCs", func() {
	It("should find the snapshot name from a matching PGDATA PVC", func() {
		pvcs := []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "instance-1",
					Labels: map[string]string{
						utils.InstanceNameLabelName: "instance-1",
						utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					DataSource: &corev1.TypedLocalObjectReference{
						Kind: "VolumeSnapshot",
						Name: "snap-xyz",
					},
				},
			},
		}
		Expect(getSnapshotNameFromPVCs(pvcs, "instance-1")).To(Equal("snap-xyz"))
	})

	It("should return empty string when no matching PVC", func() {
		pvcs := []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-instance",
					Labels: map[string]string{
						utils.InstanceNameLabelName: "other-instance",
						utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					DataSource: &corev1.TypedLocalObjectReference{
						Kind: "VolumeSnapshot",
						Name: "snap-xyz",
					},
				},
			},
		}
		Expect(getSnapshotNameFromPVCs(pvcs, "instance-1")).To(Equal(""))
	})

	It("should return empty string when PVC has no DataSource", func() {
		pvcs := []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "instance-1",
					Labels: map[string]string{
						utils.InstanceNameLabelName: "instance-1",
						utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
					},
				},
			},
		}
		Expect(getSnapshotNameFromPVCs(pvcs, "instance-1")).To(Equal(""))
	})

	It("should ignore non-PGDATA PVCs", func() {
		pvcs := []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "instance-1-wal",
					Labels: map[string]string{
						utils.InstanceNameLabelName: "instance-1",
						utils.PvcRoleLabelName:      "PG_WAL",
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					DataSource: &corev1.TypedLocalObjectReference{
						Kind: "VolumeSnapshot",
						Name: "snap-wal",
					},
				},
			},
		}
		Expect(getSnapshotNameFromPVCs(pvcs, "instance-1")).To(Equal(""))
	})
})
