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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cluster_cleanup", func() {
	var (
		r      ClusterReconciler
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		scheme = schemeBuilder.BuildWithAllKnownScheme()
		r = ClusterReconciler{
			Scheme: scheme,
		}
	})

	It("should delete completed jobs", func(ctx SpecContext) {
		jobList := &batchv1.JobList{Items: []batchv1.Job{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "test-1", Namespace: "test"},
				Spec: batchv1.JobSpec{
					Completions: ptr.To(int32(1)),
				},
				Status: batchv1.JobStatus{
					Succeeded: 1,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "test-2", Namespace: "test"},
				Spec: batchv1.JobSpec{
					Completions: ptr.To(int32(1)),
				},
				Status: batchv1.JobStatus{
					Succeeded: 0,
				},
			},
		}}
		cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(jobList).Build()
		r.Client = cli
		r.cleanupCompletedJobs(ctx, *jobList)

		err := cli.Get(ctx, client.ObjectKeyFromObject(&jobList.Items[0]), &batchv1.Job{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		err = cli.Get(ctx, client.ObjectKeyFromObject(&jobList.Items[1]), &batchv1.Job{})
		Expect(err).ToNot(HaveOccurred())
	})

	It("should NOT delete failed jobs (left for troubleshooting)", func(ctx SpecContext) {
		jobList := &batchv1.JobList{Items: []batchv1.Job{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "failed-job", Namespace: "test"},
				Spec: batchv1.JobSpec{
					Completions: ptr.To(int32(1)),
				},
				Status: batchv1.JobStatus{
					Succeeded: 0,
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: "True",
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "running-job", Namespace: "test"},
				Spec: batchv1.JobSpec{
					Completions: ptr.To(int32(1)),
				},
				Status: batchv1.JobStatus{
					Succeeded: 0,
				},
			},
		}}
		cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(jobList).Build()
		r.Client = cli
		r.cleanupCompletedJobs(ctx, *jobList)

		By("verifying failed job is NOT deleted (kept for troubleshooting)")
		err := cli.Get(ctx, client.ObjectKeyFromObject(&jobList.Items[0]), &batchv1.Job{})
		Expect(err).ToNot(HaveOccurred())

		By("verifying running job is not deleted")
		err = cli.Get(ctx, client.ObjectKeyFromObject(&jobList.Items[1]), &batchv1.Job{})
		Expect(err).ToNot(HaveOccurred())
	})
})
