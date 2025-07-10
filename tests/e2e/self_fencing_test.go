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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/nodes"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Self-fencing with liveness probe", Serial, Label(tests.LabelDisruptive), func() {
	const (
		level           = tests.Lowest
		namespacePrefix = "self-fencing"
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if !IsLocal() {
			Skip("This test is only run on local cluster")
		}
	})

	verifyIsolatedPrimary := func(namespace, isolatedPod, isolatedNode string, livenessPingerEnabled bool) {
		By("verifying the isolatedPod behaviour", func() {
			defaultCommand := fmt.Sprintf(
				"docker exec %v crictl ps -a -q "+
					"--label io.kubernetes.pod.namespace=%s,io.kubernetes.pod.name=%s "+
					"--name postgres", isolatedNode, namespace, isolatedPod)

			if livenessPingerEnabled {
				Eventually(func(g Gomega) {
					out, _, err := run.Unchecked(fmt.Sprintf("%s -s Exited", defaultCommand))
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(out).ToNot(BeEmpty())
					if out != "" {
						GinkgoWriter.Printf("Container %s (%s) has been terminated\n",
							isolatedPod, strings.TrimSpace(out))
					}
				}, 120).Should(Succeed())
			} else {
				Consistently(func(g Gomega) {
					out, _, err := run.Unchecked(fmt.Sprintf("%s -s Running", defaultCommand))
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(out).ToNot(BeEmpty())
					if out != "" {
						GinkgoWriter.Printf("Container %s (%s) is still running\n",
							isolatedPod, strings.TrimSpace(out))
					}
				}, 20, 5).Should(Succeed())
			}
		})
	}

	assertLivenessPinger := func(clusterManifest string, livenessPingerEnabled bool) {
		var namespace, clusterName, isolatedNode string
		var err error
		var oldPrimaryPod *corev1.Pod

		DeferCleanup(func() {
			// Ensure the isolatedNode networking is re-established
			if CurrentSpecReport().Failed() {
				_, _, _ = run.Unchecked(fmt.Sprintf("docker network connect kind %v", isolatedNode))
			}
		})

		By("creating a Cluster", func() {
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, clusterManifest, env)
		})

		By("setting up the environment", func() {
			// Ensure the operator is not running on the same node as the primary.
			// If it is, we switch to a new primary
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			operatorPod, err := operator.GetPod(env.Ctx, env.Client)
			Expect(err).NotTo(HaveOccurred())
			if primaryPod.Spec.NodeName == operatorPod.Spec.NodeName {
				AssertSwitchover(namespace, clusterName, env)
			}
		})

		By("disconnecting the node containing the primary", func() {
			oldPrimaryPod, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			isolatedNode = oldPrimaryPod.Spec.NodeName
			_, _, err = run.Unchecked(fmt.Sprintf("docker network disconnect kind %v", isolatedNode))
			Expect(err).ToNot(HaveOccurred())
		})

		By("verifying that a new primary has been promoted", func() {
			AssertClusterEventuallyReachesPhase(namespace, clusterName,
				[]string{apiv1.PhaseFailOver}, 120)
			Eventually(func(g Gomega) {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cluster.Status.CurrentPrimary).ToNot(BeEquivalentTo(oldPrimaryPod.Name))
			}, testTimeouts[timeouts.NewPrimaryAfterFailover]).Should(Succeed())
		})

		verifyIsolatedPrimary(namespace, oldPrimaryPod.Name, isolatedNode, livenessPingerEnabled)

		By("reconnecting the isolated Node", func() {
			_, _, err = run.Unchecked(fmt.Sprintf("docker network connect kind %v", isolatedNode))
			Expect(err).ToNot(HaveOccurred())

			// Assert that the oldPrimary comes back as a replica
			namespacedName := types.NamespacedName{
				Namespace: oldPrimaryPod.Namespace,
				Name:      oldPrimaryPod.Name,
			}
			timeout := 180
			Eventually(func(g Gomega) {
				pod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, namespacedName, &pod)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(utils.IsPodActive(pod)).To(BeTrue())
				g.Expect(utils.IsPodReady(pod)).To(BeTrue())
				g.Expect(specs.IsPodStandby(pod)).To(BeTrue())
				g.Expect(nodes.IsNodeReachable(env.Ctx, env.Client, isolatedNode)).To(BeTrue())
			}, timeout).Should(Succeed())
		})
	}

	When("livenessPinger is enabled", func() {
		const sampleFile = fixturesDir + "/self-fencing/cluster-liveness-pinger-enabled.yaml.template"
		It("will terminate an isolated primary", func() {
			assertLivenessPinger(sampleFile, true)
		})
	})

	When("livenessPinger is disabled", func() {
		const sampleFile = fixturesDir + "/self-fencing/cluster-liveness-pinger-disabled.yaml.template"
		It("will not restart an isolated primary", func() {
			assertLivenessPinger(sampleFile, false)
		})
	})
})
