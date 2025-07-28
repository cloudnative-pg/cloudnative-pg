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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
