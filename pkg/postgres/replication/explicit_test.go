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
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("synchronous replica configuration with the new API", func() {
	When("data durability is required", func() {
		It("creates configuration with the ANY clause", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				Method:                     apiv1.SynchronousReplicaConfigurationMethodAny,
				Number:                     2,
				MaxStandbyNamesFromCluster: nil,
				StandbyNamesPre:            []string{},
				StandbyNamesPost:           []string{},
			}
			cluster.Status = apiv1.ClusterStatus{
				CurrentPrimary: "one",
				InstancesStatus: map[apiv1.PodStatus][]string{
					apiv1.PodHealthy: {"one", "two", "three"},
				},
			}

			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "ANY",
				NumSync:      2,
				StandbyNames: []string{"three", "two", "one"},
			}))
		})

		It("creates configuration with the FIRST clause", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				Method:                     apiv1.SynchronousReplicaConfigurationMethodFirst,
				Number:                     2,
				MaxStandbyNamesFromCluster: nil,
				StandbyNamesPre:            []string{},
				StandbyNamesPost:           []string{},
			}
			cluster.Status = apiv1.ClusterStatus{
				CurrentPrimary: "one",
				InstancesStatus: map[apiv1.PodStatus][]string{
					apiv1.PodHealthy: {"one", "two", "three"},
				},
			}

			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "FIRST",
				NumSync:      2,
				StandbyNames: []string{"three", "two", "one"},
			}))
		})

		It("considers the maximum number of standby names", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				Method:                     apiv1.SynchronousReplicaConfigurationMethodFirst,
				Number:                     2,
				MaxStandbyNamesFromCluster: ptr.To(1),
				StandbyNamesPre:            []string{},
				StandbyNamesPost:           []string{},
			}
			cluster.Status = apiv1.ClusterStatus{
				CurrentPrimary: "one",
				InstancesStatus: map[apiv1.PodStatus][]string{
					apiv1.PodHealthy: {"one", "two", "three"},
				},
			}

			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "FIRST",
				NumSync:      2,
				StandbyNames: []string{"three"},
			}))
		})

		It("prepends the prefix and append the suffix", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				Method:                     apiv1.SynchronousReplicaConfigurationMethodFirst,
				Number:                     2,
				MaxStandbyNamesFromCluster: ptr.To(1),
				StandbyNamesPre:            []string{"prefix", "here"},
				StandbyNamesPost:           []string{"suffix", "there"},
			}
			cluster.Status = apiv1.ClusterStatus{
				CurrentPrimary: "one",
				InstancesStatus: map[apiv1.PodStatus][]string{
					apiv1.PodHealthy: {"one", "two", "three"},
				},
			}

			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "FIRST",
				NumSync:      2,
				StandbyNames: []string{"prefix", "here", "three", "suffix", "there"},
			}))
		})

		It("enforce synchronous replication even if there are no healthy replicas", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				Method:                     apiv1.SynchronousReplicaConfigurationMethodFirst,
				Number:                     2,
				MaxStandbyNamesFromCluster: ptr.To(1),
			}
			cluster.Status = apiv1.ClusterStatus{}

			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "FIRST",
				NumSync:      2,
				StandbyNames: []string{"example-placeholder"},
			}))
		})

		It("includes pods that do not report the status", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				Method:                     apiv1.SynchronousReplicaConfigurationMethodFirst,
				Number:                     2,
				MaxStandbyNamesFromCluster: nil,
				StandbyNamesPre:            []string{},
				StandbyNamesPost:           []string{},
			}
			cluster.Status = apiv1.ClusterStatus{
				CurrentPrimary: "one",
				InstancesStatus: map[apiv1.PodStatus][]string{
					apiv1.PodHealthy: {"one", "three"},
				},
				InstanceNames: []string{"one", "two", "three"},
			}

			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "FIRST",
				NumSync:      2,
				StandbyNames: []string{"three", "two", "one"},
			}))
		})
	})

	When("Data durability is preferred", func() {
		It("creates configuration with the ANY clause", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				DataDurability:             apiv1.DataDurabilityLevelPreferred,
				Method:                     apiv1.SynchronousReplicaConfigurationMethodAny,
				Number:                     2,
				MaxStandbyNamesFromCluster: nil,
				StandbyNamesPre:            []string{},
				StandbyNamesPost:           []string{},
			}
			cluster.Status = apiv1.ClusterStatus{
				CurrentPrimary: "one",
				InstancesStatus: map[apiv1.PodStatus][]string{
					apiv1.PodHealthy: {"one", "two", "three"},
				},
			}

			// Important: the name of the primary is not included in the list
			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "ANY",
				NumSync:      2,
				StandbyNames: []string{"three", "two"},
			}))
		})

		It("creates configuration with the FIRST clause", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				DataDurability:             apiv1.DataDurabilityLevelPreferred,
				Method:                     apiv1.SynchronousReplicaConfigurationMethodFirst,
				Number:                     2,
				MaxStandbyNamesFromCluster: nil,
				StandbyNamesPre:            []string{},
				StandbyNamesPost:           []string{},
			}
			cluster.Status = apiv1.ClusterStatus{
				CurrentPrimary: "one",
				InstancesStatus: map[apiv1.PodStatus][]string{
					apiv1.PodHealthy: {"one", "two", "three"},
				},
			}

			// Important: the name of the primary is not included in the list
			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "FIRST",
				NumSync:      2,
				StandbyNames: []string{"three", "two"},
			}))
		})

		It("considers the maximum number of standby names", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				DataDurability:             apiv1.DataDurabilityLevelPreferred,
				Method:                     apiv1.SynchronousReplicaConfigurationMethodFirst,
				Number:                     2,
				MaxStandbyNamesFromCluster: ptr.To(1),
				StandbyNamesPre:            []string{},
				StandbyNamesPost:           []string{},
			}
			cluster.Status = apiv1.ClusterStatus{
				CurrentPrimary: "a-primary",
				InstancesStatus: map[apiv1.PodStatus][]string{
					apiv1.PodHealthy: {"a-primary", "two", "three"},
				},
			}

			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "FIRST",
				NumSync:      1,
				StandbyNames: []string{"three"},
			}))
		})

		It("ignores the prefix and the suffix", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				DataDurability:   apiv1.DataDurabilityLevelPreferred,
				Method:           apiv1.SynchronousReplicaConfigurationMethodFirst,
				Number:           2,
				StandbyNamesPre:  []string{"prefix", "here"},
				StandbyNamesPost: []string{"suffix", "there"},
			}
			cluster.Status = apiv1.ClusterStatus{
				CurrentPrimary: "one",
				InstancesStatus: map[apiv1.PodStatus][]string{
					apiv1.PodHealthy: {"one", "two", "three"},
				},
			}

			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "FIRST",
				NumSync:      2,
				StandbyNames: []string{"three", "two"},
			}))
		})

		It("disables synchronous replication when no instance is available", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				DataDurability:             apiv1.DataDurabilityLevelPreferred,
				Method:                     apiv1.SynchronousReplicaConfigurationMethodFirst,
				Number:                     2,
				MaxStandbyNamesFromCluster: ptr.To(1),
			}
			cluster.Status = apiv1.ClusterStatus{}

			Expect(explicitSynchronousStandbyNames(cluster).IsZero()).To(BeTrue())
		})

		It("does not include pods that do not report the status", func() {
			cluster := createFakeCluster("example")
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				DataDurability:             apiv1.DataDurabilityLevelPreferred,
				Method:                     apiv1.SynchronousReplicaConfigurationMethodFirst,
				Number:                     2,
				MaxStandbyNamesFromCluster: nil,
				StandbyNamesPre:            []string{},
				StandbyNamesPost:           []string{},
			}
			cluster.Status = apiv1.ClusterStatus{
				CurrentPrimary: "one",
				InstancesStatus: map[apiv1.PodStatus][]string{
					apiv1.PodHealthy: {"one", "three"},
				},
				InstanceNames: []string{"one", "two", "three"},
			}

			Expect(explicitSynchronousStandbyNames(cluster)).To(Equal(postgres.SynchronousStandbyNamesConfig{
				Method:       "FIRST",
				NumSync:      1,
				StandbyNames: []string{"three"},
			}))
		})
	})
})
