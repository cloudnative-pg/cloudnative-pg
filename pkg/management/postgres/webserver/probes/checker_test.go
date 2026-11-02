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
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getProbeRunnerFromCluster", func() {
	startupTolerantChecker := startupPgIsReadyChecker{inner: pgIsReadyChecker{}}

	It("uses the pg_isready strategy when no probe is configured", func() {
		cluster := apiv1.Cluster{}
		Expect(getProbeRunnerFromCluster(probeTypeStartup, cluster)).To(Equal(startupTolerantChecker))
		Expect(getProbeRunnerFromCluster(probeTypeReadiness, cluster)).To(Equal(pgIsReadyChecker{}))
	})

	It("tolerates a server rejecting connections only in the startup probe", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Startup:   &apiv1.ProbeWithStrategy{Type: apiv1.ProbeStrategyPgIsReady},
					Readiness: &apiv1.ProbeWithStrategy{Type: apiv1.ProbeStrategyPgIsReady},
				},
			},
		}
		Expect(getProbeRunnerFromCluster(probeTypeStartup, cluster)).To(Equal(startupTolerantChecker))
		Expect(getProbeRunnerFromCluster(probeTypeReadiness, cluster)).To(Equal(pgIsReadyChecker{}))
	})

	It("defaults to the pg_isready strategy when the probe is configured without a type", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Startup:   &apiv1.ProbeWithStrategy{},
					Readiness: &apiv1.ProbeWithStrategy{},
				},
			},
		}
		Expect(getProbeRunnerFromCluster(probeTypeStartup, cluster)).To(Equal(startupTolerantChecker))
		Expect(getProbeRunnerFromCluster(probeTypeReadiness, cluster)).To(Equal(pgIsReadyChecker{}))
	})

	It("uses the query strategy when configured", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Startup: &apiv1.ProbeWithStrategy{Type: apiv1.ProbeStrategyQuery},
				},
			},
		}
		Expect(getProbeRunnerFromCluster(probeTypeStartup, cluster)).To(Equal(pgQueryChecker{}))
	})

	It("uses the streaming strategy when configured, propagating the maximum lag", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Startup: &apiv1.ProbeWithStrategy{
						Type:       apiv1.ProbeStrategyStreaming,
						MaximumLag: ptr.To(resource.MustParse("100")),
					},
				},
			},
		}
		Expect(getProbeRunnerFromCluster(probeTypeStartup, cluster)).To(Equal(
			pgStreamingChecker{maximumLag: ptr.To(uint64(100))}))
	})

	It("uses the streaming strategy without a lag limit when none is configured", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Startup: &apiv1.ProbeWithStrategy{Type: apiv1.ProbeStrategyStreaming},
				},
			},
		}
		Expect(getProbeRunnerFromCluster(probeTypeStartup, cluster)).To(Equal(pgStreamingChecker{}))
	})

	It("does not leak the configuration of one probe into the other", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Startup: &apiv1.ProbeWithStrategy{Type: apiv1.ProbeStrategyQuery},
				},
			},
		}
		Expect(getProbeRunnerFromCluster(probeTypeReadiness, cluster)).To(Equal(pgIsReadyChecker{}))

		cluster = apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Readiness: &apiv1.ProbeWithStrategy{Type: apiv1.ProbeStrategyQuery},
				},
			},
		}
		Expect(getProbeRunnerFromCluster(probeTypeStartup, cluster)).To(Equal(startupTolerantChecker))
	})
})
