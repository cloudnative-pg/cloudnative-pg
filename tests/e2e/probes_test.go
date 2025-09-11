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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests in which we check that the configuration of the readiness probes is applied
var _ = Describe("Probes configuration tests", Label(tests.LabelBasic), func() {
	const (
		level = tests.High
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("can change the probes configuration", func(ctx SpecContext) {
		var namespace string

		const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml.template"
		const clusterName = "postgresql-storage-class"

		// IMPORTANT: for this E2e to work, these values need to be different
		// than the default Kubernetes settings
		probeConfiguration := apiv1.Probe{
			InitialDelaySeconds: 2,
			PeriodSeconds:       4,
			TimeoutSeconds:      8,
		}
		probesConfiguration := apiv1.ProbesConfiguration{
			Startup: &apiv1.ProbeWithStrategy{
				Probe: probeConfiguration,
			},
			Liveness: &apiv1.LivenessProbe{
				Probe: probeConfiguration,
			},
			Readiness: &apiv1.ProbeWithStrategy{
				Probe: probeConfiguration,
			},
		}

		assertProbeCoherentWithConfiguration := func(probe *corev1.Probe) {
			Expect(probe.InitialDelaySeconds).To(BeEquivalentTo(probeConfiguration.InitialDelaySeconds))
			Expect(probe.PeriodSeconds).To(BeEquivalentTo(probeConfiguration.PeriodSeconds))
			Expect(probe.TimeoutSeconds).To(BeEquivalentTo(probeConfiguration.TimeoutSeconds))
		}

		assertProbesCoherentWithConfiguration := func(container *corev1.Container) {
			assertProbeCoherentWithConfiguration(container.LivenessProbe)
			assertProbeCoherentWithConfiguration(container.ReadinessProbe)
			assertProbeCoherentWithConfiguration(container.StartupProbe)
		}

		var defaultReadinessProbe *corev1.Probe
		var defaultLivenessProbe *corev1.Probe
		var defaultStartupProbe *corev1.Probe

		By("creating an empty cluster", func() {
			// Create a cluster in a namespace we'll delete after the test
			const namespacePrefix = "probes"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)
		})

		By("getting the default probes configuration", func() {
			var pod corev1.Pod
			err := env.Client.Get(ctx, client.ObjectKey{
				Name:      fmt.Sprintf("%s-1", clusterName),
				Namespace: namespace,
			}, &pod)
			Expect(err).ToNot(HaveOccurred())

			Expect(pod.Spec.Containers[0].Name).To(Equal("postgres"))
			defaultReadinessProbe = pod.Spec.Containers[0].ReadinessProbe.DeepCopy()
			defaultLivenessProbe = pod.Spec.Containers[0].LivenessProbe.DeepCopy()
			defaultStartupProbe = pod.Spec.Containers[0].StartupProbe.DeepCopy()
		})

		By("applying a probe configuration", func() {
			var cluster apiv1.Cluster
			err := env.Client.Get(ctx, client.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			}, &cluster)
			Expect(err).ToNot(HaveOccurred())

			originalCluster := cluster.DeepCopy()
			cluster.Spec.Probes = probesConfiguration.DeepCopy()

			err = env.Client.Patch(ctx, &cluster, client.MergeFrom(originalCluster))
			Expect(err).ToNot(HaveOccurred())
		})

		By("waiting for the cluster to restart", func() {
			AssertClusterEventuallyReachesPhase(namespace, clusterName,
				[]string{apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 120)
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)
		})

		By("checking the applied settings", func() {
			var cluster apiv1.Cluster
			err := env.Client.Get(ctx, client.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			}, &cluster)
			Expect(err).ToNot(HaveOccurred())

			for _, instance := range cluster.Status.InstanceNames {
				var pod corev1.Pod
				err := env.Client.Get(ctx, client.ObjectKey{
					Name:      instance,
					Namespace: namespace,
				}, &pod)
				Expect(err).ToNot(HaveOccurred())

				Expect(pod.Spec.Containers[0].Name).To(Equal("postgres"))
				assertProbesCoherentWithConfiguration(&pod.Spec.Containers[0])
			}
		})

		By("reverting back the changes", func() {
			var cluster apiv1.Cluster
			err := env.Client.Get(ctx, client.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			}, &cluster)
			Expect(err).ToNot(HaveOccurred())

			originalCluster := cluster.DeepCopy()
			cluster.Spec.Probes = &apiv1.ProbesConfiguration{}

			err = env.Client.Patch(ctx, &cluster, client.MergeFrom(originalCluster))
			Expect(err).ToNot(HaveOccurred())
		})

		By("waiting for the cluster to restart", func() {
			AssertClusterEventuallyReachesPhase(namespace, clusterName,
				[]string{apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 120)
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)
		})

		By("checking the applied settings", func() {
			var cluster apiv1.Cluster
			err := env.Client.Get(ctx, client.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			}, &cluster)
			Expect(err).ToNot(HaveOccurred())

			for _, instance := range cluster.Status.InstanceNames {
				var pod corev1.Pod
				err = env.Client.Get(ctx, client.ObjectKey{
					Name:      instance,
					Namespace: namespace,
				}, &pod)
				Expect(err).ToNot(HaveOccurred())

				Expect(pod.Spec.Containers[0].Name).To(Equal("postgres"))
				Expect(pod.Spec.Containers[0].LivenessProbe).To(BeEquivalentTo(defaultLivenessProbe))
				Expect(pod.Spec.Containers[0].ReadinessProbe).To(BeEquivalentTo(defaultReadinessProbe))
				Expect(pod.Spec.Containers[0].StartupProbe).To(BeEquivalentTo(defaultStartupProbe))
			}
		})
	})
})
