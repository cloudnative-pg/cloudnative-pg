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

package probes

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isolation check bookkeeping", func() {
	var executor *livenessExecutor

	// clusterWithThreshold builds a Cluster asking for the given failure threshold.
	clusterWithThreshold := func(threshold int32) *apiv1.Cluster {
		return &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Liveness: &apiv1.LivenessProbe{
						Probe: apiv1.Probe{FailureThreshold: threshold},
					},
				},
			},
		}
	}

	BeforeEach(func() {
		executor = &livenessExecutor{}
	})

	It("does not ask for a shutdown before the threshold is reached", func() {
		cluster := clusterWithThreshold(3)
		Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())
		Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())
	})

	It("asks for a shutdown once the threshold is reached", func() {
		cluster := clusterWithThreshold(3)
		Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())
		Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())
		Expect(executor.claimIsolationShutdown(cluster)).To(BeTrue())
	})

	It("asks again after another threshold of failures, so a request that did not stop "+
		"the instance is retried", func() {
		cluster := clusterWithThreshold(3)
		for range 2 {
			Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())
		}
		Expect(executor.claimIsolationShutdown(cluster)).To(BeTrue())

		// A claim that is not released blocks any further one, so release it here to
		// exercise the counter reset on its own, the way a failed handover would.
		executor.releaseIsolationShutdownClaim()

		// The count restarted rather than latching, so a persisting isolation asks again.
		for range 2 {
			Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())
		}
		Expect(executor.claimIsolationShutdown(cluster)).To(BeTrue())
	})

	It("forgets the failures accumulated so far once the check succeeds again", func() {
		cluster := clusterWithThreshold(3)
		Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())
		Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())

		executor.isolationCheckSucceeded()

		Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())
		Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())
		Expect(executor.claimIsolationShutdown(cluster)).To(BeTrue())
	})

	It("asks for a shutdown on the first failure when the threshold is one", func() {
		Expect(executor.claimIsolationShutdown(clusterWithThreshold(1))).To(BeTrue())
	})

	It("honours a threshold derived from the legacy livenessProbeTimeout", func() {
		// 90s over the default 10s period is 9 attempts, not the bare default of 3.
		timeout := int32(90)
		cluster := &apiv1.Cluster{Spec: apiv1.ClusterSpec{LivenessProbeTimeout: &timeout}}

		for range 8 {
			Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())
		}
		Expect(executor.claimIsolationShutdown(cluster)).To(BeTrue())
	})

	It("blocks a further claim while one is outstanding, whatever the count", func() {
		cluster := clusterWithThreshold(1)
		Expect(executor.claimIsolationShutdown(cluster)).To(BeTrue())
		Expect(executor.claimIsolationShutdown(cluster)).To(BeFalse())

		executor.releaseIsolationShutdownClaim()
		Expect(executor.claimIsolationShutdown(cluster)).To(BeTrue())
	})
})
