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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ensuring the correctness of synchronous replica data calculation", func() {
	It("should return all the non primary pods as electable", func(ctx SpecContext) {
		cluster := createFakeCluster("example")
		number, names := getSyncReplicasData(ctx, cluster)
		Expect(number).To(Equal(2))
		Expect(names).To(Equal([]string{"example-2", "example-3"}))
	})

	It("should return only the pod in the different AZ", func(ctx SpecContext) {
		const (
			primaryPod     = "exampleAntiAffinity-1"
			sameZonePod    = "exampleAntiAffinity-2"
			differentAZPod = "exampleAntiAffinity-3"
		)

		cluster := createFakeCluster("exampleAntiAffinity")
		cluster.Spec.PostgresConfiguration.SyncReplicaElectionConstraint = apiv1.SyncReplicaElectionConstraints{
			Enabled:                true,
			NodeLabelsAntiAffinity: []string{"az"},
		}
		cluster.Status.Topology = apiv1.Topology{
			SuccessfullyExtracted: true,
			Instances: map[apiv1.PodName]apiv1.PodTopologyLabels{
				primaryPod: map[string]string{
					"az": "one",
				},
				sameZonePod: map[string]string{
					"az": "one",
				},
				differentAZPod: map[string]string{
					"az": "three",
				},
			},
		}

		number, names := getSyncReplicasData(ctx, cluster)

		Expect(number).To(Equal(1))
		Expect(names).To(Equal([]string{differentAZPod}))
	})

	It("should lower the synchronous replica number to enforce self-healing", func(ctx SpecContext) {
		cluster := createFakeCluster("exampleOnePod")
		cluster.Status = apiv1.ClusterStatus{
			CurrentPrimary: "exampleOnePod-1",
			InstancesStatus: map[apiv1.PodStatus][]string{
				apiv1.PodHealthy: {"exampleOnePod-1"},
				apiv1.PodFailed:  {"exampleOnePod-2", "exampleOnePod-3"},
			},
		}
		number, names := getSyncReplicasData(ctx, cluster)

		Expect(number).To(BeZero())
		Expect(names).To(BeEmpty())
		Expect(cluster.Spec.MinSyncReplicas).To(Equal(1))
	})

	It("should behave correctly if there is no ready host", func(ctx SpecContext) {
		cluster := createFakeCluster("exampleNoPods")
		cluster.Status = apiv1.ClusterStatus{
			CurrentPrimary: "example-1",
			InstancesStatus: map[apiv1.PodStatus][]string{
				apiv1.PodFailed: {"exampleNoPods-1", "exampleNoPods-2", "exampleNoPods-3"},
			},
		}
		number, names := getSyncReplicasData(ctx, cluster)

		Expect(number).To(BeZero())
		Expect(names).To(BeEmpty())
	})
})

var _ = Describe("legacy synchronous_standby_names configuration", func() {
	It("generate the correct value for the synchronous_standby_names parameter", func(ctx SpecContext) {
		cluster := createFakeCluster("exampleNoPods")
		cluster.Spec.MinSyncReplicas = 2
		cluster.Spec.MaxSyncReplicas = 2
		cluster.Status = apiv1.ClusterStatus{
			CurrentPrimary: "example-1",
			InstancesStatus: map[apiv1.PodStatus][]string{
				apiv1.PodHealthy: {"one", "two", "three"},
			},
		}
		synchronousStandbyNames := legacySynchronousStandbyNames(ctx, cluster)

		Expect(synchronousStandbyNames).To(Equal(
			postgres.SynchronousStandbyNamesConfig{
				Method:       "ANY",
				NumSync:      2,
				StandbyNames: []string{"one", "three", "two"},
			},
		))
	})
})
