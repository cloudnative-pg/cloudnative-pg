/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	clusterv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Synchronous Replicas", func() {
	const namespace = "sync-replicas-e2e"
	const sampleFile = fixturesDir + "/sync_replicas/cluster-syncreplicas.yaml"
	const clusterName = "cluster-syncreplicas"
	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})
	It("can manage sync replicas", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// First we check that the starting situation is the expected one
		By("checking that we have the correct amount of syncreplicas", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			cluster := &clusterv1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(err).ToNot(HaveOccurred())
			currentPrimary := cluster.Status.CurrentPrimary

			// We should have 2 candidates for quorum standbys
			timeout := time.Second * 60
			Eventually(func() (int, error, error) {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      currentPrimary,
				}
				primaryPod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, namespacedName, &primaryPod)
				Expect(err).ToNot(HaveOccurred())
				query := "SELECT count(*) from pg_stat_replication WHERE sync_state = 'quorum'"
				out, _, err := env.ExecCommand(
					env.Ctx, primaryPod, "postgres", &timeout,
					"psql", "-U", "postgres", "-tAc", query)
				value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
				return value, err, atoiErr
			}, timeout).Should(BeEquivalentTo(2))
		})
		By("checking that synchronous_standby_names reflects cluster's changes", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}

			// Set MaxSyncReplicas to 1
			err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				cluster := &clusterv1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				Expect(err).ToNot(HaveOccurred())
				cluster.Spec.MaxSyncReplicas = 1
				return env.Client.Update(env.Ctx, cluster)
			})
			Expect(err).ToNot(HaveOccurred())

			// Scale the cluster down to 2 pods
			_, _, err := tests.Run(fmt.Sprintf("kubectl scale --replicas=2 -n %v cluster/%v", namespace, clusterName))
			Expect(err).ToNot(HaveOccurred())
			timeout := 120
			// Wait for pod 3 to be completely terminated
			Eventually(func() (int, error) {
				podList, err := env.GetClusterPodList(namespace, clusterName)
				return len(podList.Items), err
			}, timeout).Should(BeEquivalentTo(2))

			// Construct the expected synchronous_standby_names value
			var podNames []string
			cluster := &clusterv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(err).ToNot(HaveOccurred())
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				podNames = append(podNames, pod.GetName())
			}
			ExpectedValue := "ANY " + fmt.Sprint(cluster.Spec.MaxSyncReplicas) + " (\"" + strings.Join(podNames, "\",\"") + "\")"

			// Verify the parameter has been updated in every pod
			commandtimeout := time.Second * 2
			for _, pod := range podList.Items {
				pod := pod // pin the variable
				Eventually(func() (string, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &commandtimeout,
						"psql", "-U", "postgres", "-tAc", "show synchronous_standby_names")
					value := strings.Trim(stdout, "\n")
					return value, err
				}, timeout).Should(BeEquivalentTo(ExpectedValue))
			}
		})

		By("erroring out when SyncReplicas fields are invalid", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}

			cluster := &clusterv1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(err).ToNot(HaveOccurred())
			// Expect an error. MaxSyncReplicas must be lower than the number of instances
			cluster.Spec.MaxSyncReplicas = 2
			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).To(HaveOccurred())

			cluster = &clusterv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(err).ToNot(HaveOccurred())
			// Expect an error. MinSyncReplicas must be lower than MaxSyncReplicas
			cluster.Spec.MinSyncReplicas = 2
			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).To(HaveOccurred())
		})
	})
})
