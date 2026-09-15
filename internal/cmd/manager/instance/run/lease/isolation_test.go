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
	"errors"
	"sync/atomic"
	"time"

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

var _ = Describe("pinger.ensureInstancesAreReachable", func() {
	const (
		ourIdentity = "cluster-example-1"
		probeDelay  = 300 * time.Millisecond
	)

	fourInstances := &apiv1.Cluster{Status: apiv1.ClusterStatus{
		InstancesReportedState: map[apiv1.PodName]apiv1.InstanceReportedState{
			"cluster-example-1": {IP: "10.0.0.1"},
			"cluster-example-2": {IP: "10.0.0.2"},
			"cluster-example-3": {IP: "10.0.0.3"},
			"cluster-example-4": {IP: "10.0.0.4"},
		},
	}}

	It("probes the peers concurrently rather than one after another", func() {
		var probed atomic.Int32
		checker := pinger{probe: func(_, _ string) (string, error) {
			probed.Add(1)
			time.Sleep(probeDelay)
			return ourIdentity, nil
		}}

		start := time.Now()
		Expect(checker.ensureInstancesAreReachable(fourInstances, ourIdentity)).To(Succeed())
		elapsed := time.Since(start)

		// Three peers, one probeDelay each. In sequence this takes at least
		// 3*probeDelay; concurrently it takes just over one.
		Expect(probed.Load()).To(BeEquivalentTo(3))
		Expect(elapsed).To(BeNumerically("<", 2*probeDelay))
	})

	It("reports a peer that cannot be reached", func() {
		checker := pinger{probe: func(host, _ string) (string, error) {
			if host == "cluster-example-3" {
				return "", errors.New("unreachable")
			}
			return ourIdentity, nil
		}}

		err := checker.ensureInstancesAreReachable(fourInstances, ourIdentity)
		Expect(err).To(MatchError("unreachable"))
	})

	It("reports a peer that names another target primary", func() {
		checker := pinger{probe: func(host, _ string) (string, error) {
			if host == "cluster-example-4" {
				return "cluster-example-9", nil
			}
			return ourIdentity, nil
		}}

		err := checker.ensureInstancesAreReachable(fourInstances, ourIdentity)
		var superseded *SupersededError
		Expect(errors.As(err, &superseded)).To(BeTrue())
		Expect(superseded.TargetPrimary).To(Equal("cluster-example-9"))
	})

	It("accepts a peer that reports no target primary at all", func() {
		checker := pinger{probe: func(_, _ string) (string, error) {
			return "", nil
		}}

		Expect(checker.ensureInstancesAreReachable(fourInstances, ourIdentity)).To(Succeed())
	})
})
