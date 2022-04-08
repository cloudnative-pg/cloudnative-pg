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

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("POD Disruption Budget specifications", func() {
	instancesNum := int32(3)
	minAvailablePrimary := int32(1)
	replicas := instancesNum - minAvailablePrimary
	minAvailableReplicas := replicas - 1
	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thistest",
			Namespace: "default",
		},
		Spec: apiv1.ClusterSpec{Instances: instancesNum},
	}

	It("have the same name as the PostgreSQL cluster", func() {
		result := BuildReplicasPodDisruptionBudget(cluster)
		Expect(result.Name).To(Equal(cluster.Name))
		Expect(result.Namespace).To(Equal(cluster.Namespace))
	})

	It("require not more than one unavailable replicas", func() {
		result := BuildReplicasPodDisruptionBudget(cluster)
		Expect(result.Spec.MinAvailable.IntVal).To(Equal(minAvailableReplicas))
	})

	It("require at least one primary instance to be available at all times", func() {
		result := BuildPrimaryPodDisruptionBudget(cluster)
		Expect(result.Spec.MinAvailable.IntVal).To(Equal(minAvailablePrimary))
	})
})
