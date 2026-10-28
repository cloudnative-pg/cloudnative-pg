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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Enabling the cross-namespace resources support on a running cluster makes
// every instance manager restart itself to watch the managed resources in
// every namespace. PostgreSQL must keep running across that restart.
var _ = Describe("Cross-namespace declarative databases", Label(tests.LabelDeclarativeDatabases), func() {
	const (
		clusterManifest = fixturesDir + "/declarative_databases_cross_namespace/cluster.yaml.template"
		level           = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// instanceState is what tells whether PostgreSQL was restarted in a Pod
	type instanceState struct {
		postmasterStartTime string
		restartCount        int32
	}

	getInstanceStates := func(namespace, clusterName string) map[string]instanceState {
		GinkgoHelper()

		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		states := make(map[string]instanceState, len(podList.Items))
		for _, pod := range podList.Items {
			stdout, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{Namespace: namespace, PodName: pod.Name},
				"postgres",
				"SELECT pg_catalog.pg_postmaster_start_time()")
			Expect(err).ToNot(HaveOccurred())

			states[pod.Name] = instanceState{
				postmasterStartTime: strings.TrimSpace(stdout),
				restartCount:        postgresContainerRestartCount(pod),
			}
		}
		return states
	}

	It("enables the support on a running cluster without restarting PostgreSQL", func() {
		var (
			clusterNamespace, databaseNamespace, clusterName string
			err                                              error
			statesBefore                                     map[string]instanceState
		)
		const databaseObjectName = "cross-namespace-db"

		By("creating the cluster without the cross-namespace resources support", func() {
			clusterNamespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, "xns-db-cluster")
			Expect(err).ToNot(HaveOccurred())
			databaseNamespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, "xns-db-app")
			Expect(err).ToNot(HaveOccurred())

			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())

			clusterasserts.AssertCreateCluster(env, testTimeouts, clusterNamespace, clusterName, clusterManifest)
		})

		By("recording when PostgreSQL started in every instance", func() {
			statesBefore = getInstanceStates(clusterNamespace, clusterName)
			Expect(statesBefore).ToNot(BeEmpty())
		})

		By("enabling the cross-namespace resources support", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, clusterNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			updated := cluster.DeepCopy()
			updated.Spec.EnableCrossNamespaceResources = true
			Expect(objects.Patch(env.Ctx, env.Client, updated, client.MergeFrom(cluster))).To(Succeed())
		})

		By("creating a Database in another namespace targeting the cluster", func() {
			database := &apiv1.Database{
				ObjectMeta: metav1.ObjectMeta{
					Name:      databaseObjectName,
					Namespace: databaseNamespace,
				},
				Spec: apiv1.DatabaseSpec{
					ClusterRef: apiv1.ClusterObjectReference{
						Name:      clusterName,
						Namespace: clusterNamespace,
					},
					Name:  "cross_namespace",
					Owner: "app",
				},
			}
			_, err := objects.Create(env.Ctx, env.Client, database)
			Expect(err).ToNot(HaveOccurred())
		})

		By("ensuring the Database is reconciled from the other namespace", func() {
			// This only happens once the instance manager of the primary
			// restarted with a cache watching every namespace
			Eventually(func(g Gomega) {
				var database apiv1.Database
				g.Expect(env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: databaseNamespace,
					Name:      databaseObjectName,
				}, &database)).To(Succeed())
				g.Expect(database.Status.Applied).To(Equal(ptr.To(true)))
				g.Expect(database.Status.Message).To(BeEmpty())
			}, 300).WithPolling(10 * time.Second).Should(Succeed())

			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, clusterNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{Namespace: clusterNamespace, PodName: primary.Name},
					"postgres",
					"SELECT count(*) FROM pg_catalog.pg_database WHERE datname = 'cross_namespace'")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(strings.TrimSpace(stdout)).To(Equal("1"))
			}, 30).Should(Succeed())
		})

		By("verifying PostgreSQL was not restarted in any instance", func() {
			clusterasserts.AssertClusterIsReady(env, clusterNamespace, clusterName,
				testTimeouts[timeouts.ClusterIsReady])
			Expect(getInstanceStates(clusterNamespace, clusterName)).To(Equal(statesBefore))
		})
	})
})

// postgresContainerRestartCount returns how many times the kubelet restarted
// the PostgreSQL container of a Pod
func postgresContainerRestartCount(pod corev1.Pod) int32 {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == specs.PostgresContainerName {
			return status.RestartCount
		}
	}
	return -1
}
