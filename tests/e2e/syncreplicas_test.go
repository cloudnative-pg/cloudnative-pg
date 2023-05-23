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
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Synchronous Replicas", Label(tests.LabelReplication), func() {
	var namespace string
	var clusterName string
	const level = tests.Medium
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	It("can manage sync replicas", func() {
		const namespacePrefix = "sync-replicas-e2e"
		clusterName = "cluster-syncreplicas"
		const sampleFile = fixturesDir + "/sync_replicas/cluster-syncreplicas.yaml.template"
		var err error
		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// First we check that the starting situation is the expected one
		By("checking that we have the correct amount of syncreplicas", func() {
			// We should have 2 candidates for quorum standbys
			timeout := time.Second * 60
			Eventually(func() (int, error, error) {
				query := "SELECT count(*) from pg_stat_replication WHERE sync_state = 'quorum'"
				out, _, err := env.ExecCommandWithPsqlClient(
					namespace,
					clusterName,
					psqlClientPod,
					apiv1.SuperUserSecretSuffix,
					utils.AppDBName,
					query,
				)
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
			Eventually(func(g Gomega) error {
				cluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				g.Expect(err).ToNot(HaveOccurred())

				cluster.Spec.MaxSyncReplicas = 1
				return env.Client.Update(env.Ctx, cluster)
			}, 60, 5).Should(BeNil())

			// Scale the cluster down to 2 pods
			_, _, err := utils.Run(fmt.Sprintf("kubectl scale --replicas=2 -n %v cluster/%v", namespace, clusterName))
			Expect(err).ToNot(HaveOccurred())
			timeout := 120
			// Wait for pod 3 to be completely terminated
			Eventually(func() (int, error) {
				podList, err := env.GetClusterPodList(namespace, clusterName)
				return len(podList.Items), err
			}, timeout).Should(BeEquivalentTo(2))

			// Construct the expected synchronous_standby_names value
			var podNames []string
			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(err).ToNot(HaveOccurred())
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				if cluster.Status.CurrentPrimary != pod.GetName() {
					podNames = append(podNames, pod.GetName())
				}
			}
			ExpectedValue := "ANY " + fmt.Sprint(cluster.Spec.MaxSyncReplicas) + " (\"" + strings.Join(podNames, "\",\"") + "\")"

			// Verify the parameter has been updated in every pod
			commandTimeout := time.Second * 10
			for _, pod := range podList.Items {
				pod := pod // pin the variable
				Eventually(func() (string, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &commandTimeout,
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

			cluster := &apiv1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(err).ToNot(HaveOccurred())
			// Expect an error. MaxSyncReplicas must be lower than the number of instances
			cluster.Spec.MaxSyncReplicas = 2
			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).To(HaveOccurred())

			cluster = &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(err).ToNot(HaveOccurred())
			// Expect an error. MinSyncReplicas must be lower than MaxSyncReplicas
			cluster.Spec.MinSyncReplicas = 2
			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).To(HaveOccurred())
		})
	})

	It("will not prevent a cluster with pg_stat_statements from being created", func() {
		const namespacePrefix = "sync-replicas-statstatements"
		clusterName = "cluster-pgstatstatements"
		const sampleFile = fixturesDir + "/sync_replicas/cluster-pgstatstatements.yaml.template"
		var err error
		// Are extensions a problem with synchronous replication? No, absolutely not,
		// but to install pg_stat_statements you need to create the relative extension
		// and that will be done just after having bootstrapped the first instance,
		// which is the primary.
		// If the number of ready replicas is not taken into consideration while
		// bootstrapping the cluster, the CREATE EXTENSION instruction will block
		// the primary since the desired number of synchronous replicas (even when 1)
		// is not met.
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})

		AssertCreateCluster(namespace, clusterName, sampleFile, env)
		AssertClusterIsReady(namespace, clusterName, 30, env)

		By("checking that synchronous_standby_names has the expected value on the primary", func() {
			Eventually(func() string {
				out, _, err := utils.Run(
					fmt.Sprintf("kubectl exec -n %v %v-1 -c postgres -- "+
						"psql -U postgres -tAc \"select setting from pg_settings where name = 'synchronous_standby_names'\"",
						namespace, clusterName))
				if err != nil {
					return ""
				}
				return strings.Trim(out, "\n")
			}, 30).Should(Equal("ANY 1 (\"cluster-pgstatstatements-2\",\"cluster-pgstatstatements-3\")"))
		})
	})
})
