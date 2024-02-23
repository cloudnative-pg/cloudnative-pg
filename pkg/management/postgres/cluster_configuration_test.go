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

package postgres

import (
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func createFakeCluster(name string) *apiv1.Cluster {
	primaryPod := fmt.Sprintf("%s-1", name)
	cluster := &apiv1.Cluster{}
	cluster.Default()
	cluster.Spec.Instances = 3
	cluster.Spec.MaxSyncReplicas = 2
	cluster.Spec.MinSyncReplicas = 1
	cluster.Status = apiv1.ClusterStatus{
		CurrentPrimary: primaryPod,
		InstancesStatus: map[utils.PodStatus][]string{
			utils.PodHealthy: {primaryPod, fmt.Sprintf("%s-2", name), fmt.Sprintf("%s-3", name)},
			utils.PodFailed:  {},
		},
	}
	return cluster
}

var _ = Describe("ensuring the correctness of synchronous replica data calculation", func() {
	It("should return all the non primary pods as electable", func() {
		cluster := createFakeCluster("example")
		number, names := GetSyncReplicasData(cluster)
		Expect(number).To(Equal(2))
		Expect(names).To(Equal([]string{"example-2", "example-3"}))
	})

	It("should return only the pod in the different AZ", func() {
		const (
			primaryPod     = "example-1"
			sameZonePod    = "example-2"
			differentAZPod = "example-3"
		)

		cluster := createFakeCluster("example")
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

		number, names := GetSyncReplicasData(cluster)

		Expect(number).To(Equal(1))
		Expect(names).To(Equal([]string{differentAZPod}))
	})

	It("should lower the synchronous replica number to enforce self-healing", func() {
		cluster := createFakeCluster("example")
		cluster.Status = apiv1.ClusterStatus{
			CurrentPrimary: "example-1",
			InstancesStatus: map[utils.PodStatus][]string{
				utils.PodHealthy: {"example-1"},
				utils.PodFailed:  {"example-2", "example-3"},
			},
		}
		number, names := GetSyncReplicasData(cluster)

		Expect(number).To(BeZero())
		Expect(names).To(BeEmpty())
		Expect(cluster.Spec.MinSyncReplicas).To(Equal(1))
	})
})
