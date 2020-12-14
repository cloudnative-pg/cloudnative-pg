package e2e

import (
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Failover", func() {
	It("reacts to primary failure", func() {
		const namespace = "failover-e2e"
		const sampleFile = samplesDir + "/cluster-example.yaml"
		const clusterName = "cluster-example"
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		}()

		var pods []string
		var currentPrimary, targetPrimary, pausedReplica string

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// First we check that the starting situation is the expected one
		By("checking that CurrentPrimary and TargetPrimary are the same", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			cluster := &clusterv1alpha1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
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
		// We pause the replication on a standby. In this way we know that
		// this standby will be behind the other when we do some work.
		By("pausing the replication on the 2nd node of the Cluster", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      pausedReplica,
			}
			pausedPod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &pausedPod)
			Expect(err).ToNot(HaveOccurred())
			timeout := time.Second * 2
			_, _, err = env.ExecCommand(env.Ctx, pausedPod, "postgres", &timeout,
				"psql", "-U", "postgres", "-c", "SELECT pg_wal_replay_pause()")
			Expect(err).ToNot(HaveOccurred())
		})
		// And now we do a checkpoint and a switch wal, so we're sure
		// the paused standby is behind
		By("generating some WAL traffic in the Cluster", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      currentPrimary,
			}
			primaryPod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &primaryPod)
			Expect(err).ToNot(HaveOccurred())
			timeout := time.Second * 2
			_, _, err = env.ExecCommand(env.Ctx, primaryPod, "postgres", &timeout,
				"psql", "-U", "postgres", "-c", "CHECKPOINT; SELECT pg_switch_wal()")
			Expect(err).ToNot(HaveOccurred())
			commandTimeout := 60
			// The replay_lsn of the targetPrimary should be ahead
			// compared to the pausedReplica one
			Eventually(func() (string, error) {
				primaryPod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, namespacedName, &primaryPod)
				Expect(err).ToNot(HaveOccurred())
				query := fmt.Sprintf("SELECT true FROM pg_stat_replication "+
					"WHERE application_name = '%v' "+
					"AND replay_lsn > (SELECT replay_lsn "+
					"FROM pg_stat_replication WHERE "+
					"application_name = '%v')", targetPrimary, pausedReplica)
				out, _, err := env.ExecCommand(env.Ctx, primaryPod, "postgres", &timeout,
					"psql", "-U", "postgres", "-tAc", query)
				return strings.TrimSpace(out), err
			}, commandTimeout).Should(BeEquivalentTo("t"))
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

			timeout := 120
			Eventually(func() (string, error) {
				cluster := &clusterv1alpha1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.TargetPrimary, err
			}, timeout).ShouldNot(BeEquivalentTo(currentPrimary))
			cluster := &clusterv1alpha1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(cluster.Status.TargetPrimary, err).To(BeEquivalentTo(targetPrimary))
		})
		By("waiting that the TargetPrimary become also CurrentPrimary", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			timeout := 30
			Eventually(func() (string, error) {
				cluster := &clusterv1alpha1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.CurrentPrimary, err
			}, timeout).Should(BeEquivalentTo(targetPrimary))
		})
	})
})
