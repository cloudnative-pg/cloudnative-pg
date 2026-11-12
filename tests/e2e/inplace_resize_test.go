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
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
		versionInfo, err := env.Interface.Discovery().ServerVersion()
		Expect(err).ToNot(HaveOccurred())
		currentVersion, err := semver.NewVersion(strings.TrimPrefix(versionInfo.String(), "v"))
		Expect(err).ToNot(HaveOccurred())
		coreResources, err := env.Interface.Discovery().ServerResourcesForGroupVersion("v1")
		Expect(err).ToNot(HaveOccurred())
		hasResize := slices.ContainsFunc(coreResources.APIResources,
			func(r metav1.APIResource) bool { return r.Name == "pods/resize" })
		if currentVersion.LessThan(semver.MustParse("1.33.0")) || !hasResize {
			Skip("This test runs only on Kubernetes 1.33 or greater, with in-place pod resize enabled")
		}
	})

	It("resizes the pods in place, and recreates them when the memory limit decreases", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)

		// the kubelet sorts the statuses by name, and other actors can inject containers
		postgresStatus := func(pod corev1.Pod) corev1.ContainerStatus {
			return pod.Status.ContainerStatuses[slices.IndexFunc(pod.Status.ContainerStatuses,
				func(status corev1.ContainerStatus) bool { return status.Name == specs.PostgresContainerName })]
		}
		listPods := func(g Gomega) []corev1.Pod {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(podList.Items).To(HaveLen(3))
			return podList.Items
		}
		setLimits := func(cpu, memory string) {
			Eventually(func(g Gomega) error {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				cluster.Spec.Resources.Limits = corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(cpu),
					corev1.ResourceMemory: resource.MustParse(memory),
				}
				return env.Client.Update(env.Ctx, cluster)
			}, RetryTimeout, PollingTime).Should(Succeed())
		}

		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primary := cluster.Status.CurrentPrimary
		original := make(map[string]corev1.Pod)
		for _, pod := range listPods(Default) {
			original[pod.Name] = pod
		}

		By("increasing the cpu and memory limits", func() {
			setLimits("400m", "768Mi")
			Eventually(func(g Gomega) {
				for _, pod := range listPods(g) {
					// the resources the kubelet applied to the running container
					applied := postgresStatus(pod).Resources
					g.Expect(applied).ToNot(BeNil())
					g.Expect(applied.Limits.Cpu().String()).To(Equal("400m"), pod.Name)
					g.Expect(applied.Limits.Memory().String()).To(Equal("768Mi"), pod.Name)
				}
			}, testTimeouts[timeouts.PodRollout]).Should(Succeed())
		})

		By("verifying that no pod was recreated or restarted, and no switchover happened", func() {
			Consistently(func(g Gomega) {
				for _, pod := range listPods(g) {
					g.Expect(pod.UID).To(Equal(original[pod.Name].UID), pod.Name)
					g.Expect(postgresStatus(pod).RestartCount).
						To(Equal(postgresStatus(original[pod.Name]).RestartCount), pod.Name)
				}
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cluster.Status.CurrentPrimary).To(Equal(primary))
				g.Expect(cluster.Status.TargetPrimary).To(Equal(primary))
			}, 30, 5).Should(Succeed())
			clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
		})

		By("decreasing the memory limit, which recreates the pods", func() {
			setLimits("400m", "512Mi")
			Eventually(func(g Gomega) {
				for _, pod := range listPods(g) {
					g.Expect(pod.UID).ToNot(Equal(original[pod.Name].UID), pod.Name)
					postgres := slices.IndexFunc(pod.Spec.Containers,
						func(container corev1.Container) bool { return container.Name == specs.PostgresContainerName })
					g.Expect(pod.Spec.Containers[postgres].Resources.Limits.Memory().String()).To(Equal("512Mi"), pod.Name)
				}
			}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())
			clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
		})
	})
})
