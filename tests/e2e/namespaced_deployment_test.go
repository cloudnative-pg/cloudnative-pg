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

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/nodes"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Namespaced Deployment", Label(tests.LabelNoOpenshift, tests.LabelDisruptive,
	tests.LabelNamespacedOperator), Ordered, Serial, func() {
	const (
		operatorNamespace          = "cnpg-system"
		namespacedOperatorManifest = fixturesDir + "/namespaced/manifest.yaml"
		clusterName                = "postgresql-storage-class"
		sampleFile                 = fixturesDir + "/base/cluster-storage-class.yaml.template"
		level                      = tests.Highest
	)

	BeforeAll(func() {
		if IsOpenshift() {
			Skip("This test case is not applicable on OpenShift clusters")
		}

		By("verifying operator is deployed", func() {
			var deployment appsv1.Deployment
			err := env.Client.Get(env.Ctx, types.NamespacedName{
				Namespace: operatorNamespace,
				Name:      "cnpg-controller-manager",
			}, &deployment)
			Expect(err).NotTo(HaveOccurred(), "operator must be deployed")
		})

		ConfigureNamespacedDeployment(env, operatorNamespace)
	})

	AfterAll(func() {
		RevertNamespacedDeployment(env, operatorNamespace)
	})

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			namespaces.DumpNamespaceObjects(
				env.Ctx, env.Client,
				operatorNamespace, "out/"+CurrentSpecReport().LeafNodeText+"operator.log")
		}
		err := DeleteResourcesFromFile(operatorNamespace, sampleFile)
		Expect(err).ToNot(HaveOccurred())
	})

	It("can create and reconcile clusters in namespaced mode", func() {
		By("creating a cluster in the operator namespace", func() {
			CreateResourceFromFile(operatorNamespace, sampleFile)
		})

		By("verifying cluster becomes ready", func() {
			AssertClusterIsReady(operatorNamespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
		})

		By("verifying cluster can be reconciled", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, operatorNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			oldCluster := cluster.DeepCopy()
			cluster.Spec.PostgresConfiguration.Parameters["max_connections"] = "150"
			err = env.Client.Patch(env.Ctx, cluster, ctrlclient.MergeFrom(oldCluster))
			Expect(err).ToNot(HaveOccurred())

			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, operatorNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() (string, error) {
				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: primary.Namespace,
						PodName:   primary.Name,
					},
					postgres.PostgresDBName,
					"show max_connections")
				return strings.Trim(stdout, "\n"), err
			}, 300).Should(BeEquivalentTo("150"))
		})

		By("verifying cluster can be scaled", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, operatorNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			oldCluster := cluster.DeepCopy()
			cluster.Spec.Instances = 2
			err = env.Client.Patch(env.Ctx, cluster, ctrlclient.MergeFrom(oldCluster))
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() (int, error) {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, operatorNamespace, clusterName)
				if err != nil {
					return 0, err
				}
				return len(podList.Items), nil
			}, 300).Should(BeEquivalentTo(2))

			AssertClusterIsReady(operatorNamespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
		})
	})

	Context("node drain in namespaced mode", Label(tests.LabelDisruptive), func() {
		var nodesWithLabels []string

		BeforeEach(func() {
			nodeList, _ := nodes.List(env.Ctx, env.Client)
			for _, node := range nodeList.Items {
				if (node.Spec.Unschedulable != true) && (len(node.Spec.Taints) == 0) {
					nodesWithLabels = append(nodesWithLabels, node.Name)
					cmd := fmt.Sprintf("kubectl label node %v drain=drain --overwrite", node.Name)
					_, stderr, err := run.Run(cmd)
					Expect(stderr).To(BeEmpty())
					Expect(err).ToNot(HaveOccurred())
				}
				if len(nodesWithLabels) == 3 {
					break
				}
			}
			Expect(len(nodesWithLabels)).Should(BeNumerically(">=", 2),
				"Not enough nodes are available for this test")
		})

		AfterEach(func() {
			err := nodes.UncordonAll(env.Ctx, env.Client)
			Expect(err).ToNot(HaveOccurred())
			for _, node := range nodesWithLabels {
				cmd := fmt.Sprintf("kubectl label node %v drain-", node)
				_, _, _ = run.Run(cmd)
			}
			nodesWithLabels = nil
		})

		It("can drain a node with cluster pods", func() {
			By("creating a cluster in operator namespace", func() {
				CreateResourceFromFile(operatorNamespace, sampleFile)
				AssertClusterIsReady(operatorNamespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
			})

			By("disabling PDB", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, operatorNamespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				oldCluster := cluster.DeepCopy()
				cluster.Spec.EnablePDB = ptr.To(false)
				err = env.Client.Patch(env.Ctx, cluster, ctrlclient.MergeFrom(oldCluster))
				Expect(err).ToNot(HaveOccurred())
			})

			tableLocator := TableLocator{
				Namespace:    operatorNamespace,
				ClusterName:  clusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    "test",
			}

			By("loading test data", func() {
				AssertCreateTestData(env, tableLocator)
			})

			oldPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, operatorNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By("draining the primary node", func() {
				_ = nodes.DrainPrimary(
					env.Ctx, env.Client,
					operatorNamespace, clusterName,
					testTimeouts[timeouts.DrainNode],
				)
			})

			By("verifying failover after drain", func() {
				Eventually(func() (string, error) {
					pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, operatorNamespace, clusterName)
					if err != nil {
						return "", err
					}
					return pod.Name, err
				}, 180).ShouldNot(BeEquivalentTo(oldPrimary.Name))
			})

			By("uncordoning all nodes", func() {
				err := nodes.UncordonAll(env.Ctx, env.Client)
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying data and cluster health", func() {
				AssertClusterIsReady(operatorNamespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
				AssertDataExpectedCount(env, tableLocator, 2)
				AssertClusterStandbysAreStreaming(operatorNamespace, clusterName, 140)
			})
		})
	})
})
