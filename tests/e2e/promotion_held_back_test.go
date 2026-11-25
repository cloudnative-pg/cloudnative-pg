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
	"fmt"
	"strings"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A replica that is missing data of a primary that stopped cleanly is not
// promoted by a switchover, and it is by a failover. The replicas are kept
// behind by pausing their WAL receivers while the primary is written.
var _ = Describe("Promotion held back", Serial, Label(tests.LabelSelfHealing), func() {
	const (
		sampleFile      = fixturesDir + "/switchover/cluster-switchover.yaml.template"
		namespacePrefix = "promotion-held-back"
		level           = tests.Medium

		// heldBackReason is the reason of the event of a promotion held back
		heldBackReason = "PromotionHeldBack"

		// tableName is the table that only the primary has
		tableName = "only_on_the_primary"

		// heldBackFor is how long a promotion is observed to stay held back
		heldBackFor = 20 * time.Second
	)

	var (
		namespace, clusterName string
		oldPrimary, target     string

		// walReceivers is the PID of the WAL receiver of every replica
		walReceivers map[string]string
	)

	query := func(podName, sql string) string {
		GinkgoHelper()
		out, _, err := exec.EventuallyExecQueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{Namespace: namespace, PodName: podName},
			postgres.PostgresDBName, sql, RetryTimeout, PollingTime)
		Expect(err).ToNot(HaveOccurred())
		return strings.TrimSpace(out)
	}

	signalWalReceivers := func(signal string) {
		GinkgoHelper()
		commandTimeout := 10 * time.Second
		for replica, pid := range walReceivers {
			pod, err := podutils.Get(env.Ctx, env.Client, namespace, replica)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = env.EventuallyExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
				"sh", "-c", fmt.Sprintf("kill -%v %v", signal, pid))
			Expect(err).ToNot(HaveOccurred())
		}
	}

	getLease := func(g Gomega) *coordinationv1.Lease {
		lease := &coordinationv1.Lease{}
		g.Expect(env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: clusterName}, lease)).
			To(Succeed())
		return lease
	}

	// resumeReplicasAfterShutdown resumes the WAL receivers once the primary
	// has stopped cleanly, when there is nothing left for them to receive
	resumeReplicasAfterShutdown := func() {
		GinkgoHelper()
		Eventually(func(g Gomega) {
			position, err := utils.ParseLeasePosition(getLease(g).Annotations[utils.LeasePositionAnnotationName])
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(position.State).To(Equal(utils.PgDataStateShutDown))
		}, RetryTimeout).Should(Succeed())
		signalWalReceivers("CONT")
	}

	heldBackEvents := func(g Gomega) int {
		eventList := &corev1.EventList{}
		g.Expect(env.Client.List(env.Ctx, eventList, ctrlclient.InNamespace(namespace))).To(Succeed())
		count := 0
		for i := range eventList.Items {
			event := &eventList.Items[i]
			if event.Reason == heldBackReason && event.InvolvedObject.Name == clusterName {
				count++
			}
		}
		return count
	}

	currentPrimary := func(g Gomega) string {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		g.Expect(err).ToNot(HaveOccurred())
		return cluster.Status.CurrentPrimary
	}

	// holdSwitchover asks for a switchover to the replica that is behind, and
	// checks that it is held back
	holdSwitchover := func() {
		GinkgoHelper()
		By("asking for a switchover to a replica that is behind", func() {
			_, _, err := run.Run(fmt.Sprintf("kubectl cnpg promote %v %v -n %v", clusterName, target, namespace))
			Expect(err).ToNot(HaveOccurred())
		})

		By("reporting with an event that the promotion is held back", func() {
			resumeReplicasAfterShutdown()
			Eventually(heldBackEvents, testTimeouts[timeouts.NewPrimaryAfterSwitchover]).Should(BeNumerically(">", 0))
		})

		By("not promoting the replica", func() {
			Consistently(currentPrimary, heldBackFor, PollingTime).Should(Equal(oldPrimary))
		})
	}

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		var err error
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
		Expect(err).ToNot(HaveOccurred())
		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)

		By("pausing the WAL receiver of every replica", func() {
			oldPrimary = currentPrimary(Default)
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			target = ""
			walReceivers = make(map[string]string)
			for i := range podList.Items {
				replica := podList.Items[i].Name
				if replica == oldPrimary {
					continue
				}
				if target == "" {
					target = replica
				}
				walReceivers[replica] = query(replica,
					"SELECT pid FROM pg_catalog.pg_stat_activity WHERE backend_type = 'walreceiver'")
				Expect(walReceivers[replica]).ToNot(BeEmpty())
			}
			Expect(walReceivers).To(HaveLen(2))
			signalWalReceivers("STOP")

			// The WAL senders are terminated, not to wait for the replication timeout
			query(oldPrimary, "SELECT pg_catalog.pg_terminate_backend(pid) FROM pg_catalog.pg_stat_replication")
			primaryPod, err := podutils.Get(env.Ctx, env.Client, namespace, oldPrimary)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() (int, error) {
				return postgres.CountReplicas(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig, primaryPod, RetryTimeout)
			}, RetryTimeout).Should(BeZero())
		})

		By("writing data that only the primary has", func() {
			query(oldPrimary, fmt.Sprintf("CREATE TABLE %v AS SELECT generate_series(1, 1000) AS i", tableName))
		})
	})

	It("holds back a switchover, which is cancelled by promoting the previous primary back", func() {
		holdSwitchover()

		By("cancelling the switchover", func() {
			_, _, err := run.Run(fmt.Sprintf("kubectl cnpg promote --force %v %v -n %v",
				clusterName, oldPrimary, namespace))
			Expect(err).ToNot(HaveOccurred())
		})

		By("having the previous primary back, with its data", func() {
			clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
			Expect(currentPrimary(Default)).To(Equal(oldPrimary))
			Expect(query(oldPrimary, fmt.Sprintf("SELECT count(*) FROM %v", tableName))).To(Equal("1000"))
		})
	})

	It("holds back a switchover, until the position of the previous primary is removed from the lease", func() {
		holdSwitchover()

		By("removing the position of the previous primary from the lease", func() {
			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				lease := getLease(Default)
				delete(lease.Annotations, utils.LeasePositionAnnotationName)
				return env.Client.Update(env.Ctx, lease)
			})
			Expect(err).ToNot(HaveOccurred())
		})

		By("promoting the replica, without the data it was missing", func() {
			Eventually(currentPrimary, testTimeouts[timeouts.NewPrimaryAfterSwitchover]).Should(Equal(target))
			clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
			Expect(query(target, fmt.Sprintf("SELECT pg_catalog.to_regclass('%v') IS NULL", tableName))).
				To(Equal("t"))
		})
	})

	It("does not hold back a failover after a clean shutdown of the primary", func() {
		By("deleting the pod of the primary, which stops cleanly", func() {
			Expect(podutils.Delete(env.Ctx, env.Client, namespace, oldPrimary)).To(Succeed())
			resumeReplicasAfterShutdown()
		})

		By("promoting a replica", func() {
			Eventually(currentPrimary, testTimeouts[timeouts.NewPrimaryAfterFailover]).ShouldNot(Equal(oldPrimary))
			clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
			Expect(heldBackEvents(Default)).To(BeZero())
		})
	})
})
