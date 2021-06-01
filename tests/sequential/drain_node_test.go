/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package sequential

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
)

// Set of tests in which we check that operator is able to failover primary and bring back
// replica's when we drain node
var _ = Describe("E2E Drain Node", func() {
	var nodesWithLabels []string

	BeforeEach(func() {
		nodes, _ := env.GetNodeList()
		// We label all the nodes where we could run the workloads
		for _, node := range nodes.Items {
			if (node.Spec.Unschedulable != true) && (len(node.Spec.Taints) == 0) {
				nodesWithLabels = append(nodesWithLabels, node.Name)
				cmd := fmt.Sprintf("kubectl label node %v drain=drain --overwrite", node.Name)
				_, _, err := tests.Run(cmd)
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
		UncordonAllNodes()
		for _, node := range nodesWithLabels {
			cmd := fmt.Sprintf("kubectl label node %v drain- ", node)
			_, _, err := tests.Run(cmd)
			Expect(err).ToNot(HaveOccurred())
		}
		nodesWithLabels = nil
	})

	Context("Maintenance on, reuse pvc on", func() {
		// Initialize empty global namespace variable
		namespace := ""
		const sampleFile = fixturesDir + "/drain-node/cluster-example.yaml"
		const clusterName = "cluster-example"

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

		// We cordon one node, so pods will run on one or two nodes. This
		// is only to create a harder situation for the operator.
		// We then drain the node containing the primary and expect the pod(s)
		// to be back only when its PVC is available. On GKE with the default
		// storage class and on AKS with Rook this happens immediately. When
		// the storage is bound to the node, we have to uncordon the node
		// first. We uncordon it in all cases and check for the UIDs of the
		// PVC(s).
		// TODO: since the podDisruptionBudget is dropped when reusePVC
		// is on, if case the pods are all on the same node the drain will
		// fail. However, this shouldn't happen unless one of the two nodes
		// is overloaded, since the anti-affinity should keep pods away from
		// each other.
		It("can drain the primary pod's node", func() {
			// Set unique namespace
			namespace = "drain-node-e2e-pvc-on-two-nodes"

			By("leaving only two nodes uncordoned", func() {
				// mark a node unschedulable so the pods will be distributed only on two nodes
				for _, cordonNode := range nodesWithLabels[:len(nodesWithLabels)-2] {
					cmd := fmt.Sprintf("kubectl cordon %v", cordonNode)
					_, _, err := tests.Run(cmd)
					Expect(err).ToNot(HaveOccurred())
				}
			})

			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("waiting for the jobs to be removed", func() {
				// Wait for jobs to be removed
				timeout := 180
				Eventually(func() (int, error) {
					podList, err := env.GetPodList(namespace)
					return len(podList.Items), err
				}, timeout).Should(BeEquivalentTo(3))
			})

			// Load test data
			oldPrimary := clusterName + "-1"
			AssertTestDataCreation(namespace, clusterName)

			// We create a mapping between the pod names and the UIDs of
			// their volumes. We do not expect the UIDs to change.
			// We take advantage of the fact that related PVCs and Pods have
			// the same name.
			podList, err := env.GetClusterPodList(namespace, clusterName)
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

			// Drain the node containing the primary pod
			podNames := drainPrimaryNode(namespace, clusterName)

			By("verifying failover after drain", func() {
				timeout := 180
				// Expect a failover to have happened
				Eventually(func() (string, error) {
					pod, err := env.GetClusterPrimary(namespace, clusterName)
					return pod.Name, err
				}, timeout).ShouldNot(BeEquivalentTo(oldPrimary))
			})

			By("uncordon nodes and check new pods use old pvcs", func() {
				UncordonAllNodes()
				// ensure evicted pods have restarted and are running as replicas
				timeout := 300
				for _, podName := range podNames {
					namespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      podName,
					}
					Eventually(func() (bool, error) {
						pod := corev1.Pod{}
						err := env.Client.Get(env.Ctx, namespacedName, &pod)
						return utils.IsPodActive(pod) && utils.IsPodReady(pod) && specs.IsPodStandby(pod), err
					}, timeout).Should(BeTrue())

					pod := corev1.Pod{}
					err = env.Client.Get(env.Ctx, namespacedName, &pod)
					// Check that the PVC UID hasn't changed
					pvc := corev1.PersistentVolumeClaim{}
					err = env.Client.Get(env.Ctx, namespacedName, &pvc)
					Expect(pvc.GetUID(), err).To(BeEquivalentTo(pvcUIDMap[podName]))
				}
			})

			// Expect the test data previously created to be available
			AssertTestDataExistence(namespace, clusterName)
			assertClusterStandbysAreStreaming(namespace, clusterName)
		})
	})

	Context("Maintenance on, reuse pvc off", func() {
		// Set unique namespace
		const namespace = "drain-node-e2e-pvc-off-single-node"
		const sampleFile = fixturesDir + "/drain-node/cluster-example-pvc-off.yaml"
		const clusterName = "cluster-example"

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

		// With reusePVC set to off, draining a node should create new pods
		// on different nodes. We expect to see the cluster pods having
		// all different names from the initial ones after the drain.
		It("drains the primary pod's node, when all the pods are on a single node", func() {
			// We leave a single node uncordoned, so all the pods we create
			// will go there
			By("leaving a single uncordoned", func() {
				for _, cordonNode := range nodesWithLabels[:len(nodesWithLabels)-1] {
					cmd := fmt.Sprintf("kubectl cordon %v", cordonNode)
					_, _, err := tests.Run(cmd)
					Expect(err).ToNot(HaveOccurred())
				}
			})

			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Avoid pod from init jobs interfering with the tests
			By("waiting for the jobs to be removed", func() {
				// Wait for jobs to be removed
				timeout := 180
				Eventually(func() (int, error) {
					podList, err := env.GetPodList(namespace)
					return len(podList.Items), err
				}, timeout).Should(BeEquivalentTo(3))
			})

			// Retrieve the names of the current pods. All of them should
			// not exists anymore after the drain
			var podsBeforeDrain []string
			By("retrieving the current pods' names", func() {
				podList, err := env.GetClusterPodList(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				for _, pod := range podList.Items {
					podsBeforeDrain = append(podsBeforeDrain, pod.Name)
				}
			})

			// Load test data
			AssertTestDataCreation(namespace, clusterName)

			// We uncordon a cordoned node. New pods can go there.
			By("uncordon node for pod failover", func() {
				cmd := fmt.Sprintf("kubectl uncordon %v", nodesWithLabels[0])
				_, _, err := tests.Run(cmd)
				Expect(err).ToNot(HaveOccurred())
			})

			// Drain the node containing the primary pod. Pods should be moved
			// to the node we've just uncordoned
			drainPrimaryNode(namespace, clusterName)

			// Expect pods to be recreated and to be ready
			AssertClusterIsReady(namespace, clusterName, 600, env)

			// Expect pods to be running on the uncordoned node and to have new names
			By("verifying cluster pods changed names", func() {
				timeout := 300
				Eventually(func() (bool, error) {
					matchingNames := int32(0)
					isClusterReady := false
					podList, err := env.GetClusterPodList(namespace, clusterName)
					for _, pod := range podList.Items {
						Expect(pod.Spec.NodeName).To(BeEquivalentTo(nodesWithLabels[0]))
						// compare the old pod list with the current pod names
						for _, oldName := range podsBeforeDrain {
							if pod.GetName() == oldName {
								matchingNames++
							}
						}
					}
					if len(podList.Items) == 3 && matchingNames == 0 {
						isClusterReady = true
					}
					return isClusterReady, err
				}, timeout).Should(BeTrue())
			})

			// Expect the test data previously created to be available
			AssertTestDataExistence(namespace, clusterName)
			assertClusterStandbysAreStreaming(namespace, clusterName)
			UncordonAllNodes()
		})
	})
})

// drainPrimaryNode drains the node containing the primary pod.
// It returns the names of the pods that were running on that node
func drainPrimaryNode(namespace string, clusterName string) []string {
	var primaryNode string
	var podNames []string
	By("identifying primary node and draining", func() {
		pod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primaryNode = pod.Spec.NodeName

		// Gather the pods running on this node
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			if pod.Spec.NodeName == primaryNode {
				podNames = append(podNames, pod.Name)
			}
		}

		// Draining the primary pod's node
		cmd := fmt.Sprintf("kubectl drain %v --ignore-daemonsets --delete-local-data --force", primaryNode)
		timeout := 900
		Eventually(func() error {
			_, _, err := tests.RunUnchecked(cmd)
			return err
		}, timeout).ShouldNot(HaveOccurred())
	})

	By("ensuring no cluster pod is still running on the drained node", func() {
		timeout := 60
		Eventually(func() ([]string, error) {
			var usedNodes []string
			podList, err := env.GetClusterPodList(namespace, clusterName)
			for _, pod := range podList.Items {
				usedNodes = append(usedNodes, pod.Spec.NodeName)
			}
			return usedNodes, err
		}, timeout).ShouldNot(ContainElement(primaryNode))
	})

	return podNames
}

// assertClusterStandbysAreStreaming verifies that all the standbys of a
// cluster have a wal receiver running.
func assertClusterStandbysAreStreaming(namespace string, clusterName string) {
	podList, err := env.GetClusterPodList(namespace, clusterName)
	Expect(err).NotTo(HaveOccurred())
	primary, err := env.GetClusterPrimary(namespace, clusterName)
	Expect(err).NotTo(HaveOccurred())
	for _, pod := range podList.Items {
		// Primary should be ignored
		if pod.GetName() == primary.GetName() {
			continue
		}
		timeout := time.Second
		out, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &timeout,
			"psql", "-U", "postgres", "-tAc", "SELECT count(*) FROM pg_stat_wal_receiver")
		Expect(err).NotTo(HaveOccurred())
		value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
		Expect(value, atoiErr).To(BeEquivalentTo(1), "Pod %v not streaming", pod.Name)
	}
}
