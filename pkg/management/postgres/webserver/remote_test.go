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

package webserver

import (
	"time"

	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isWALReplaySkipEnabled", func() {
	clusterWithStartupProbe := func(probe *apiv1.ProbeWithStrategy) *apiv1.Cluster {
		return &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Startup: probe,
				},
			},
		}
	}

	It("is disabled by default", func() {
		Expect(isWALReplaySkipEnabled(&apiv1.Cluster{})).To(BeFalse())
		Expect(isWALReplaySkipEnabled(clusterWithStartupProbe(nil))).To(BeFalse())
		Expect(isWALReplaySkipEnabled(clusterWithStartupProbe(&apiv1.ProbeWithStrategy{}))).To(BeFalse())
	})

	It("is disabled when explicitly turned off", func() {
		probe := &apiv1.ProbeWithStrategy{SkipOnWALReplay: ptr.To(false)}
		Expect(isWALReplaySkipEnabled(clusterWithStartupProbe(probe))).To(BeFalse())
	})

	It("is enabled with the default startup strategy", func() {
		probe := &apiv1.ProbeWithStrategy{SkipOnWALReplay: ptr.To(true)}
		Expect(isWALReplaySkipEnabled(clusterWithStartupProbe(probe))).To(BeTrue())
	})

	It("is enabled with the explicit pg_isready strategy", func() {
		probe := &apiv1.ProbeWithStrategy{
			SkipOnWALReplay: ptr.To(true),
			Type:            apiv1.ProbeStrategyPgIsReady,
		}
		Expect(isWALReplaySkipEnabled(clusterWithStartupProbe(probe))).To(BeTrue())
	})

	It("is disabled with custom startup strategies", func() {
		for _, strategy := range []apiv1.ProbeStrategyType{
			apiv1.ProbeStrategyStreaming,
			apiv1.ProbeStrategyQuery,
		} {
			probe := &apiv1.ProbeWithStrategy{
				SkipOnWALReplay: ptr.To(true),
				Type:            strategy,
			}
			Expect(isWALReplaySkipEnabled(clusterWithStartupProbe(probe))).To(BeFalse())
		}
	})
})

var _ = Describe("walReplayStallTimeout", func() {
	const startDelay = time.Hour

	It("keeps the timeout unchanged for the default segment size", func() {
		Expect(walReplayStallTimeout(startDelay, 16*1024*1024)).To(Equal(startDelay))
	})

	It("keeps the timeout unchanged when the segment size is unknown", func() {
		Expect(walReplayStallTimeout(startDelay, 0)).To(Equal(startDelay))
	})

	It("scales the timeout proportionally for larger segments", func() {
		Expect(walReplayStallTimeout(startDelay, 64*1024*1024)).To(Equal(4 * startDelay))
		Expect(walReplayStallTimeout(startDelay, 1024*1024*1024)).To(Equal(64 * startDelay))
	})

	It("does not scale for smaller segments", func() {
		Expect(walReplayStallTimeout(startDelay, 1024*1024)).To(Equal(startDelay))
	})
})
