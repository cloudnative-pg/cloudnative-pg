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

package postgres

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LoadEnforcedParametersFromCluster", func() {
	It("should parse enforced params from cluster", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						maxConnectionsParameter:     "200",
						"max_wal_senders":           "20",
						"max_worker_processes":      "18",
						"max_prepared_transactions": "50",
					},
				},
			},
		}
		enforcedParamsInPGData, err := LoadEnforcedParametersFromCluster(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(enforcedParamsInPGData).To(HaveLen(4))
		Expect(enforcedParamsInPGData[maxConnectionsParameter]).To(Equal(200))
		Expect(enforcedParamsInPGData["max_wal_senders"]).To(Equal(20))
		Expect(enforcedParamsInPGData["max_worker_processes"]).To(Equal(18))
		Expect(enforcedParamsInPGData["max_prepared_transactions"]).To(Equal(50))
	})

	It("report error if user given one in incorrect value in the cluster", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						maxConnectionsParameter:     "200s",
						"max_wal_senders":           "20",
						"max_worker_processes":      "18",
						"max_prepared_transactions": "50",
					},
				},
			},
		}
		_, err := LoadEnforcedParametersFromCluster(cluster)
		Expect(err).To(HaveOccurred())
	})

	It("ignore the non-enforced params user give", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						maxConnectionsParameter: "200",
						"wal_sender_timeout":    "10min",
					},
				},
			},
		}
		enforcedParamsInPGData, err := LoadEnforcedParametersFromCluster(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(enforcedParamsInPGData).To(HaveLen(1))
		Expect(enforcedParamsInPGData[maxConnectionsParameter]).To(Equal(200))
	})
})
