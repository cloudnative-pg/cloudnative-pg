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
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/machinery/pkg/types"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fixedRunner struct {
	err error
}

func (r fixedRunner) IsHealthy(context.Context, *postgres.Instance) error {
	return r.err
}

var _ = Describe("notDivergedChecker", func() {
	It("fails as the wrapped probe does, without querying the instance", func(ctx SpecContext) {
		errNotReady := errors.New("not ready")
		Expect(notDivergedChecker{inner: fixedRunner{err: errNotReady}}.IsHealthy(ctx, postgres.NewInstance())).
			To(MatchError(errNotReady))
	})

	Context("with the replay position of the instance", func() {
		var instance *postgres.Instance

		writeWalFile := func(name, content string) {
			Expect(os.WriteFile(filepath.Join(instance.PgData, "pg_wal", name), []byte(content), 0o600)).
				To(Succeed())
		}
		checkerAt := func(replayLSN types.LSN) notDivergedChecker {
			return notDivergedChecker{
				inner: fixedRunner{},
				replayLSN: func(context.Context, *postgres.Instance) (types.LSN, error) {
					return replayLSN, nil
				},
			}
		}

		BeforeEach(func() {
			instance = postgres.NewInstance()
			instance.PgData = GinkgoT().TempDir()
			Expect(os.Mkdir(filepath.Join(instance.PgData, "pg_wal"), 0o700)).To(Succeed())
			// a standby that was on timeline 1, which the primary left at 0/4000000
			writeWalFile("000000010000000000000003", "")
			writeWalFile("000000010000000000000004", "")
			writeWalFile("000000010000000000000005", "")
			writeWalFile("00000002.history", "1\t0/4000000\tno recovery target specified\n")
		})

		It("fails when the standby replayed past the fork point", func(ctx SpecContext) {
			Expect(checkerAt("0/46210C8").IsHealthy(ctx, instance)).
				To(MatchError(ContainSubstring("replayed up to 0/46210C8 on timeline 1")))
		})

		// as a former primary rewound onto the new timeline, or a lagging standby,
		// that still holds the segments it wrote past the fork point
		It("passes when the standby did not replay past the fork point", func(ctx SpecContext) {
			Expect(checkerAt("0/3000000").IsHealthy(ctx, instance)).To(Succeed())
		})

		It("passes on a primary, which has no replay position", func(ctx SpecContext) {
			Expect(checkerAt("").IsHealthy(ctx, instance)).To(Succeed())
		})

		It("fails when the replay position cannot be read", func(ctx SpecContext) {
			errQuery := errors.New("connection refused")
			checker := notDivergedChecker{
				inner: fixedRunner{},
				replayLSN: func(context.Context, *postgres.Instance) (types.LSN, error) {
					return "", errQuery
				},
			}

			Expect(checker.IsHealthy(ctx, instance)).To(MatchError(errQuery))
		})

		It("fails when pg_wal cannot be listed", func(ctx SpecContext) {
			Expect(os.RemoveAll(filepath.Join(instance.PgData, "pg_wal"))).To(Succeed())

			Expect(checkerAt("0/46210C8").IsHealthy(ctx, instance)).
				To(MatchError(ContainSubstring("while checking for a timeline divergence")))
		})
	})
})

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
