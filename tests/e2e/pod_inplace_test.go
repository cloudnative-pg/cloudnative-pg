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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod Inplace Resource Updates", Label(tests.LabelSelfHealing), func() {
	const (
		namespacePrefix = "cluster-pod-inplace"
		sampleFile      = fixturesDir + "/pod_inplace/cluster-pod-inplace.yaml.template"
		clusterName     = "cluster-pod-inplace"
		level           = tests.Medium
	)
	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("correctly handles in-place resource updates", func() {
		var err error
		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// Wait for cluster to be ready
		By("waiting for cluster to be ready", func() {
			timeout := 120
			Eventually(func() (bool, error) {
				cluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}, cluster)
				if err != nil {
					return false, err
				}
				return cluster.Status.Phase == apiv1.PhaseHealthy, nil
			}, timeout).Should(BeTrue())
		})

		// Test 1: In-place resource update with NotRequired policy
		By("testing in-place resource update with NotRequired policy", func() {
			// Get the current pod UIDs before update
			podList := &corev1.PodList{}
			err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"cnpg.io/cluster": clusterName})
			Expect(err).ToNot(HaveOccurred())

			originalPodUIDs := make(map[string]types.UID)
			for _, pod := range podList.Items {
				originalPodUIDs[pod.Name] = pod.UID
			}

			// Update cluster resources
			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Update CPU and memory resources (smaller values for faster testing)
			cluster.Spec.Resources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("400m"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}

			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Wait for the update to be applied
			timeout := 60
			Eventually(func() (bool, error) {
				updatedCluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}, updatedCluster)
				if err != nil {
					return false, err
				}

				// Check if resources are updated
				if updatedCluster.Spec.Resources.Requests.Cpu().Cmp(resource.MustParse("200m")) != 0 {
					return false, nil
				}
				if updatedCluster.Spec.Resources.Requests.Memory().Cmp(resource.MustParse("512Mi")) != 0 {
					return false, nil
				}

				// Check if pods still have the same UIDs (in-place update)
				currentPodList := &corev1.PodList{}
				err = env.Client.List(env.Ctx, currentPodList, ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{"cnpg.io/cluster": clusterName})
				if err != nil {
					return false, err
				}

				for _, pod := range currentPodList.Items {
					if originalPodUIDs[pod.Name] != pod.UID {
						return false, nil // Pod was recreated
					}
				}

				return true, nil
			}, timeout).Should(BeTrue())

			// Verify pods are still running and healthy
			Eventually(func() (bool, error) {
				podList := &corev1.PodList{}
				err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{"cnpg.io/cluster": clusterName})
				if err != nil {
					return false, err
				}

				for _, pod := range podList.Items {
					if !utils.IsPodReady(pod) {
						return false, nil
					}
				}
				return true, nil
			}, timeout).Should(BeTrue())
		})

		// Test 2: Resource update with RestartContainer policy
		By("testing resource update with RestartContainer policy", func() {
			// Update the cluster to use RestartContainer policy for CPU
			cluster := &apiv1.Cluster{}
			err := env.Client.Get(env.Ctx, types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Update container resize policy to require restart for CPU changes
			cluster.Spec.ContainerResizePolicy = []corev1.ContainerResizePolicy{
				{
					ResourceName:  corev1.ResourceCPU,
					RestartPolicy: corev1.RestartContainer,
				},
				{
					ResourceName:  corev1.ResourceMemory,
					RestartPolicy: corev1.NotRequired,
				},
			}

			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Get current pod UIDs
			podList := &corev1.PodList{}
			err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"cnpg.io/cluster": clusterName})
			Expect(err).ToNot(HaveOccurred())

			originalPodUIDs := make(map[string]types.UID)
			for _, pod := range podList.Items {
				originalPodUIDs[pod.Name] = pod.UID
			}

			// Update CPU resources (should trigger pod recreation)
			cluster.Spec.Resources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("300m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("600m"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}

			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Wait for pods to be recreated (UIDs should change)
			timeout := 60
			Eventually(func() (bool, error) {
				currentPodList := &corev1.PodList{}
				err := env.Client.List(env.Ctx, currentPodList, ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{"cnpg.io/cluster": clusterName})
				if err != nil {
					return false, err
				}

				// Check if at least one pod was recreated (UID changed)
				for _, pod := range currentPodList.Items {
					if originalPodUIDs[pod.Name] != pod.UID {
						return true, nil // At least one pod was recreated
					}
				}
				return false, nil
			}, timeout).Should(BeTrue())

			// Verify cluster is still healthy after recreation
			Eventually(func() (bool, error) {
				cluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}, cluster)
				if err != nil {
					return false, err
				}
				return cluster.Status.Phase == apiv1.PhaseHealthy, nil
			}, timeout).Should(BeTrue())
		})

		// Test 3: Memory-only update with NotRequired policy
		By("testing memory-only update with NotRequired policy", func() {
			// Update policy to allow in-place memory updates
			cluster := &apiv1.Cluster{}
			err := env.Client.Get(env.Ctx, types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}, cluster)
			Expect(err).ToNot(HaveOccurred())

			cluster.Spec.ContainerResizePolicy = []corev1.ContainerResizePolicy{
				{
					ResourceName:  corev1.ResourceCPU,
					RestartPolicy: corev1.NotRequired,
				},
				{
					ResourceName:  corev1.ResourceMemory,
					RestartPolicy: corev1.NotRequired,
				},
			}

			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Get current pod UIDs
			podList := &corev1.PodList{}
			err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"cnpg.io/cluster": clusterName})
			Expect(err).ToNot(HaveOccurred())

			originalPodUIDs := make(map[string]types.UID)
			for _, pod := range podList.Items {
				originalPodUIDs[pod.Name] = pod.UID
			}

			// Update only memory resources
			cluster.Spec.Resources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("300m"),
					corev1.ResourceMemory: resource.MustParse("768Mi"), // Only memory changed
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("600m"),
					corev1.ResourceMemory: resource.MustParse("1.5Gi"), // Only memory changed
				},
			}

			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Wait for the update to be applied in-place
			timeout := 60
			Eventually(func() (bool, error) {
				updatedCluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}, updatedCluster)
				if err != nil {
					return false, err
				}

				// Check if memory resources are updated
				if updatedCluster.Spec.Resources.Requests.Memory().Cmp(resource.MustParse("768Mi")) != 0 {
					return false, nil
				}

				// Check if pods still have the same UIDs (in-place update)
				currentPodList := &corev1.PodList{}
				err = env.Client.List(env.Ctx, currentPodList, ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{"cnpg.io/cluster": clusterName})
				if err != nil {
					return false, err
				}

				for _, pod := range currentPodList.Items {
					if originalPodUIDs[pod.Name] != pod.UID {
						return false, nil // Pod was recreated
					}
				}

				return true, nil
			}, timeout).Should(BeTrue())
		})

		// Test 4: Verify resource limits are actually applied
		By("verifying resource limits are applied to pods", func() {
			podList := &corev1.PodList{}
			err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"cnpg.io/cluster": clusterName})
			Expect(err).ToNot(HaveOccurred())

			for _, pod := range podList.Items {
				// Find the postgres container
				for _, container := range pod.Spec.Containers {
					if container.Name == "postgres" {
						// Verify CPU limits
						Expect(container.Resources.Limits.Cpu().Cmp(resource.MustParse("600m"))).To(Equal(0))
						// Verify Memory limits
						Expect(container.Resources.Limits.Memory().Cmp(resource.MustParse("1.5Gi"))).To(Equal(0))
						// Verify CPU requests
						Expect(container.Resources.Requests.Cpu().Cmp(resource.MustParse("300m"))).To(Equal(0))
						// Verify Memory requests
						Expect(container.Resources.Requests.Memory().Cmp(resource.MustParse("768Mi"))).To(Equal(0))
					}
				}
			}
		})
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			namespaces.DumpNamespaceObjects(
				env.Ctx, env.Client,
				namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
		err := namespaces.DeleteNamespaceAndWait(env.Ctx, env.Client, namespace, 120)
		Expect(err).ToNot(HaveOccurred())
	})
})
