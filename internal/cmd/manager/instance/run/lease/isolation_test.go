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

package lease

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("peersToProbe", func() {
	const ourIdentity = "cluster-example-1"

	clusterWith := func(state map[apiv1.PodName]apiv1.InstanceReportedState) *apiv1.Cluster {
		return &apiv1.Cluster{Status: apiv1.ClusterStatus{InstancesReportedState: state}}
	}

	It("returns every peer that has an address", func() {
		peers := peersToProbe(clusterWith(map[apiv1.PodName]apiv1.InstanceReportedState{
			"cluster-example-1": {IP: "10.0.0.1"},
			"cluster-example-2": {IP: "10.0.0.2"},
			"cluster-example-3": {IP: "10.0.0.3"},
		}), ourIdentity)

		Expect(peers).To(ConsistOf(
			peer{host: "cluster-example-2", ip: "10.0.0.2"},
			peer{host: "cluster-example-3", ip: "10.0.0.3"},
		))
	})

	It("skips our own entry", func() {
		peers := peersToProbe(clusterWith(map[apiv1.PodName]apiv1.InstanceReportedState{
			"cluster-example-1": {IP: "10.0.0.1"},
		}), ourIdentity)

		Expect(peers).To(BeEmpty())
	})

	It("skips a peer that has no address yet", func() {
		peers := peersToProbe(clusterWith(map[apiv1.PodName]apiv1.InstanceReportedState{
			"cluster-example-1": {IP: "10.0.0.1"},
			"cluster-example-2": {IP: "10.0.0.2"},
			"cluster-example-3": {IP: ""},
		}), ourIdentity)

		Expect(peers).To(ConsistOf(peer{host: "cluster-example-2", ip: "10.0.0.2"}))
	})

	It("returns no peer when the reported state is empty", func() {
		Expect(peersToProbe(clusterWith(nil), ourIdentity)).To(BeEmpty())
	})
})
