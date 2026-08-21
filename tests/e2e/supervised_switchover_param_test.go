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

package e2e

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/logs"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Tests for https://github.com/cloudnative-pg/cloudnative-pg/issues/10716
//
// Decreasing an enforced (hot-standby sensitive) parameter such as
// max_connections while primaryUpdateStrategy is "supervised" puts the cluster
// in "Waiting for user action": the value can only be lowered by restarting the
// primary in place (which applies it first; followers then catch up). A
// switchover does NOT converge the cluster, because the promoted replica still
// carries the higher value (verifyParametersForFollower keeps followers aligned
// upwards). If a switchover happens anyway, the demoted old primary must not
// abort recovery with "insufficient parameter settings".
var _ = Describe("Supervised decrease of an enforced parameter", Serial,
	Label(tests.LabelSelfHealing), func() {
		const (
			sampleFile = fixturesDir + "/switchover/cluster-switchover-supervised.yaml.template"
			level      = tests.Medium
		)
		var namespace string

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
		})

		// setupClusterWithPendingDecrease creates the supervised cluster running
		// with max_connections=150, lowers it to 100 and waits until the cluster
		// asks for user action. It returns the cluster name.
		setupClusterWithPendingDecrease := func(namespacePrefix string) string {
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
			Expect(err).ToNot(HaveOccurred())

			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)

			By("starting with max_connections=150 on every instance", func() {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				for i := range podList.Items {
					Eventually(pgasserts.QueryMatchExpectationPredicate(env, &podList.Items[i],
						postgres.PostgresDBName, "SHOW max_connections", "150"),
						RetryTimeout).Should(Succeed())
				}
			})

			By("decreasing max_connections to 100", func() {
				err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return err
					}
					cluster.Spec.PostgresConfiguration.Parameters["max_connections"] = "100"
					return env.Client.Update(env.Ctx, cluster)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			// With a supervised strategy the operator does not restart the primary
			// on its own: it waits for the user.
			clusterasserts.AssertClusterEventuallyReachesPhase(env, namespace, clusterName,
				[]string{apiv1.PhaseWaitingForUser}, 120)

			return clusterName
		}

		It("asks for a primary restart (not a switchover) and converges after one", func() {
			clusterName := setupClusterWithPendingDecrease("supervised-decrease-restart")

			// The status must point the user to the action that actually
			// converges the cluster: an in-place primary restart, not a switchover.
			By("reporting that the user must restart the primary", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Status.PhaseReason).To(ContainSubstring("restart of the primary instance"))
			})

			// The documented action for a decrease is an in-place restart of the
			// primary. This is what `kubectl cnpg restart <cluster> <primary>`
			// does for the primary: it sets the cluster phase to
			// PhaseInplacePrimaryRestart, which the operator then performs.
			By("requesting an in-place restart of the primary", func() {
				err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return err
					}
					cluster.Status.Phase = apiv1.PhaseInplacePrimaryRestart
					cluster.Status.PhaseReason = "Requested by the e2e test"
					return env.Client.Status().Update(env.Ctx, cluster)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			// After the restart the cluster must become ready again and every
			// instance must run with the decreased value.
			clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])

			By("every instance converged to max_connections=100", func() {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				for i := range podList.Items {
					Eventually(pgasserts.QueryMatchExpectationPredicate(env, &podList.Items[i],
						postgres.PostgresDBName, "SHOW max_connections", "100"),
						RetryTimeout).Should(Succeed())
				}
			})
		})

		It("does not abort recovery on the old primary if a switchover happens during the pending decrease",
			func() {
				clusterName := setupClusterWithPendingDecrease("supervised-decrease-switchover")

				// NOTE: we trigger the switchover by hand instead of reusing
				// clusterasserts.AssertSwitchover because that helper ends by waiting
				// for the cluster to reach PhaseHealthy. Here the decrease is still
				// pending after the switchover, so the cluster stays in
				// PhaseWaitingForUser; the regression we guard against is purely that
				// the demoted old primary rejoins as a ready standby.
				var oldPrimary, targetPrimary string
				By("triggering a supervised switchover while the decrease is pending", func() {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					oldPrimary = cluster.Status.CurrentPrimary

					podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					pods := make([]string, 0, len(podList.Items))
					for _, p := range podList.Items {
						pods = append(pods, p.Name)
					}
					sort.Strings(pods)
					Expect(pods[0]).To(BeEquivalentTo(oldPrimary))
					targetPrimary = pods[1]

					err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
						cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
						if err != nil {
							return err
						}
						cluster.Status.TargetPrimary = targetPrimary
						return env.Client.Status().Update(env.Ctx, cluster)
					})
					Expect(err).ToNot(HaveOccurred())
				})

				By("waiting for the new primary to take over", func() {
					Eventually(func(g Gomega) {
						cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(cluster.Status.CurrentPrimary).To(BeEquivalentTo(targetPrimary))
					}, testTimeouts[timeouts.NewPrimaryAfterSwitchover]).Should(Succeed())
				})

				// The regression for #10716: the demoted old primary must rejoin
				// as a standby instead of looping on
				// "recovery aborted because of insufficient parameter settings".
				By("having the old primary rejoin as a ready standby", func() {
					namespacedName := types.NamespacedName{Namespace: namespace, Name: oldPrimary}
					Eventually(func() (bool, error) {
						pod := corev1.Pod{}
						err := env.Client.Get(env.Ctx, namespacedName, &pod)
						return utils.IsPodActive(pod) && utils.IsPodReady(pod) && specs.IsPodStandby(pod), err
					}, 180).Should(BeTrue())
				})

				By("not aborting recovery with insufficient parameter settings", func() {
					// The demoted old primary must rejoin without ever logging the
					// fatal recovery abort. We inspect both the current container
					// logs and, when present, the previously terminated container:
					// if the fix regressed, the failed standby start would surface
					// "insufficient parameter settings" in one of the two.
					const fatalAbort = "insufficient parameter settings"

					assertNoAbort := func(logEntries []map[string]interface{}) {
						for _, entry := range logEntries {
							record, ok := entry["record"].(map[string]interface{})
							if !ok {
								continue
							}
							message, _ := record["message"].(string)
							Expect(message).ToNot(ContainSubstring(fatalAbort))
						}
					}

					currentLogs, err := logs.ParseJSONLogs(env.Ctx, env.Interface, namespace, oldPrimary)
					Expect(err).ToNot(HaveOccurred())
					assertNoAbort(currentLogs)

					// Previous-container logs exist only if the container restarted.
					// When the demoted primary came up clean there is nothing (or only
					// the healthy old-primary container) to inspect, so a retrieval
					// error here is not a failure; on a regression the crashed standby
					// start is captured here even after the container restarted.
					previousLogs, err := logs.ParseJSONLogsWithOptions(env.Ctx, env.Interface, namespace, oldPrimary,
						&corev1.PodLogOptions{Previous: true})
					if err == nil {
						assertNoAbort(previousLogs)
					}
				})
			})
	})
