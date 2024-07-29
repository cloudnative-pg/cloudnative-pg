/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package replication

import (
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("synchronous replica configuration with the new API", func() {
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
			InstancesStatus: map[utils.PodStatus][]string{
				utils.PodHealthy: {"one", "two", "three"},
			},
		}

		Expect(explicitSynchronousStandbyNames(cluster)).To(Equal("ANY 2 (\"three\",\"two\")"))
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
			InstancesStatus: map[utils.PodStatus][]string{
				utils.PodHealthy: {"one", "two", "three"},
			},
		}

		Expect(explicitSynchronousStandbyNames(cluster)).To(Equal("FIRST 2 (\"three\",\"two\")"))
	})

	It("consider the maximum number of standby names", func() {
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
			InstancesStatus: map[utils.PodStatus][]string{
				utils.PodHealthy: {"one", "two", "three"},
			},
		}

		Expect(explicitSynchronousStandbyNames(cluster)).To(Equal("FIRST 2 (\"three\")"))
	})

	It("prepend the prefix and append the suffix", func() {
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
			InstancesStatus: map[utils.PodStatus][]string{
				utils.PodHealthy: {"one", "two", "three"},
			},
		}

		Expect(explicitSynchronousStandbyNames(cluster)).To(
			Equal("FIRST 2 (\"prefix\",\"here\",\"three\",\"suffix\",\"there\")"))
	})

	It("returns an empty value when no instance is available", func() {
		cluster := createFakeCluster("example")
		cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
			Method:                     apiv1.SynchronousReplicaConfigurationMethodFirst,
			Number:                     2,
			MaxStandbyNamesFromCluster: ptr.To(1),
		}
		cluster.Status = apiv1.ClusterStatus{}

		Expect(explicitSynchronousStandbyNames(cluster)).To(BeEmpty())
	})
})
