/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"sort"
	"strings"
	"time"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Failover", Label(tests.LabelSelfHealing), func() {
	var namespace string
	const (
		level = tests.Medium
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	failoverTest := func(namespace, clusterName string, hasDelay bool) {
		var pods []string
		var currentPrimary, targetPrimary, pausedReplica, pid string
		commandTimeout := time.Second * 10

		// We check that the currentPrimary is the -1 instance as expected,
		// and we define the targetPrimary (-3) and pausedReplica (-2).
		By("checking that CurrentPrimary and TargetPrimary are equal", func() {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(cluster.Status.CurrentPrimary, err).To(
				BeEquivalentTo(cluster.Status.TargetPrimary))
			currentPrimary = cluster.Status.CurrentPrimary

			// Gather pod names
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(len(podList.Items), err).To(BeEquivalentTo(3))
			for _, p := range podList.Items {
				pods = append(pods, p.Name)
			}
			sort.Strings(pods)
			Expect(pods[0]).To(BeEquivalentTo(currentPrimary))
			pausedReplica = pods[1]
			targetPrimary = pods[2]
		})
		// We pause the walreceiver on the pausedReplica standby with a SIGSTOP.
		// In this way we know that this standby will lag behind when
		// we do some work on the primary.
		By("pausing the walreceiver on the 2nd node of the Cluster", func() {
			primaryPod, err := env.GetPod(namespace, currentPrimary)
			Expect(err).ToNot(HaveOccurred())
			pausedPod, err := env.GetPod(namespace, pausedReplica)
			Expect(err).ToNot(HaveOccurred())

			// Get the walreceiver pid
			query := "SELECT pid FROM pg_stat_activity WHERE backend_type = 'walreceiver'"
			out, _, err := env.EventuallyExecCommand(
				env.Ctx, *pausedPod, specs.PostgresContainerName, &commandTimeout,
				"psql", "-U", "postgres", "-tAc", query)
			Expect(err).ToNot(HaveOccurred())
			pid = strings.Trim(out, "\n")

			// Send the SIGSTOP
			_, _, err = env.EventuallyExecCommand(env.Ctx, *pausedPod, specs.PostgresContainerName, &commandTimeout,
				"sh", "-c", fmt.Sprintf("kill -STOP %v", pid))
			Expect(err).ToNot(HaveOccurred())

			// Terminate the pausedReplica walsender on the primary.
			// We don't want to wait for the replication timeout.
			query = fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_replication "+
				"WHERE application_name = '%v'", pausedReplica)
			_, _, err = env.EventuallyExecCommand(
				env.Ctx, *primaryPod, specs.PostgresContainerName, &commandTimeout,
				"psql", "-U", "postgres", "-tAc", query)
			Expect(err).ToNot(HaveOccurred())

			// Expect the primary to have lost connection with the stopped standby
			Eventually(func() (int, error) {
				primaryPod, err = env.GetPod(namespace, currentPrimary)
				Expect(err).ToNot(HaveOccurred())
				return utils.CountReplicas(env, primaryPod)
			}, RetryTimeout).Should(BeEquivalentTo(1))
		})

		// Perform a CHECKPOINT on the primary and wait for the working standby
		// to replicate at it
		By("generating some WAL traffic in the Cluster", func() {
			primaryPod, err := env.GetPod(namespace, currentPrimary)
			Expect(err).ToNot(HaveOccurred())

			// Gather the current WAL LSN
			initialLSN, _, err := env.EventuallyExecCommand(
				env.Ctx, *primaryPod, specs.PostgresContainerName, &commandTimeout,
				"psql", "-U", "postgres", "-tAc", "SELECT pg_current_wal_lsn()")
			Expect(err).ToNot(HaveOccurred())

			// Execute a checkpoint
			_, _, err = env.EventuallyExecCommand(
				env.Ctx, *primaryPod, specs.PostgresContainerName, &commandTimeout,
				"psql", "-U", "postgres", "-tAc", "CHECKPOINT")
			Expect(err).ToNot(HaveOccurred())

			// The replay_lsn of the targetPrimary should be ahead
			// of the one before the checkpoint
			Eventually(func() (string, error) {
				primaryPod, err = env.GetPod(namespace, currentPrimary)
				Expect(err).ToNot(HaveOccurred())
				query := fmt.Sprintf("SELECT true FROM pg_stat_replication "+
					"WHERE application_name = '%v' AND replay_lsn > '%v'",
					targetPrimary, strings.Trim(initialLSN, "\n"))
				out, _, err := env.EventuallyExecCommand(
					env.Ctx, *primaryPod, specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-tAc", query)
				return strings.TrimSpace(out), err
			}, RetryTimeout).Should(BeEquivalentTo("t"))
		})

		// Force-delete the primary. Eventually the cluster should elect a
		// new target primary (and we check that it's the expected one)
		By("deleting the CurrentPrimary node to trigger a failover", func() {
			// Delete the target primary
			quickDelete := &ctrlclient.DeleteOptions{
				GracePeriodSeconds: &quickDeletionPeriod,
			}
			err := env.DeletePod(namespace, currentPrimary, quickDelete)
			Expect(err).ToNot(HaveOccurred())

			// We wait until the operator knows that the primary is dead.
			// At this point the promotion is waiting for all the walreceivers
			// to be disconnected. We can send the SIGCONT now.
			Eventually(func() (int, error) {
				cluster, err := env.GetCluster(namespace, clusterName)
				return cluster.Status.ReadyInstances, err
			}, RetryTimeout).Should(BeEquivalentTo(2))

			pausedPod, err := env.GetPod(namespace, pausedReplica)
			Expect(err).ToNot(HaveOccurred())

			// Send the SIGCONT to the walreceiver PID to resume execution
			_, _, err = env.EventuallyExecCommand(env.Ctx, *pausedPod, specs.PostgresContainerName,
				&commandTimeout, "sh", "-c", fmt.Sprintf("kill -CONT %v", pid))
			Expect(err).ToNot(HaveOccurred())

			if hasDelay {
				By("making sure that the operator is enforcing the switchover delay")
				timeout := 120
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.CurrentPrimaryFailingSinceTimestamp, err
				}, timeout).Should(Not(Equal("")))
			}

			By("making sure that the the targetPrimary has switched away from current primary")
			// The operator should eventually set the cluster target primary to
			// the instance we expect to take that role (-3).
			Eventually(func() (string, error) {
				cluster, err := env.GetCluster(namespace, clusterName)
				return cluster.Status.TargetPrimary, err
			}, testTimeouts[utils.NewTargetOnFailover]).
				ShouldNot(
					Or(BeEquivalentTo(currentPrimary),
						BeEquivalentTo(apiv1.PendingFailoverMarker)))
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(cluster.Status.TargetPrimary, err).To(
				BeEquivalentTo(targetPrimary))
		})

		// Finally, the cluster current primary should be changed by the
		// operator to the target primary
		By("waiting for the TargetPrimary to become CurrentPrimary", func() {
			Eventually(func() (string, error) {
				cluster, err := env.GetCluster(namespace, clusterName)
				return cluster.Status.CurrentPrimary, err
			}, testTimeouts[utils.NewPrimaryAfterFailover]).Should(BeEquivalentTo(targetPrimary))
		})
	}

	// This tests only checks that after the failure of a primary the instance
	// that has received/applied more WALs is promoted.
	// To make sure that we know which instance is promoted, we pause the
	// second instance walreceiver via a SIGSTOP signal, create WALs and then
	// delete the primary pod. We need to make sure to SIGCONT the walreceiver,
	// otherwise the operator will wait forever for the walreceiver to die
	// before deciding which instance to promote (which should be the third).
	It("reacts to primary failure", func() {
		const (
			sampleFile      = fixturesDir + "/failover/cluster-failover.yaml.template"
			namespacePrefix = "failover-e2e"
		)
		var err error
		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err := env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		failoverTest(namespace, clusterName, false)
	})

	It("reacts to primary failure while respecting the delay", func() {
		const (
			sampleFile      = fixturesDir + "/failover/cluster-failover-delay.yaml.template"
			namespacePrefix = "failover-e2e-delay"
		)
		var err error
		namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		clusterName, err := env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		failoverTest(namespace, clusterName, true)
	})
})
