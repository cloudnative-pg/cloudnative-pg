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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/fencing"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/replicationslot"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A real divergence: the operator promotes the replica that lags behind,
// while the other one, fenced during the failover so it cannot be elected,
// has already replayed WAL the new primary never received.
var _ = Describe("Diverged replica", Label(tests.LabelSelfHealing), func() {
	const (
		sampleFile      = fixturesDir + "/failover/cluster-failover.yaml.template"
		namespacePrefix = "diverged-replica-e2e"
		level           = tests.Medium
		// how long the operator may take to notice a divergence, with
		// nothing else happening in the cluster
		detectionTimeout = 120
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("detects, contains and releases a replica left on a discarded timeline", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
		Expect(err).ToNot(HaveOccurred())
		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)

		oldPrimary := clusterName + "-1"
		newPrimary := clusterName + "-2"
		diverged := clusterName + "-3"
		commandTimeout := time.Second * 10

		query := func(podName, sql string) string {
			GinkgoHelper()
			out, _, err := exec.EventuallyExecQueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{Namespace: namespace, PodName: podName},
				postgres.PostgresDBName, sql, RetryTimeout, PollingTime,
			)
			Expect(err).ToNot(HaveOccurred())
			return strings.TrimSpace(out)
		}
		getCluster := func(g Gomega) *apiv1.Cluster {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			return cluster
		}
		setReconciliationDisabled := func(disabled bool) {
			GinkgoHelper()
			Eventually(func(g Gomega) {
				cluster := getCluster(g)
				updated := cluster.DeepCopy()
				if disabled {
					if updated.Annotations == nil {
						updated.Annotations = map[string]string{}
					}
					updated.Annotations[utils.ReconciliationLoopAnnotationName] = "disabled"
				} else {
					delete(updated.Annotations, utils.ReconciliationLoopAnnotationName)
				}
				g.Expect(env.Client.Patch(env.Ctx, updated, ctrlclient.MergeFrom(cluster))).To(Succeed())
			}, RetryTimeout).Should(Succeed())
		}
		isFenced := func(g Gomega, instance string) bool {
			fenced, err := utils.GetFencedInstances(getCluster(g).Annotations)
			g.Expect(err).ToNot(HaveOccurred())
			return fenced.Has(instance)
		}
		isPodReady := func(g Gomega, podName string) bool {
			pod, err := podutils.Get(env.Ctx, env.Client, namespace, podName)
			g.Expect(err).ToNot(HaveOccurred())
			return utils.IsPodReady(*pod)
		}

		var walReceiverPID, divergedReplayLSN string
		By("stopping the WAL receiver of the replica that will be promoted", func() {
			walReceiverPID = query(newPrimary,
				"SELECT pid FROM pg_catalog.pg_stat_activity WHERE backend_type = 'walreceiver'")
			pod, err := podutils.Get(env.Ctx, env.Client, namespace, newPrimary)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = env.EventuallyExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
				"sh", "-c", fmt.Sprintf("kill -STOP %v", walReceiverPID))
			Expect(err).ToNot(HaveOccurred())
			query(oldPrimary, fmt.Sprintf("SELECT pg_catalog.pg_terminate_backend(pid) "+
				"FROM pg_catalog.pg_stat_replication WHERE application_name = '%v'", newPrimary))
		})

		By("writing data that only the other replica receives", func() {
			lsn := query(oldPrimary, "SELECT pg_catalog.pg_current_wal_lsn()")
			query(oldPrimary, "CREATE TABLE discarded AS SELECT generate_series(1, 10000) AS id")
			query(oldPrimary, "CHECKPOINT")
			Eventually(func() string {
				return query(diverged, fmt.Sprintf(
					"SELECT pg_catalog.pg_last_wal_replay_lsn() > '%v'::pg_lsn", lsn))
			}, RetryTimeout).Should(Equal("t"))
			divergedReplayLSN = query(diverged, "SELECT pg_catalog.pg_last_wal_replay_lsn()")
		})

		By("fencing that replica so the failover cannot elect it", func() {
			Expect(fencing.On(env.Ctx, env.Client, diverged, namespace, clusterName,
				fencing.UsingAnnotation)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(isPodReady(g, diverged)).To(BeFalse())
			}, RetryTimeout).Should(Succeed())
		})

		By("losing the primary, so the lagging replica is promoted", func() {
			quickDelete := &ctrlclient.DeleteOptions{GracePeriodSeconds: &quickDeletionPeriod}
			Expect(podutils.Delete(env.Ctx, env.Client, namespace, oldPrimary, quickDelete)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(getCluster(g).Status.ReadyInstances).To(Equal(1))
			}, RetryTimeout).Should(Succeed())

			pod, err := podutils.Get(env.Ctx, env.Client, namespace, newPrimary)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = env.EventuallyExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
				"sh", "-c", fmt.Sprintf("kill -CONT %v", walReceiverPID))
			Expect(err).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(getCluster(g).Status.CurrentPrimary).To(Equal(newPrimary))
			}, testTimeouts[timeouts.NewPrimaryAfterFailover]).Should(Succeed())
		})

		By("checking the fenced replica replayed past the point the new timeline forked from", func() {
			Expect(query(newPrimary, fmt.Sprintf(
				"SELECT '%v'::pg_lsn > split_part(pg_catalog.pg_read_file('pg_wal/00000002.history'), E'\\t', 2)::pg_lsn",
				divergedReplayLSN))).To(Equal("t"))
		})

		By("waiting for the former primary to be back as a replica", func() {
			Eventually(func(g Gomega) {
				g.Expect(getCluster(g).Status.ReadyInstances).To(Equal(2))
				g.Expect(isPodReady(g, oldPrimary)).To(BeTrue())
			}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())
		})

		By("lifting the fence with the operator stopped: the replica follows the new primary and diverges", func() {
			// with its reconciliation disabled, the operator cannot fence the
			// replica again, so only the readiness probe can keep it out
			setReconciliationDisabled(true)
			Expect(fencing.Off(env.Ctx, env.Client, diverged, namespace, clusterName,
				fencing.UsingAnnotation)).To(Succeed())
			// the pod never becomes ready, so the query helpers cannot be used
			Eventually(func(g Gomega) {
				pod, err := podutils.Get(env.Ctx, env.Client, namespace, diverged)
				g.Expect(err).ToNot(HaveOccurred())
				_, _, err = exec.Command(env.Ctx, env.Interface, env.RestClientConfig, *pod,
					specs.PostgresContainerName, &commandTimeout,
					"sh", "-c", "test -f $PGDATA/pg_wal/00000002.history")
				g.Expect(err).ToNot(HaveOccurred())
			}, detectionTimeout).Should(Succeed())
		})

		By("keeping the replica out of the read services with only its readiness probe", func() {
			// only a check that outlasts a few readiness probe periods tells
			// it stays out
			Consistently(func(g Gomega) {
				g.Expect(isPodReady(g, diverged)).To(BeFalse())
				for _, service := range []string{clusterName + "-ro", clusterName + "-r"} {
					slice, err := testsUtils.GetEndpointSliceByServiceName(env.Ctx, env.Client, namespace, service)
					g.Expect(err).ToNot(HaveOccurred())
					for _, endpoint := range slice.Endpoints {
						if endpoint.TargetRef != nil && endpoint.TargetRef.Name == diverged {
							g.Expect(endpoint.Conditions.Ready).To(HaveValue(BeFalse()), service)
						}
					}
				}
			}, 40, 2).Should(Succeed())
		})

		By("detecting, fencing it and dropping its slot once the operator resumes", func() {
			setReconciliationDisabled(false)
			Eventually(func(g Gomega) {
				cluster := getCluster(g)
				g.Expect(cluster.Status.DivergedInstances).To(HaveKey(apiv1.PodName(diverged)))
				entry := cluster.Status.DivergedInstances[apiv1.PodName(diverged)]
				g.Expect(entry.TimeLineID).To(Equal(1))
				g.Expect(entry.PrimaryTimeLineID).To(Equal(2))
				cond := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionReplicasHealthy))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(isFenced(g, diverged)).To(BeTrue())
				g.Expect(cluster.Status.DivergedInstances[apiv1.PodName(diverged)].Parked).To(BeTrue())
				slotName := cluster.Spec.ReplicationSlots.HighAvailability.GetSlotNameFromInstanceName(diverged)
				g.Expect(slotName).ToNot(BeEmpty())
				slots, err := replicationslot.GetReplicationSlotsOnPod(env.Ctx, env.Client, env.Interface,
					env.RestClientConfig, namespace, newPrimary, postgres.PostgresDBName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(slots).ToNot(ContainElement(slotName))
			}, RetryTimeout).Should(Succeed())
		})

		By("releasing it once rebuilt, as kubectl cnpg destroy does", func() {
			var pvcs corev1.PersistentVolumeClaimList
			Expect(env.Client.List(env.Ctx, &pvcs, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{utils.InstanceNameLabelName: diverged})).To(Succeed())
			Expect(pvcs.Items).ToNot(BeEmpty())
			for i := range pvcs.Items {
				Expect(env.Client.Delete(env.Ctx, &pvcs.Items[i])).To(Succeed())
			}
			Expect(podutils.Delete(env.Ctx, env.Client, namespace, diverged)).To(Succeed())

			Eventually(func(g Gomega) {
				cluster := getCluster(g)
				g.Expect(cluster.Status.DivergedInstances).To(BeEmpty())
				g.Expect(isFenced(g, diverged)).To(BeFalse())
				cond := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionReplicasHealthy))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())
			clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
		})
	})
})
