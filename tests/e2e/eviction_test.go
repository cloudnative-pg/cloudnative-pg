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

	"github.com/avast/retry-go/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This test evicts a CNPG cluster's pod to simulate out of memory issues.
// Under this condition, the operator will immediately delete that evicted pod
// and a new pod will be created after that. We are using the status API to patch the pod
// status to phase=Failed, reason=Evict to simulate the eviction.
// There are several test cases:
// 1. Eviction of primary pod in a single instance cluster (using patch simulate) -- included
// 2. Eviction of standby pod in a multiple instance cluster (using patch simulate) -- included
// 3. Eviction of primary pod in a multiple instance cluster (using drain simulate) -- included
// Please note that, for manually testing, we can also use the pgbench to simulate the OOM, but given that the
// process of eviction is
// 1. the node is running out of memory
// 2. node started to evict pod, from the lowest priority first so BestEffort -> Burstable
// see: https://kubernetes.io/docs/concepts/scheduling-eviction/node-pressure-eviction/
// #pod-selection-for-kubelet-eviction
// we can not guarantee how much memory is left in node and when it will get OOM and start eviction,
// so we choose to use patch and drain to simulate the eviction. The patch status issued one problem,
// when evicting the primary pod of multiple clusters.

var _ = Describe("Pod eviction", Serial, Label(tests.LabelDisruptive), func() {
	const (
		level                    = tests.Low
		singleInstanceSampleFile = fixturesDir + "/eviction/single-instance-cluster.yaml.template"
		multiInstanceSampleFile  = fixturesDir + "/eviction/multi-instance-cluster.yaml.template"
	)

	evictPod := func(podName string, namespace string, env *environment.TestingEnvironment, timeoutSeconds uint) error {
		var pod corev1.Pod
		err := env.Client.Get(env.Ctx,
			ctrlclient.ObjectKey{Namespace: namespace, Name: podName},
			&pod)
		if err != nil {
			return err
		}
		oldPodRV := pod.GetResourceVersion()
		GinkgoWriter.Printf("Old resource version for %v pod: %v \n", pod.GetName(), oldPodRV)
		patch := ctrlclient.MergeFrom(pod.DeepCopy())
		pod.Status.Phase = "Failed"
		pod.Status.Reason = "Evicted"
		// Patching the Pod status
		err = env.Client.Status().Patch(env.Ctx, &pod, patch)
		if err != nil {
			return fmt.Errorf("failed to patch status for Pod: %v", pod.Name)
		}

		// Checking the Pod is actually evicted and resource version changed
		err = retry.New(
			retry.Delay(2*time.Second),
			retry.Attempts(timeoutSeconds)).
			Do(
				func() error {
					err = env.Client.Get(env.Ctx,
						ctrlclient.ObjectKey{Namespace: namespace, Name: podName},
						&pod)
					if err != nil {
						return err
					}
					// Sometimes the eviction status is too short, we can not see if has been changed.
					// We checked the resource version here
					if oldPodRV != pod.GetResourceVersion() {
						GinkgoWriter.Printf("New resource version for %v pod: %v \n",
							pod.GetName(), pod.GetResourceVersion())
						return nil
					}
					return fmt.Errorf("pod %v has not been evicted", pod.Name)
				},
			)
		return err
	}

	Context("Pod eviction in single instance cluster", Ordered, func() {
		var namespace string

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
		})

		BeforeAll(func() {
			// limit the case running on local kind env as we are using taint to simulate the eviction
			// we do not know if other cloud vendor crd controller is running on the node been evicted
			if !IsLocal() {
				Skip("This test is only run on local cluster")
			}
			const namespacePrefix = "single-instance-pod-eviction"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			By("creating a cluster", func() {
				// Create a cluster in a namespace we'll delete after the test
				clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, singleInstanceSampleFile)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterName, singleInstanceSampleFile, env)
			})
		})

		It("evicts the primary pod in single instance cluster", func() {
			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, singleInstanceSampleFile)
			Expect(err).ToNot(HaveOccurred())
			podName := clusterName + "-1"
			err = evictPod(podName, namespace, env, 60)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the pod to be ready again", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}
				Eventually(func() (bool, error) {
					pod := corev1.Pod{}
					err := env.Client.Get(env.Ctx, namespacedName, &pod)
					if err != nil {
						return false, nil
					}
					return utils.IsPodActive(pod) && utils.IsPodReady(pod), err
				}, 60).Should(BeTrue())
			})

			By("checking the cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)
			})
		})
	})

	Context("Pod eviction in a multiple instance cluster", Ordered, func() {
		var (
			namespace       string
			taintNodeName   string
			needRemoveTaint bool
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
			if !IsLocal() {
				Skip("This test is only run on local cluster")
			}
		})

		BeforeAll(func() {
			const namespacePrefix = "multi-instance-pod-eviction"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			By("Creating a cluster with multiple instances", func() {
				// Create a cluster in a namespace and shared in containers, we'll delete after the test
				clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, multiInstanceSampleFile)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterName, multiInstanceSampleFile, env)
			})

			By("retrieving the nodeName for primary pod", func() {
				var primaryPod *corev1.Pod
				clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, multiInstanceSampleFile)
				Expect(err).ToNot(HaveOccurred())
				primaryPod, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				taintNodeName = primaryPod.Spec.NodeName
			})
		})
		AfterAll(func() {
			if needRemoveTaint {
				By("cleaning the taint on node", func() {
					cmd := fmt.Sprintf("kubectl taint nodes %v node.kubernetes.io/memory-pressure:NoExecute-",
						taintNodeName)
					_, _, err := run.Run(cmd)
					Expect(err).ToNot(HaveOccurred())
				})
			}
		})

		It("evicts the replica pod in multiple instance cluster", func() {
			var podName string

			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, multiInstanceSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Find the standby pod
			By("getting standby pod to evict", func() {
				podList, _ := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(len(podList.Items)).To(BeEquivalentTo(3))
				for _, pod := range podList.Items {
					// Avoid parting non ready nodes, non active nodes, or primary nodes
					if specs.IsPodStandby(pod) {
						podName = pod.Name
						break
					}
				}
				Expect(podName).ToNot(BeEmpty())
			})

			err = evictPod(podName, namespace, env, 60)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the replica to be ready again", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}
				Eventually(func() (bool, error) {
					pod := corev1.Pod{}
					err := env.Client.Get(env.Ctx, namespacedName, &pod)
					if err != nil {
						return false, nil
					}
					return utils.IsPodActive(pod) && utils.IsPodReady(pod), err
				}, 60).Should(BeTrue())
			})

			By("checking the cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)
			})
		})

		It("evicts the primary pod in multiple instance cluster", func() {
			var primaryPod *corev1.Pod

			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, multiInstanceSampleFile)
			Expect(err).ToNot(HaveOccurred())
			primaryPod, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			// We can not use patch to simulate the eviction of a primary pod;
			// so that we use taint to simulate the real eviction

			By("taint the node to simulate pod been evicted", func() {
				cmd := fmt.Sprintf("kubectl taint nodes %v node.kubernetes.io/memory-pressure:NoExecute", taintNodeName)
				_, _, err = run.Run(cmd)
				Expect(err).ToNot(HaveOccurred())
				needRemoveTaint = true

				time.Sleep(3 * time.Second)

				cmd = fmt.Sprintf("kubectl taint nodes %v node.kubernetes.io/memory-pressure:NoExecute-", taintNodeName)
				_, _, err = run.Run(cmd)
				Expect(err).ToNot(HaveOccurred())
				needRemoveTaint = false
			})

			By("checking switchover happens", func() {
				Eventually(func() (bool, error) {
					podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return false, err
					}
					for _, p := range podList.Items {
						if specs.IsPodPrimary(p) && primaryPod.GetName() != p.GetName() {
							return true, nil
						}
					}
					return false, nil
				}, 60).Should(BeTrue())
			})

			// Pod need rejoin, need more time
			By("checking the cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)
			})
		})
	})
})
