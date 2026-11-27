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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The primary of a cluster is killed by the OOM killer, repeatedly if needed,
// until the operator fails over. Once the disruption is over every instance
// has to be on the same timeline and attached to the new primary.
var _ = Describe("Failover after an OOM", Serial, Label(tests.LabelSelfHealing), func() {
	const (
		level = tests.Medium

		// maxOOMAttempts is how many times the primary runs out of memory,
		// at most, waiting for the operator to fail over
		maxOOMAttempts = 5

		// restartBackoffTimeout is how long to wait for an instance killed by
		// the OOM killer, again and again, to be restarted by the kubelet
		restartBackoffTimeout = 3 * time.Minute

		// failingOverReason is the reason of the event of a failover starting
		failingOverReason = "FailingOver"

		// stableFor is how long the cluster has to stay settled before the
		// disruption is considered over, to leave the operator the time to
		// start a failover that was decided before the primary was back
		stableFor = 15 * time.Second
	)

	// timelineFatal is the error of a startup process that finds a WAL file
	// from a timeline that is newer than the one of the position it needs
	timelineFatal := regexp.MustCompile(
		`belongs to timeline \d+, but previous recovered WAL file came from timeline \d+`)

	// oomScenario is a way to make the instances run out of memory
	type oomScenario struct {
		sampleFile      string
		namespacePrefix string

		// replica tells if a replica runs out of memory together with the
		// primary, the first time. It is not repeated: the failover quorum
		// check needs the replicas to be ready to promote one of them, so a
		// replica that is down again and again would prevent the failover.
		replica bool
	}

	var namespace, clusterName string

	query := func(podName, sql string) string {
		GinkgoHelper()
		out, _, err := exec.EventuallyExecQueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{Namespace: namespace, PodName: podName},
			postgres.PostgresDBName, sql, RetryTimeout, PollingTime)
		Expect(err).ToNot(HaveOccurred())
		return strings.TrimSpace(out)
	}

	getCluster := func(g Gomega) *apiv1.Cluster {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		g.Expect(err).ToNot(HaveOccurred())
		return cluster
	}

	// restartCount is the number of restarts of the postgres container
	restartCount := func(g Gomega, podName string) int32 {
		pod, err := podutils.Get(env.Ctx, env.Client, namespace, podName)
		g.Expect(err).ToNot(HaveOccurred())
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == specs.PostgresContainerName {
				return status.RestartCount
			}
		}
		g.Expect(false).To(BeTrue(), "no postgres container status in the pod %v", podName)
		return 0
	}

	// postmasterStart is when the postmaster started, which changes when
	// PostgreSQL restarts after a crash even if the container is not restarted
	postmasterStart := func(podName string) (string, error) {
		out, _, err := exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{Namespace: namespace, PodName: podName},
			postgres.PostgresDBName,
			"SELECT extract(epoch FROM pg_catalog.pg_postmaster_start_time())")
		return strings.TrimSpace(out), err
	}

	// timeline is the timeline an instance is on: the one it writes to for
	// a primary, the one it receives from for a replica
	timeline := func(podName string, isPrimary bool) int {
		GinkgoHelper()
		sql := "SELECT received_tli FROM pg_catalog.pg_stat_wal_receiver"
		if isPrimary {
			sql = "SELECT ('x' || substr(pg_catalog.pg_walfile_name(" +
				"pg_catalog.pg_current_wal_lsn()), 1, 8))::bit(32)::int"
		}
		tli, err := strconv.Atoi(query(podName, sql))
		Expect(err).ToNot(HaveOccurred())
		return tli
	}

	failingOverEvents := func(g Gomega) int {
		eventList := &corev1.EventList{}
		g.Expect(env.Client.List(env.Ctx, eventList, ctrlclient.InNamespace(namespace))).To(Succeed())
		count := 0
		for i := range eventList.Items {
			event := &eventList.Items[i]
			if event.Reason == failingOverReason && event.InvolvedObject.Name == clusterName {
				count++
			}
		}
		return count
	}

	// isSettled tells if the cluster looks healthy, with the primary that
	// is passed. It is a snapshot: only the same answer for a while is a
	// proof that the disruption is over.
	isSettled := func(g Gomega, primary string) bool {
		cluster := getCluster(g)
		if cluster.Status.Phase != apiv1.PhaseHealthy ||
			cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary ||
			cluster.Status.CurrentPrimary != primary ||
			cluster.Status.ReadyInstances != cluster.Spec.Instances {
			return false
		}
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		g.Expect(err).ToNot(HaveOccurred())
		for i := range podList.Items {
			if !utils.IsPodReady(podList.Items[i]) {
				return false
			}
		}
		return true
	}

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// outOfMemory runs a query that allocates in the backend much more than
	// the memory limit of the Pod. Nothing is replicated, so no other instance
	// is affected.
	outOfMemory := func(podName string) {
		GinkgoHelper()
		_, stderr, err := exec.QueryInInstancePodWithTimeout(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{Namespace: namespace, PodName: podName},
			postgres.PostgresDBName,
			"SELECT cardinality(array_agg(i)) FROM generate_series(1, 100000000) AS i", 2*time.Minute)
		Expect(err).To(HaveOccurred(),
			"the query was expected to be interrupted by the OOM killer on %v, stderr: %v", podName, stderr)
	}

	DescribeTable("ends up with every instance on the same timeline, attached to the primary", func(scenario oomScenario) {
		By("having a running cluster", func() {
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, scenario.namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			sampleFile := fixturesDir + "/oom_failover/" + scenario.sampleFile
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
			Expect(err).ToNot(HaveOccurred())
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)
		})

		var oldPrimary, replica string
		var oldTimeline int
		var oldRestarts int32
		var oldPostmasterStart string

		By("recording the state before the OOM", func() {
			oldPrimary = getCluster(Default).Status.CurrentPrimary
			oldTimeline = timeline(oldPrimary, true)
			oldRestarts = restartCount(Default, oldPrimary)
			var err error
			oldPostmasterStart, err = postmasterStart(oldPrimary)
			Expect(err).ToNot(HaveOccurred())

			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for i := range podList.Items {
				if name := podList.Items[i].Name; name != oldPrimary && (replica == "" || name < replica) {
					replica = name
				}
			}
		})

		By("running out of memory on the primary until the operator fails over", func() {
			// A single OOM is usually over before the operator reacts. Every
			// new one makes the kubelet wait longer before restarting the
			// container, until the primary is down for long enough.
			// The wait grows with every OOM, because the kubelet backs off
			// before restarting a container that keeps being killed.
			wait := int(restartBackoffTimeout.Seconds())
			failedOver := func(g Gomega) bool { return failingOverEvents(g) > 0 }

			restarts, start := oldRestarts, oldPostmasterStart
			for attempt := 1; attempt <= maxOOMAttempts && !failedOver(Default); attempt++ {
				By(fmt.Sprintf("waiting for PostgreSQL to accept queries (attempt %d)", attempt), func() {
					Eventually(func(g Gomega) {
						if failedOver(g) {
							return
						}
						restarts = restartCount(g, oldPrimary)
						var err error
						start, err = postmasterStart(oldPrimary)
						g.Expect(err).ToNot(HaveOccurred())
						if scenario.replica {
							_, err = postmasterStart(replica)
							g.Expect(err).ToNot(HaveOccurred())
						}
					}, wait, PollingTime).Should(Succeed(),
						"the instances do not accept queries again and the operator did not fail over")
				})
				if failedOver(Default) {
					break
				}

				By(fmt.Sprintf("running out of memory (attempt %d)", attempt), func() {
					if scenario.replica && attempt == 1 {
						outOfMemory(replica)
					}
					outOfMemory(oldPrimary)
				})

				By(fmt.Sprintf("seeing PostgreSQL restart (attempt %d)", attempt), func() {
					Eventually(func(g Gomega) {
						if failedOver(g) || restartCount(g, oldPrimary) > restarts {
							return
						}
						newStart, err := postmasterStart(oldPrimary)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(newStart).ToNot(Equal(start))
					}, wait, PollingTime).Should(Succeed())

					pod, err := podutils.Get(env.Ctx, env.Client, namespace, oldPrimary)
					Expect(err).ToNot(HaveOccurred())
					for _, status := range pod.Status.ContainerStatuses {
						if status.Name == specs.PostgresContainerName && status.LastTerminationState.Terminated != nil {
							GinkgoWriter.Printf("The container of %v was terminated: %v\n",
								oldPrimary, status.LastTerminationState.Terminated.Reason)
						}
					}
				})
			}

			Eventually(failedOver, wait, PollingTime).Should(BeTrue(),
				"the operator did not fail over after %d OOMs of the primary", maxOOMAttempts)
		})

		var primary string
		By("waiting for the cluster to settle", func() {
			// The cluster has to look settled with the same primary for
			// stableFor. Whatever breaks that, such as a failover starting,
			// restarts the window.
			var since time.Time
			var settledPrimary string
			Eventually(func(g Gomega) {
				current := getCluster(g).Status.CurrentPrimary
				if current != settledPrimary || !isSettled(g, current) {
					settledPrimary, since = current, time.Time{}
				} else if since.IsZero() {
					since = time.Now()
				}
				g.Expect(since).ToNot(BeZero(), "the cluster is not settled")
				g.Expect(time.Since(since)).To(BeNumerically(">=", stableFor))
			}, testTimeouts[timeouts.ClusterIsReady]+int(stableFor.Seconds()), PollingTime).Should(Succeed())
			primary = settledPrimary
		})

		By("having a new primary", func() {
			GinkgoWriter.Printf("Old primary: %v, new primary: %v\n", oldPrimary, primary)
			Expect(primary).ToNot(Equal(oldPrimary), "the operator failed over, the primary is expected to change")
		})

		By("having no instance failing on a timeline mismatch", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for i := range podList.Items {
				logs, err := podutils.ContainerLogs(
					env.Ctx, env.Interface, namespace, podList.Items[i].Name, specs.PostgresContainerName)
				Expect(err).ToNot(HaveOccurred())
				Expect(timelineFatal.FindString(logs)).To(BeEmpty(),
					"instance %v could not follow the new timeline", podList.Items[i].Name)
			}
		})

		By("having every instance on the same timeline", func() {
			expected := oldTimeline + 1
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for i := range podList.Items {
				name := podList.Items[i].Name
				Expect(timeline(name, name == primary)).To(Equal(expected),
					"instance %v is not on the expected timeline", name)
			}
		})

		By("having every replica streaming from the primary", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			replicas := make([]string, 0, len(podList.Items)-1)
			for i := range podList.Items {
				if podList.Items[i].Name != primary {
					replicas = append(replicas, podList.Items[i].Name)
				}
			}
			sort.Strings(replicas)

			Eventually(func() string {
				return query(primary, "SELECT coalesce(string_agg(application_name, ',' "+
					"ORDER BY application_name), '') FROM pg_catalog.pg_stat_replication "+
					"WHERE state = 'streaming'")
			}, RetryTimeout, PollingTime).Should(Equal(strings.Join(replicas, ",")),
				fmt.Sprintf("the replicas of %v are expected to be streaming from it", primary))
		})
	},
		Entry("when the primary runs out of memory", oomScenario{
			sampleFile:      "cluster-oom-primary.yaml.template",
			namespacePrefix: "oom-primary",
		}),
		Entry("when the primary and a replica run out of memory", oomScenario{
			sampleFile:      "cluster-oom-primary-replica.yaml.template",
			namespacePrefix: "oom-primary-replica",
			replica:         true,
		}),
	)
})
