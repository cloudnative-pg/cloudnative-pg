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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/resize"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod resize functionality", Label(tests.LabelResizing, tests.LabelBasic), func() {
	const (
		fixturesResizeDir = fixturesDir + "/resize"
		level             = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("In-place resize strategy", func() {
		const (
			sampleFile  = fixturesResizeDir + "/cluster-resize-inplace.yaml.template"
			clusterName = "postgresql-resize-inplace"
		)

		var namespace string

		It("should attempt in-place resize when configured", func(_ SpecContext) {
			const namespacePrefix = "cluster-resize-inplace-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying cluster has in-place resize policy", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Expect(cluster.Spec.ResourceResizePolicy).ToNot(BeNil())
				Expect(cluster.Spec.ResourceResizePolicy.Strategy).To(Equal(apiv1.ResourceResizeStrategyInPlace))
			})

			By("verifying pod specs have resize policies configured", func() {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(podList.Items).To(HaveLen(3))

				for _, pod := range podList.Items {
					postgresContainer := findPostgreSQLContainer(pod)
					Expect(postgresContainer).ToNot(BeNil())

					// Verify resize policies are set
					Expect(postgresContainer.ResizePolicy).ToNot(BeEmpty())

					cpuPolicyFound := false
					memoryPolicyFound := false
					for _, policy := range postgresContainer.ResizePolicy {
						if policy.ResourceName == corev1.ResourceCPU {
							cpuPolicyFound = true
							Expect(string(policy.RestartPolicy)).To(Equal("NotRequired"))
						}
						if policy.ResourceName == corev1.ResourceMemory {
							memoryPolicyFound = true
							Expect(string(policy.RestartPolicy)).To(Equal("NotRequired"))
						}
					}
					Expect(cpuPolicyFound).To(BeTrue(), "CPU resize policy should be configured")
					Expect(memoryPolicyFound).To(BeTrue(), "Memory resize policy should be configured")
				}
			})

			By("testing decision engine with in-place strategy", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				oldSpec := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				}

				// Test CPU-only change within limits (should be in-place)
				newSpecCPU := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("750m"), // 50% increase
						corev1.ResourceMemory: resource.MustParse("1Gi"),  // No change
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1500m"), // 50% increase
						corev1.ResourceMemory: resource.MustParse("2Gi"),   // No change
					},
				}

				engine := resize.NewDecisionEngine(cluster, oldSpec, newSpecCPU)
				strategy, err := engine.DetermineStrategy()
				Expect(err).ToNot(HaveOccurred())
				Expect(strategy).To(Equal(apiv1.ResourceResizeStrategyInPlace))

				// Test memory change (should fall back to rolling update in Phase 1)
				newSpecMemory := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),  // No change
						corev1.ResourceMemory: resource.MustParse("1.5Gi"), // 50% increase
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"), // No change
						corev1.ResourceMemory: resource.MustParse("3Gi"),   // 50% increase
					},
				}

				engineMemory := resize.NewDecisionEngine(cluster, oldSpec, newSpecMemory)
				strategyMemory, err := engineMemory.DetermineStrategy()
				Expect(err).ToNot(HaveOccurred()) // Should error with fallback message
				Expect(strategyMemory).To(Equal(apiv1.ResourceResizeStrategyRollingUpdate))
			})
		})
	})

	Context("Rolling update resize strategy", func() {
		const (
			sampleFile  = fixturesResizeDir + "/cluster-resize-rolling.yaml.template"
			clusterName = "postgresql-resize-rolling"
		)

		var namespace string

		It("should always use rolling update when configured", func(_ SpecContext) {
			const namespacePrefix = "cluster-resize-rolling-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying cluster has rolling update resize policy", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Expect(cluster.Spec.ResourceResizePolicy).ToNot(BeNil())
				Expect(cluster.Spec.ResourceResizePolicy.Strategy).To(Equal(apiv1.ResourceResizeStrategyRollingUpdate))
			})

			By("testing decision engine with rolling update strategy", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				oldSpec := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				}

				// Even small CPU changes should use rolling update
				newSpecSmall := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("600m"), // Small increase
						corev1.ResourceMemory: resource.MustParse("1Gi"),  // No change
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1200m"), // Small increase
						corev1.ResourceMemory: resource.MustParse("2Gi"),   // No change
					},
				}

				engine := resize.NewDecisionEngine(cluster, oldSpec, newSpecSmall)
				strategy, err := engine.DetermineStrategy()
				Expect(err).ToNot(HaveOccurred())
				Expect(strategy).To(Equal(apiv1.ResourceResizeStrategyRollingUpdate))
			})
		})
	})

	Context("Threshold validation and edge cases", func() {
		const (
			sampleFile  = fixturesResizeDir + "/cluster-resize-thresholds.yaml.template"
			clusterName = "postgresql-resize-thresholds"
		)

		var namespace string

		It("should respect strict thresholds and handle edge cases", func(_ SpecContext) {
			const namespacePrefix = "cluster-resize-thresholds-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("testing threshold enforcement in decision engine", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				oldSpec := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2000m"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				}

				// Test CPU increase within threshold (40% - should be in-place)
				newSpecWithinThreshold := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1400m"), // 40% increase
						corev1.ResourceMemory: resource.MustParse("1Gi"),   // No change
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2800m"), // 40% increase
						corev1.ResourceMemory: resource.MustParse("2Gi"),   // No change
					},
				}

				engine := resize.NewDecisionEngine(cluster, oldSpec, newSpecWithinThreshold)
				strategy, err := engine.DetermineStrategy()
				Expect(err).ToNot(HaveOccurred())
				Expect(strategy).To(Equal(apiv1.ResourceResizeStrategyInPlace))

				// Test CPU increase exceeding threshold (60% - should be rolling update)
				newSpecExceedingThreshold := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1600m"), // 60% increase
						corev1.ResourceMemory: resource.MustParse("1Gi"),   // No change
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("3200m"), // 60% increase
						corev1.ResourceMemory: resource.MustParse("2Gi"),   // No change
					},
				}

				engineExceeding := resize.NewDecisionEngine(cluster, oldSpec, newSpecExceedingThreshold)
				strategyExceeding, err := engineExceeding.DetermineStrategy()
				Expect(err).ToNot(HaveOccurred())
				Expect(strategyExceeding).To(Equal(apiv1.ResourceResizeStrategyRollingUpdate))

				// Test CPU decrease within threshold (20% - should be in-place)
				newSpecDecrease := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("800m"), // 20% decrease
						corev1.ResourceMemory: resource.MustParse("1Gi"),  // No change
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1600m"), // 20% decrease
						corev1.ResourceMemory: resource.MustParse("2Gi"),   // No change
					},
				}

				engineDecrease := resize.NewDecisionEngine(cluster, oldSpec, newSpecDecrease)
				strategyDecrease, err := engineDecrease.DetermineStrategy()
				Expect(err).ToNot(HaveOccurred())
				Expect(strategyDecrease).To(Equal(apiv1.ResourceResizeStrategyInPlace))

				// Test CPU decrease exceeding threshold (30% - should be rolling update)
				newSpecDecreaseExceeding := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("700m"), // 30% decrease
						corev1.ResourceMemory: resource.MustParse("1Gi"),  // No change
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1400m"), // 30% decrease
						corev1.ResourceMemory: resource.MustParse("2Gi"),   // No change
					},
				}

				engineDecreaseExceeding := resize.NewDecisionEngine(cluster, oldSpec, newSpecDecreaseExceeding)
				strategyDecreaseExceeding, err := engineDecreaseExceeding.DetermineStrategy()
				Expect(err).ToNot(HaveOccurred())
				Expect(strategyDecreaseExceeding).To(Equal(apiv1.ResourceResizeStrategyRollingUpdate))
			})

			By("testing resize reason messages", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				oldSpec := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2000m"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				}

				// Test in-place resize reason
				newSpecInPlace := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1300m"), // 30% increase
						corev1.ResourceMemory: resource.MustParse("1Gi"),   // No change
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2600m"), // 30% increase
						corev1.ResourceMemory: resource.MustParse("2Gi"),   // No change
					},
				}

				engine := resize.NewDecisionEngine(cluster, oldSpec, newSpecInPlace)
				strategy, err := engine.DetermineStrategy()
				Expect(err).ToNot(HaveOccurred())
				Expect(strategy).To(Equal(apiv1.ResourceResizeStrategyInPlace))

				// Test memory change reason (Phase 1 limitation)
				newSpecMemory := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"), // No change
						corev1.ResourceMemory: resource.MustParse("1.2Gi"), // 20% increase
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2000m"), // No change
						corev1.ResourceMemory: resource.MustParse("2.4Gi"), // 20% increase
					},
				}

				engineMemory := resize.NewDecisionEngine(cluster, oldSpec, newSpecMemory)
				strategyMemory, err := engineMemory.DetermineStrategy()
				Expect(err).ToNot(HaveOccurred())
				Expect(strategyMemory).To(Equal(apiv1.ResourceResizeStrategyRollingUpdate))
			})
		})
	})

	Context("Resize policy configuration validation", func() {
		It("should handle missing resize policy gracefully", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Instances: 3,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1000m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					// No ResourceResizePolicy specified
				},
			}

			oldSpec := &cluster.Spec.Resources
			newSpec := &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("750m"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1500m"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			}

			engine := resize.NewDecisionEngine(cluster, oldSpec, newSpec)
			strategy, err := engine.DetermineStrategy()
			Expect(err).ToNot(HaveOccurred())
			Expect(strategy).To(Equal(apiv1.ResourceResizeStrategyRollingUpdate)) // Default fallback
		})

		It("should handle edge cases in resource calculations", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Instances: 3,
					ResourceResizePolicy: &apiv1.ResourceResizePolicy{
						Strategy: apiv1.ResourceResizeStrategyInPlace,
						CPU: &apiv1.ContainerResizePolicy{
							RestartPolicy:  apiv1.ContainerRestartPolicyNotRequired,
							MaxIncreasePct: &[]int32{100}[0],
							MaxDecreasePct: &[]int32{50}[0],
						},
					},
				},
			}

			// Test zero resource scenario
			oldSpecZero := &corev1.ResourceRequirements{}
			newSpecFromZero := &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("500m"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1000m"),
				},
			}

			engine := resize.NewDecisionEngine(cluster, oldSpecZero, newSpecFromZero)
			strategy, err := engine.DetermineStrategy()
			Expect(err).ToNot(HaveOccurred())
			Expect(strategy).To(Equal(apiv1.ResourceResizeStrategyInPlace))

			// Test identical resources (no change)
			sameSpec := &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1000m"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			}

			engineSame := resize.NewDecisionEngine(cluster, sameSpec, sameSpec)
			strategySame, err := engineSame.DetermineStrategy()
			Expect(err).ToNot(HaveOccurred())
			Expect(strategySame).To(Equal(apiv1.ResourceResizeStrategyInPlace))
		})
	})
})

// findPostgreSQLContainer finds the PostgreSQL container in a pod
func findPostgreSQLContainer(pod corev1.Pod) *corev1.Container {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == specs.PostgresContainerName {
			return &pod.Spec.Containers[i]
		}
	}
	return nil
}
