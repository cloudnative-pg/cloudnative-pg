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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("reading quorum metadata",
	func(ctx SpecContext, currentStatus *apiv1.FailoverQuorumStatus, readError error, expected bool) {
		scheme := runtime.NewScheme()
		Expect(apiv1.AddToScheme(scheme)).To(Succeed())
		cachedQuorum := &apiv1.FailoverQuorum{
			ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "default"},
			Status: apiv1.FailoverQuorumStatus{
				StandbyNumber: 2,
				StandbyNames:  []string{"postgres-2", "postgres-3"},
			},
		}
		apiClient := fake.NewClientBuilder().WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption,
				) error {
					if readError != nil {
						return readError
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).Build()
		if currentStatus != nil {
			currentQuorum := cachedQuorum.DeepCopy()
			currentQuorum.Status = *currentStatus
			Expect(apiClient.Create(ctx, currentQuorum)).To(Succeed())
		}
		r := &ClusterReconciler{
			Client:    fake.NewClientBuilder().WithScheme(scheme).WithObjects(cachedQuorum).Build(),
			apiReader: apiClient,
		}
		cluster := &apiv1.Cluster{ObjectMeta: cachedQuorum.ObjectMeta}
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{{
				Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "postgres-3"}},
				IsPodReady: true,
			}},
		}

		allowed, err := r.evaluateQuorumCheck(ctx, cluster, statusList)
		if readError != nil {
			Expect(err).To(MatchError(readError))
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(allowed).To(Equal(expected))
	},
	Entry("uses the current synchronous replica count", &apiv1.FailoverQuorumStatus{
		StandbyNumber: 1,
		StandbyNames:  []string{"postgres-2", "postgres-3"},
	}, nil, false),
	Entry("denies failover while quorum metadata is reset", &apiv1.FailoverQuorumStatus{}, nil, false),
	Entry("denies failover when quorum metadata is missing", nil, nil, false),
	Entry("allows failover when the current metadata establishes quorum", &apiv1.FailoverQuorumStatus{
		StandbyNumber: 2,
		StandbyNames:  []string{"postgres-2", "postgres-3"},
	}, nil, true),
	Entry("returns API read errors", nil, context.DeadlineExceeded, false),
)

var _ = Describe("quorum promotion control", func() {
	r := &ClusterReconciler{}

	When("the information is not consistent because the number of synchronous standbies is zero", func() {
		sync := &apiv1.FailoverQuorum{
			Status: apiv1.FailoverQuorumStatus{
				StandbyNumber: 0,
			},
		}

		statusList := postgres.PostgresqlStatusList{}

		It("denies a failover", func(ctx SpecContext) {
			status, err := r.evaluateQuorumCheckWithStatus(ctx, sync, statusList)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeFalse())
		})
	})

	When("the information is not consistent because the standby list is empty", func() {
		sync := &apiv1.FailoverQuorum{
			Status: apiv1.FailoverQuorumStatus{
				StandbyNumber: 3,
				StandbyNames:  nil,
			},
		}

		statusList := postgres.PostgresqlStatusList{}

		It("denies a failover", func(ctx SpecContext) {
			status, err := r.evaluateQuorumCheckWithStatus(ctx, sync, statusList)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeFalse())
		})
	})

	When("there is no quorum", func() {
		sync := &apiv1.FailoverQuorum{
			Status: apiv1.FailoverQuorumStatus{
				StandbyNumber: 1,
				StandbyNames: []string{
					"postgres-2",
					"postgres-3",
				},
			},
		}

		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "postgres-3",
						},
					},
					Error:      nil,
					IsPodReady: true,
				},
			},
		}

		It("denies a failover", func(ctx SpecContext) {
			status, err := r.evaluateQuorumCheckWithStatus(ctx, sync, statusList)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeFalse())
		})
	})

	When("there is quorum", func() {
		sync := &apiv1.FailoverQuorum{
			Status: apiv1.FailoverQuorumStatus{
				StandbyNumber: 1,
				StandbyNames: []string{
					"postgres-2",
					"postgres-3",
				},
			},
		}

		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "postgres-2",
						},
					},
					Error:      nil,
					IsPodReady: true,
				},
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "postgres-3",
						},
					},
					Error:      nil,
					IsPodReady: true,
				},
			},
		}

		It("denies a failover", func(ctx SpecContext) {
			status, err := r.evaluateQuorumCheckWithStatus(ctx, sync, statusList)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeTrue())
		})
	})
})
