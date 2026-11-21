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

package replication

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetSyncReplicasBounds", func() {
	It("returns zero for a cluster without synchronous replication", func() {
		cluster := &apiv1.Cluster{}
		minReplicas, maxReplicas := GetSyncReplicasBounds(cluster)
		Expect(minReplicas).To(BeZero())
		Expect(maxReplicas).To(BeZero())
	})

	It("returns the legacy fields when the legacy configuration is used", func() {
		cluster := &apiv1.Cluster{Spec: apiv1.ClusterSpec{MinSyncReplicas: 1, MaxSyncReplicas: 2}}
		minReplicas, maxReplicas := GetSyncReplicasBounds(cluster)
		Expect(minReplicas).To(Equal(1))
		Expect(maxReplicas).To(Equal(2))
	})

	It("uses the configured number as both bounds when data durability is required", func() {
		cluster := &apiv1.Cluster{}
		cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
			Method:         apiv1.SynchronousReplicaConfigurationMethodAny,
			Number:         2,
			DataDurability: apiv1.DataDurabilityLevelRequired,
		}
		minReplicas, maxReplicas := GetSyncReplicasBounds(cluster)
		Expect(minReplicas).To(Equal(2))
		Expect(maxReplicas).To(Equal(2))
	})

	It("treats an unset data durability as required", func() {
		cluster := &apiv1.Cluster{}
		cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
			Method: apiv1.SynchronousReplicaConfigurationMethodAny,
			Number: 1,
		}
		minReplicas, maxReplicas := GetSyncReplicasBounds(cluster)
		Expect(minReplicas).To(Equal(1))
		Expect(maxReplicas).To(Equal(1))
	})

	It("has no floor when data durability is preferred", func() {
		cluster := &apiv1.Cluster{}
		cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
			Method:         apiv1.SynchronousReplicaConfigurationMethodFirst,
			Number:         2,
			DataDurability: apiv1.DataDurabilityLevelPreferred,
		}
		minReplicas, maxReplicas := GetSyncReplicasBounds(cluster)
		Expect(minReplicas).To(BeZero())
		Expect(maxReplicas).To(Equal(2))
	})
})
