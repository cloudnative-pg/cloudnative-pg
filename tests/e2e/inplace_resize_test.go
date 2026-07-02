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

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests for the inPlace resources update strategy: resource-only
// changes are applied to the running instance pods through the resize
// subresource, without recreating them; changes that cannot be applied in
// place, like memory limit decreases, fall back to the usual rolling update.
var _ = Describe("In-place resource updates", Label(tests.LabelPostgresConfiguration), func() {
	const (
		sampleFile      = fixturesDir + "/inplace_resize/cluster-inplace-resize.yaml.template"
		clusterName     = "cluster-inplace-resize"
		namespacePrefix = "inplace-resize"
		level           = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		Expect(utils.DetectPodsResize(env.APIExtensionClient.Discovery())).To(Succeed())
		if !utils.HavePodsResize() {
			Skip("This test requires in-place pod resize support (Kubernetes 1.33+)")
		}
	})

	getPostgresResources := func(pod *corev1.Pod) corev1.ResourceRequirements {
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Name == specs.PostgresContainerName {
				return pod.Spec.Containers[i].Resources
			}
		}
		return corev1.ResourceRequirements{}
	}

	getPostgresContainerStatus := func(pod *corev1.Pod) *corev1.ContainerStatus {
		for i := range pod.Status.ContainerStatuses {
			if pod.Status.ContainerStatuses[i].Name == specs.PostgresContainerName {
				return &pod.Status.ContainerStatuses[i]
			}
		}
		return nil
	}

	updateClusterResources := func(namespace string, resources corev1.ResourceRequirements) {
		Eventually(func(g Gomega) error {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())

			cluster.Spec.Resources = resources
			return env.Client.Update(env.Ctx, cluster)
		}, RetryTimeout, PollingTime).Should(Succeed())
	}

	It("applies resource changes without recreating the pods", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)

		originalPodUIDs := make(map[string]types.UID)
		originalRestartCounts := make(map[string]int32)

		By("gathering the current pods", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(podList.Items).To(HaveLen(3))
			for i := range podList.Items {
				pod := &podList.Items[i]
				originalPodUIDs[pod.Name] = pod.UID
				containerStatus := getPostgresContainerStatus(pod)
				Expect(containerStatus).ToNot(BeNil())
				originalRestartCounts[pod.Name] = containerStatus.RestartCount
			}
		})

		increasedResources := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("150m"),
				corev1.ResourceMemory: resource.MustParse("320Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("400m"),
				corev1.ResourceMemory: resource.MustParse("768Mi"),
			},
		}

		By("increasing cpu and memory of the cluster", func() {
			updateClusterResources(namespace, increasedResources)
		})

		By("waiting for the new resources to be applied to the running containers", func() {
			Eventually(func(g Gomega) {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(podList.Items).To(HaveLen(3))

				for i := range podList.Items {
					pod := &podList.Items[i]

					liveResources := getPostgresResources(pod)
					g.Expect(liveResources.Limits.Cpu().Cmp(*increasedResources.Limits.Cpu())).
						To(BeZero(), "pod %s cpu limit", pod.Name)
					g.Expect(liveResources.Limits.Memory().Cmp(*increasedResources.Limits.Memory())).
						To(BeZero(), "pod %s memory limit", pod.Name)

					// the kubelet reports the resources actually applied to
					// the running container
					containerStatus := getPostgresContainerStatus(pod)
					g.Expect(containerStatus).ToNot(BeNil())
					g.Expect(containerStatus.Resources).ToNot(BeNil())
					g.Expect(containerStatus.Resources.Limits.Memory().Cmp(*increasedResources.Limits.Memory())).
						To(BeZero(), "pod %s applied memory limit", pod.Name)
				}
			}, testTimeouts[timeouts.PodRollout]).Should(Succeed())
		})

		By("verifying no pod was recreated or restarted", func() {
			// the resize must leave the drift detection quiescent: no pod
			// recreation may happen after the resources have been applied
			Consistently(func(g Gomega) {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(podList.Items).To(HaveLen(3))

				for i := range podList.Items {
					pod := &podList.Items[i]
					g.Expect(pod.UID).To(Equal(originalPodUIDs[pod.Name]),
						"pod %s has been recreated", pod.Name)
					containerStatus := getPostgresContainerStatus(pod)
					g.Expect(containerStatus).ToNot(BeNil())
					g.Expect(containerStatus.RestartCount).To(Equal(originalRestartCounts[pod.Name]),
						"container of pod %s has been restarted", pod.Name)
				}
			}, 30, 5).Should(Succeed())

			clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
		})

		By("decreasing the memory limit", func() {
			decreasedResources := *increasedResources.DeepCopy()
			decreasedResources.Limits[corev1.ResourceMemory] = resource.MustParse("512Mi")
			decreasedResources.Requests[corev1.ResourceMemory] = resource.MustParse("256Mi")
			updateClusterResources(namespace, decreasedResources)
		})

		By("waiting for the pods to be recreated by a rolling update", func() {
			Eventually(func(g Gomega) {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(podList.Items).To(HaveLen(3))

				for i := range podList.Items {
					pod := &podList.Items[i]
					g.Expect(pod.UID).ToNot(Equal(originalPodUIDs[pod.Name]),
						"pod %s was expected to be recreated", pod.Name)

					liveResources := getPostgresResources(pod)
					g.Expect(liveResources.Limits.Memory().Cmp(resource.MustParse("512Mi"))).
						To(BeZero(), "pod %s memory limit", pod.Name)
				}
			}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())

			clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
		})
	})
})
