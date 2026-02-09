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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/nodes"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests in which we check that operator is able to fail over a new
// primary and bring back the replicas when we drain nodes
var _ = Describe("E2E Drain Node", Serial, Label(tests.LabelDisruptive, tests.LabelMaintenance), func() {
	var nodesWithLabels []string
	const level = tests.Lowest

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		nodes, _ := nodes.List(env.Ctx, env.Client)
		// We label three nodes where we could run the workloads, and ignore
		// the others. The pods of the clusters created in this test run only
		// where the drain label exists.
		for _, node := range nodes.Items {
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
		Expect(len(nodesWithLabels)).Should(BeEquivalentTo(3),
			"Not enough nodes are available for this test")
	})

	AfterEach(func() {
		// Uncordon the cordoned nodes and remove the labels we added in the
		// BeforeEach section
		err := nodes.UncordonAll(env.Ctx, env.Client)
		Expect(err).ToNot(HaveOccurred())
		for _, node := range nodesWithLabels {
			cmd := fmt.Sprintf("kubectl label node %v drain- ", node)
			_, _, err := run.Run(cmd)
			Expect(err).ToNot(HaveOccurred())
		}
		nodesWithLabels = nil
	})

	Context("Default maintenance and pvc", func() {
		const sampleFile = fixturesDir + "/drain-node/cluster-drain-node-karpenter.yaml.template"
		const clusterName = "cluster-drain-node-karpenter"

		It("will remove the pod from a node tainted by karpenter", func() {
			const namespacePrefix = "drain-node-e2e-karpeter-initiated"

			var namespace string

			By("creating the namespace and the cluster", func() {
				var err error
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterName, sampleFile, env)
			})

			By("waiting for the jobs to be removed", func() {
				timeout := testTimeouts[testsUtils.ClusterIsReady]
				Eventually(func() (int, error) {
					podList, err := pods.List(env.Ctx, env.Client, namespace)
					if err != nil {
						return 0, err
					}
					return len(podList.Items), err
				}, timeout).Should(BeEquivalentTo(3))
			})

			tableLocator := TableLocator{
				Namespace:    namespace,
				ClusterName:  clusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    "test",
			}

			By("loading test data", func() {
				AssertCreateTestData(env, tableLocator)
			})

			oldPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By("adding a taint from karpenter to the node containing the primary", func() {
				cmd := fmt.Sprintf("kubectl taint nodes %v karpenter.sh/disruption:NoSchedule", oldPrimary.Spec.NodeName)
				_, _, err := run.Run(cmd)
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying failover after drain", func() {
				timeout := testTimeouts[testsUtils.ClusterIsReady]
				Eventually(func() (string, error) {
					pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return "", err
					}
					return pod.Name, err
				}, timeout).ShouldNot(BeEquivalentTo(oldPrimary.Name))
			})

			By("removing karpenter taint from node", func() {
				cmd := fmt.Sprintf(
					"kubectl taint nodes %v karpenter.sh/disruption=NoSchedule:NoSchedule-",
					oldPrimary.Spec.NodeName,
				)
				_, _, err := run.Run(cmd)
				Expect(err).ToNot(HaveOccurred())
			})

			By("data is present and standbys are streaming", func() {
				AssertDataExpectedCount(env, tableLocator, 2)
				AssertClusterStandbysAreStreaming(namespace, clusterName, 140)
			})
		})
	})

	Context("Maintenance on, reuse pvc on", func() {
		// Initialize empty global namespace variable
		var namespace string
		const sampleFile = fixturesDir + "/drain-node/cluster-drain-node.yaml.template"
		const clusterName = "cluster-drain-node"

		// We cordon one node, so pods will run on one or two nodes. This
		// is only to create a harder situation for the operator.
		// We then drain the node containing the primary and expect the pod(s)
		// to be back only when its PVC is available. On GKE with the default
		// storage class and on AKS with Rook this happens immediately. When
		// the storage is bound to the node, we have to uncordon the node
		// first. We uncordon it in all cases and check for the UIDs of the
		// PVC(s).

		It("can drain the primary pod's node with 3 pods on 2 nodes", func() {
			const namespacePrefix = "drain-node-e2e-pvc-on-two-nodes"
			By("leaving only two nodes uncordoned", func() {
				// mark a node unschedulable so the pods will be distributed only on two nodes
				for _, cordonNode := range nodesWithLabels[:len(nodesWithLabels)-2] {
					cmd := fmt.Sprintf("kubectl cordon %v", cordonNode)
					_, _, err := run.Run(cmd)
					Expect(err).ToNot(HaveOccurred())
				}
			})
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("waiting for the jobs to be removed", func() {
				// Wait for jobs to be removed
				timeout := testTimeouts[testsUtils.ClusterIsReady]
				Eventually(func() (int, error) {
					podList, err := pods.List(env.Ctx, env.Client, namespace)
					if err != nil {
						return 0, err
					}
					return len(podList.Items), err
				}, timeout).Should(BeEquivalentTo(3))
			})

			// Load test data
			oldPrimary := clusterName + "-1"
			tableLocator := TableLocator{
				Namespace:    namespace,
				ClusterName:  clusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    "test",
			}
			AssertCreateTestData(env, tableLocator)

			// We create a mapping between the pod names and the UIDs of
			// their volumes. We do not expect the UIDs to change.
			// We take advantage of the fact that related PVCs and Pods have
			// the same name.
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			pvcUIDMap := make(map[string]types.UID)
			for _, pod := range podList.Items {
				pvcNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      pod.Name,
				}
				pvc := corev1.PersistentVolumeClaim{}
				err = env.Client.Get(env.Ctx, pvcNamespacedName, &pvc)
				Expect(err).ToNot(HaveOccurred())
				pvcUIDMap[pod.Name] = pvc.GetUID()
			}

			// Drain the node containing the primary pod and store the list of running pods
			podsOnPrimaryNode := nodes.DrainPrimary(
				env.Ctx, env.Client,
				namespace, clusterName,
				testTimeouts[testsUtils.DrainNode],
			)

			By("verifying failover after drain", func() {
				timeout := testTimeouts[testsUtils.ClusterIsReady]
				// Expect a failover to have happened
				Eventually(func() (string, error) {
					pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return "", err
					}
					return pod.Name, err
				}, timeout).ShouldNot(BeEquivalentTo(oldPrimary))
			})

			By("uncordon nodes and check new pods use old pvcs", func() {
				err := nodes.UncordonAll(env.Ctx, env.Client)
				Expect(err).ToNot(HaveOccurred())
				// Ensure evicted pods have restarted and are running.
				// one of them could have become the new primary.
				timeout := 300
				for _, podName := range podsOnPrimaryNode {
					namespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      podName,
					}
					Eventually(func() (bool, error) {
						pod := corev1.Pod{}
						err := env.Client.Get(env.Ctx, namespacedName, &pod)
						return utils.IsPodActive(pod) && utils.IsPodReady(pod), err
					}, timeout).Should(BeTrue())

					pod := corev1.Pod{}
					err = env.Client.Get(env.Ctx, namespacedName, &pod)
					Expect(err).ToNot(HaveOccurred())
					// Check that the PVC UID hasn't changed
					pvc := corev1.PersistentVolumeClaim{}
					err = env.Client.Get(env.Ctx, namespacedName, &pvc)
					Expect(pvc.GetUID(), err).To(BeEquivalentTo(pvcUIDMap[podName]))
				}
			})

			AssertDataExpectedCount(env, tableLocator, 2)
			AssertClusterStandbysAreStreaming(namespace, clusterName, 140)
		})

		// Scenario: all the pods of a cluster are on a single node and another schedulable node exists.
		// We perform the drain the node hosting the primary.
		// If PVCs can be moved: all the replicas will be killed and rescheduled to a different node,
		// then a switchover will be triggered, and the old primary will be killed and moved too.
		// The drain will succeed.
		// We have skipped this scenario on the local executors, Openshift, EKS, GKE
		// because here PVCs can not be moved, so this all replicas should be killed and can not be rescheduled on a
		// new node as there are none, the primary node can not be killed, therefore the drain fails.

		When("the cluster allows moving PVCs between nodes", func() {
			BeforeEach(func() {
				// AKS using rook allows moving PVCs between nodes
				if !MustGetEnvProfile().CanMovePVCAcrossNodes() {
					Skip("This test case is only applicable on clusters where PVC can be moved")
				}
			})
			It("can drain the primary pod's node with 3 pods on 1 nodes", func() {
				const namespacePrefix = "drain-node-e2e-pvc-on-one-nodes"
				var cordonNodes []string
				By("leaving only one node uncordoned", func() {
					// cordon all nodes but one
					for _, cordonNode := range nodesWithLabels[:len(nodesWithLabels)-1] {
						cordonNodes = append(cordonNodes, cordonNode)
						cmd := fmt.Sprintf("kubectl cordon %v", cordonNode)
						_, _, err := run.Run(cmd)
						Expect(err).ToNot(HaveOccurred())
					}
				})
				var err error
				// Create a cluster in a namespace we'll delete after the test
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterName, sampleFile, env)

				By("waiting for the jobs to be removed", func() {
					// Wait for jobs to be removed
					timeout := testTimeouts[testsUtils.ClusterIsReady]
					Eventually(func() (int, error) {
						podList, err := pods.List(env.Ctx, env.Client, namespace)
						if err != nil {
							return 0, err
						}
						return len(podList.Items), err
					}, timeout).Should(BeEquivalentTo(3))
				})

				// Load test data
				oldPrimary := clusterName + "-1"
				tableLocator := TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    "test",
				}
				AssertCreateTestData(env, tableLocator)

				// We create a mapping between the pod names and the UIDs of
				// their volumes. We do not expect the UIDs to change.
				// We take advantage of the fact that related PVCs and Pods have
				// the same name.
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				pvcUIDMap := make(map[string]types.UID)
				for _, pod := range podList.Items {
					pvcNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      pod.Name,
					}
					pvc := corev1.PersistentVolumeClaim{}
					err = env.Client.Get(env.Ctx, pvcNamespacedName, &pvc)
					Expect(err).ToNot(HaveOccurred())
					pvcUIDMap[pod.Name] = pvc.GetUID()
				}

				// We uncordon a cordoned node, so there will be a node for the PVCs
				// to move to.
				By(fmt.Sprintf("uncordon one more node '%v'", cordonNodes[0]), func() {
					cmd := fmt.Sprintf("kubectl uncordon %v", cordonNodes[0])
					_, _, err = run.Run(cmd)
					Expect(err).ToNot(HaveOccurred())
				})

				// Drain the node containing the primary pod and store the list of running pods
				podsOnPrimaryNode := nodes.DrainPrimary(
					env.Ctx, env.Client,
					namespace, clusterName,
					testTimeouts[testsUtils.DrainNode],
				)

				By("verifying failover after drain", func() {
					timeout := testTimeouts[testsUtils.ClusterIsReady]
					// Expect a failover to have happened
					Eventually(func() (string, error) {
						pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
						if err != nil {
							return "", err
						}
						return pod.Name, err
					}, timeout).ShouldNot(BeEquivalentTo(oldPrimary))
				})

				By("check new pods use old pvcs", func() {
					// Ensure evicted pods have restarted and are running.
					// one of them could have become the new primary.
					timeout := 300
					for _, podName := range podsOnPrimaryNode {
						namespacedName := types.NamespacedName{
							Namespace: namespace,
							Name:      podName,
						}
						Eventually(func() (bool, error) {
							pod := corev1.Pod{}
							err := env.Client.Get(env.Ctx, namespacedName, &pod)
							return utils.IsPodActive(pod) && utils.IsPodReady(pod), err
						}, timeout).Should(BeTrue())

						pod := corev1.Pod{}
						err = env.Client.Get(env.Ctx, namespacedName, &pod)
						// Check that the PVC UID hasn't changed
						pvc := corev1.PersistentVolumeClaim{}
						err = env.Client.Get(env.Ctx, namespacedName, &pvc)
						Expect(pvc.GetUID(), err).To(BeEquivalentTo(pvcUIDMap[podName]))
					}
				})

				AssertDataExpectedCount(env, tableLocator, 2)
				AssertClusterStandbysAreStreaming(namespace, clusterName, 140)
			})
		})
	})

	Context("Maintenance on, reuse pvc off", func() {
		// Set unique namespace
		const namespacePrefix = "drain-node-e2e-pvc-off-single-node"
		const sampleFile = fixturesDir + "/drain-node/cluster-drain-node-pvc-off.yaml.template"
		const clusterName = "cluster-drain-node"

		var namespace string
		BeforeEach(func() {
			// All GKE and AKS persistent disks are network storage located independently of the underlying Nodes, so
			// they don't get deleted after a Drain. Hence, even when using "reusePVC off", all the pods will
			// be recreated with the same name and will reuse the existing volume.
			if MustGetEnvProfile().CanMovePVCAcrossNodes() {
				Skip("This test case is only applicable on clusters with local storage")
			}
		})

		// With reusePVC set to off, draining a node should create new pods
		// on different nodes. We expect to see the cluster pods having
		// all different names from the initial ones after the drain.
		It("drains the primary pod's node, when all the pods are on a single node", func() {
			// We leave a single node uncordoned, so all the pods we create
			// will go there
			By("leaving a single uncordoned", func() {
				for _, cordonNode := range nodesWithLabels[:len(nodesWithLabels)-1] {
					cmd := fmt.Sprintf("kubectl cordon %v", cordonNode)
					_, _, err := run.Run(cmd)
					Expect(err).ToNot(HaveOccurred())
				}
			})
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Avoid pod from init jobs interfering with the tests
			By("waiting for the jobs to be removed", func() {
				// Wait for jobs to be removed
				timeout := testTimeouts[testsUtils.ClusterIsReady]
				Eventually(func() (int, error) {
					podList, err := pods.List(env.Ctx, env.Client, namespace)
					if err != nil {
						return 0, err
					}
					return len(podList.Items), err
				}, timeout).Should(BeEquivalentTo(3))
			})

			// Retrieve the names of the current pods. All of them should
			// not exist anymore after the drain
			var podsBeforeDrain []string
			By("retrieving the current pods' names", func() {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				for _, pod := range podList.Items {
					podsBeforeDrain = append(podsBeforeDrain, pod.Name)
				}
			})

			// Load test data
			tableLocator := TableLocator{
				Namespace:    namespace,
				ClusterName:  clusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    "test",
			}
			AssertCreateTestData(env, tableLocator)

			// We uncordon a cordoned node. New pods can go there.
			By("uncordon node for pod failover", func() {
				cmd := fmt.Sprintf("kubectl uncordon %v", nodesWithLabels[0])
				_, _, err := run.Run(cmd)
				Expect(err).ToNot(HaveOccurred())
			})

			// Drain the node containing the primary pod. Pods should be moved
			// to the node we've just uncordoned
			nodes.DrainPrimary(
				env.Ctx, env.Client,
				namespace, clusterName, testTimeouts[testsUtils.DrainNode],
			)

			// Expect pods to be recreated and to be ready
			AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)

			// Expect pods to be running on the uncordoned node and to have new names
			By("verifying cluster pods changed names", func() {
				timeout := 600
				Eventually(func(g Gomega) {
					matchingNames := 0
					podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					for _, pod := range podList.Items {
						// compare the old pod list with the current pod names
						for _, oldName := range podsBeforeDrain {
							if pod.GetName() == oldName {
								matchingNames++
							}
						}
					}
					g.Expect(len(podList.Items)).To(BeEquivalentTo(3))
					g.Expect(matchingNames).To(BeEquivalentTo(0))
				}, timeout).Should(Succeed())
			})

			AssertDataExpectedCount(env, tableLocator, 2)
			AssertClusterStandbysAreStreaming(namespace, clusterName, 140)
			err = nodes.UncordonAll(env.Ctx, env.Client)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("with a single instance cluster", Ordered, func() {
		const namespacePrefix = "drain-node-e2e-single-instance"
		const sampleFile = fixturesDir + "/drain-node/single-node-pdb-disabled.yaml.template"
		const clusterName = "cluster-single-instance-pdb"
		var namespace string

		BeforeAll(func() {
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
		})

		When("the PDB is disabled", func() {
			It("can drain the primary node and recover the cluster when uncordoned", func() {
				AssertCreateCluster(namespace, clusterName, sampleFile, env)

				var drainedNodeName string
				By("waiting for the jobs to be removed", func() {
					// Wait for jobs to be removed
					timeout := testTimeouts[testsUtils.ClusterIsReady]
					var podList *corev1.PodList
					Eventually(func() (int, error) {
						var err error
						podList, err = pods.List(env.Ctx, env.Client, namespace)
						if err != nil {
							return 0, err
						}
						return len(podList.Items), err
					}, timeout).Should(BeEquivalentTo(1))
					drainedNodeName = podList.Items[0].Spec.NodeName
				})

				// Load test data
				tableLocator := TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    "test",
				}
				AssertCreateTestData(env, tableLocator)

				// Drain the node containing the primary pod and store the list of running pods
				_ = nodes.DrainPrimary(
					env.Ctx, env.Client,
					namespace, clusterName,
					testTimeouts[testsUtils.DrainNode],
				)

				By("verifying the primary is now pending or somewhere else", func() {
					Eventually(func(g Gomega) {
						pod, err := pods.Get(env.Ctx, env.Client, namespace, clusterName+"-1")
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(pod).Should(SatisfyAny(
							HaveField("Spec.NodeName", Not(BeEquivalentTo(drainedNodeName))),
							HaveField("Status.Phase", BeEquivalentTo("Pending")),
						))
					}).WithTimeout(180 * time.Second).WithPolling(PollingTime * time.Second).Should(Succeed())
				})

				By("uncordoning all nodes", func() {
					err := nodes.UncordonAll(env.Ctx, env.Client)
					Expect(err).ToNot(HaveOccurred())
				})

				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
				AssertDataExpectedCount(env, tableLocator, 2)
			})
		})

		When("the PDB is enabled", func() {
			It("prevents the primary node from being drained", func() {
				By("enabling PDB", func() {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					updated := cluster.DeepCopy()
					updated.Spec.EnablePDB = ptr.To(true)
					err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
					Expect(err).ToNot(HaveOccurred())
				})

				By("having the draining of the primary node rejected", func() {
					var primaryNode string
					Eventually(func(g Gomega) {
						pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
						g.Expect(err).ToNot(HaveOccurred())
						primaryNode = pod.Spec.NodeName
					}, 60).Should(Succeed())

					// Draining the primary pod's node
					Eventually(func(g Gomega) {
						cmd := fmt.Sprintf(
							"kubectl drain %v --ignore-daemonsets --delete-emptydir-data --force --timeout=%ds",
							primaryNode, 60)
						_, stderr, err := run.Unchecked(cmd)
						g.Expect(err).To(HaveOccurred())
						g.Expect(stderr).To(ContainSubstring("Cannot evict pod as it would violate the pod's disruption budget"))
					}, 60).Should(Succeed())
				})

				By("uncordoning all nodes", func() {
					err := nodes.UncordonAll(env.Ctx, env.Client)
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})
	})
})
