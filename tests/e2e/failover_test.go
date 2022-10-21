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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Failover", func() {
	const (
		namespace   = "failover-e2e"
		sampleFile  = fixturesDir + "/base/cluster-storage-class.yaml.template"
		clusterName = "postgresql-storage-class"
		level       = tests.Medium
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	// This tests only checks that after the failure of a primary the instance
	// that has received/applied more WALs is promoted.
	// To make sure that we know which instance is promoted, we pause the
	// second instance walreceiver via a SIGSTOP signal, create WALs and then
	// delete the primary pod. We need to make sure to SIGCONT the walreceiver,
	// otherwise the operator will wait forever for the walreceiver to die
	// before deciding which instance to promote (which should be the third).
	It("reacts to primary failure", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})

		var pods []string
		var currentPrimary, targetPrimary, pausedReplica, pid string

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// We check that the currentPrimary is the -1 instance as expected,
		// and we define the targetPrimary (-3) and pausedReplica (-2).
		By("checking that CurrentPrimary and TargetPrimary are equal", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			cluster := &apiv1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
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
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      pausedReplica,
			}
			pausedPod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &pausedPod)
			Expect(err).ToNot(HaveOccurred())

			// Get the walreceiver pid
			timeout := time.Second * 2
			query := "SELECT pid FROM pg_stat_activity WHERE backend_type = 'walreceiver'"
			out, _, err := env.EventuallyExecCommand(
				env.Ctx, pausedPod, specs.PostgresContainerName, &timeout,
				"psql", "-U", "postgres", "-tAc", query)
			Expect(err).ToNot(HaveOccurred())
			pid = strings.Trim(out, "\n")

			// Send the SIGSTOP
			_, _, err = env.EventuallyExecCommand(env.Ctx, pausedPod, specs.PostgresContainerName, &timeout,
				"sh", "-c", fmt.Sprintf("kill -STOP %v", pid))
			Expect(err).ToNot(HaveOccurred())

			// Terminate the pausedReplica walsender on the primary.
			// We don't wont to wait for the replication timeout.
			namespacedName = types.NamespacedName{
				Namespace: namespace,
				Name:      currentPrimary,
			}
			primaryPod := corev1.Pod{}
			err = env.Client.Get(env.Ctx, namespacedName, &primaryPod)
			Expect(err).ToNot(HaveOccurred())
			query = fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_replication "+
				"WHERE application_name = '%v'", pausedReplica)
			_, _, err = env.EventuallyExecCommand(
				env.Ctx, primaryPod, specs.PostgresContainerName, &timeout,
				"psql", "-U", "postgres", "-tAc", query)
			Expect(err).ToNot(HaveOccurred())

			// Expect the primary to have lost connection with the stopped standby
			timeout = time.Second * 60
			Eventually(func() (int, error) {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      currentPrimary,
				}
				primaryPod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, namespacedName, &primaryPod)
				Expect(err).ToNot(HaveOccurred())
				return utils.CountReplicas(env, &primaryPod)
			}, timeout).Should(BeEquivalentTo(1))
		})

		// Perform a CHECKPOINT on the primary and wait for the working standby
		// to replicate at it
		By("generating some WAL traffic in the Cluster", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      currentPrimary,
			}
			primaryPod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &primaryPod)
			Expect(err).ToNot(HaveOccurred())

			// Get the current lsn
			timeout := time.Second * 2
			initialLSN, _, err := env.EventuallyExecCommand(
				env.Ctx, primaryPod, specs.PostgresContainerName, &timeout,
				"psql", "-U", "postgres", "-tAc", "SELECT pg_current_wal_lsn()")
			Expect(err).ToNot(HaveOccurred())

			_, _, err = env.EventuallyExecCommand(
				env.Ctx, primaryPod, specs.PostgresContainerName, &timeout,
				"psql", "-U", "postgres", "-c", "CHECKPOINT")
			Expect(err).ToNot(HaveOccurred())

			// The replay_lsn of the targetPrimary should be ahead
			// of the one before the checkpoint
			timeout = time.Second * 60
			Eventually(func() (string, error) {
				primaryPod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, namespacedName, &primaryPod)
				Expect(err).ToNot(HaveOccurred())
				query := fmt.Sprintf("SELECT true FROM pg_stat_replication "+
					"WHERE application_name = '%v' AND replay_lsn > '%v'",
					targetPrimary, strings.Trim(initialLSN, "\n"))
				out, _, err := env.EventuallyExecCommand(
					env.Ctx, primaryPod, specs.PostgresContainerName, &timeout,
					"psql", "-U", "postgres", "-tAc", query)
				return strings.TrimSpace(out), err
			}, timeout).Should(BeEquivalentTo("t"))
		})

		// Force-delete the primary. Eventually the cluster should elect a
		// new target primary (and we check that it's the expected one)
		By("deleting the CurrentPrimary node to trigger a failover", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			zero := int64(0)
			forceDelete := &ctrlclient.DeleteOptions{
				GracePeriodSeconds: &zero,
			}
			err := env.DeletePod(namespace, currentPrimary, forceDelete)
			Expect(err).ToNot(HaveOccurred())

			// We wait until the operator knows that the primary is dead.
			// At this point the promotion is waiting for all the walreceivers
			// to be disconnected. We can send the SIGCONT now.
			timeout := 60
			Eventually(func() (int, error) {
				cluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.ReadyInstances, err
			}, timeout).Should(BeEquivalentTo(2))

			namespacedPausedPodName := types.NamespacedName{
				Namespace: namespace,
				Name:      pausedReplica,
			}
			pausedPod := corev1.Pod{}
			err = env.Client.Get(env.Ctx, namespacedPausedPodName, &pausedPod)
			Expect(err).ToNot(HaveOccurred())
			commandTimeout := time.Second * 2
			_, _, err = env.EventuallyExecCommand(env.Ctx, pausedPod, specs.PostgresContainerName,
				&commandTimeout, "sh", "-c", fmt.Sprintf("kill -CONT %v", pid))
			Expect(err).ToNot(HaveOccurred())

			// The operator should eventually set the cluster target primary to
			// the instance we expect to take that role (-3).
			timeout = 120
			Eventually(func() (string, error) {
				cluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.TargetPrimary, err
			}, timeout).ShouldNot(Or(BeEquivalentTo(currentPrimary), BeEquivalentTo(apiv1.PendingFailoverMarker)))
			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(cluster.Status.TargetPrimary, err).To(
				BeEquivalentTo(targetPrimary))
		})

		// Finally, the cluster current primary should be changed by the
		// operator to the target primary
		By("waiting that the TargetPrimary become also CurrentPrimary", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			timeout := 30
			Eventually(func() (string, error) {
				cluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.CurrentPrimary, err
			}, timeout).Should(BeEquivalentTo(targetPrimary))
		})
	})
})
