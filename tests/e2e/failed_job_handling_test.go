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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Failed job handling", Serial, Label(tests.LabelReplication), func() {
	const (
		sampleFile  = fixturesDir + "/base/cluster-storage-class.yaml.template"
		clusterName = "postgresql-storage-class"
		level       = tests.High
	)

	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// This test verifies that a failed join job does not block the reconciliation loop.
	// When a join job fails, the operator should:
	// 1. Exclude the failed job from runningJobNames() count
	// 2. Continue processing other scaling operations
	// 3. Eventually reach the desired cluster state
	It("continues scaling after a join job fails", func() {
		const namespacePrefix = "failed-job-handling"
		var err error

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster with 3 instances", func() {
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
		})

		By("scaling to 4 instances", func() {
			_, _, err := run.Run(fmt.Sprintf("kubectl scale --replicas=4 -n %v cluster/%v", namespace, clusterName))
			Expect(err).ToNot(HaveOccurred())
		})

		var joinJob *batchv1.Job
		By("waiting for a join job to be created", func() {
			Eventually(func(g Gomega) {
				var jobs batchv1.JobList
				err := env.Client.List(env.Ctx, &jobs,
					k8client.InNamespace(namespace),
					k8client.MatchingLabels{
						utils.ClusterLabelName: clusterName,
						utils.JobRoleLabelName: "join",
					},
				)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(jobs.Items).ToNot(BeEmpty())
				joinJob = &jobs.Items[0]
			}, 60, 2).Should(Succeed())
		})

		By("attempting to disrupt the join job by deleting its pods", func() {
			// Try to cause job disruption by deleting pods
			// Note: The job might succeed anyway if Azure disk attach is fast enough
			for i := 0; i < 3; i++ {
				var pods corev1.PodList
				err := env.Client.List(env.Ctx, &pods,
					k8client.InNamespace(namespace),
					k8client.MatchingLabels{
						"job-name": joinJob.Name,
					},
				)
				if err != nil || len(pods.Items) == 0 {
					time.Sleep(3 * time.Second)
					continue
				}

				for _, pod := range pods.Items {
					_ = env.Client.Delete(env.Ctx, &pod,
						k8client.GracePeriodSeconds(0),
					)
				}
				time.Sleep(3 * time.Second)
			}
		})

		// The key test: whether the job fails or succeeds, the cluster should
		// eventually reach a stable state and not get stuck in "Creating replica"
		By("verifying the cluster eventually reaches healthy state", func() {
			// The reconciler should handle both success and failure scenarios:
			// - If job succeeded: cluster becomes healthy
			// - If job failed: reconciler continues (doesn't get stuck)
			// Either way, scaling down to 3 should result in a healthy cluster
			AssertClusterIsReady(namespace, clusterName, 600, env)
		})

		By("verifying we can scale back to 3 instances", func() {
			_, _, err := run.Run(fmt.Sprintf("kubectl scale --replicas=3 -n %v cluster/%v", namespace, clusterName))
			Expect(err).ToNot(HaveOccurred())
			AssertClusterIsReady(namespace, clusterName, 300, env)
		})
	})

	// This test verifies that the safety net clears stuck scaling phases
	// when the number of running instances matches the desired count
	It("clears stuck phase when instances match desired count", func() {
		const namespacePrefix = "stuck-phase-recovery"
		var err error

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster with 3 instances", func() {
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
		})

		By("verifying cluster is healthy", func() {
			AssertClusterIsReady(namespace, clusterName, 300, env)
		})

		By("manually setting phase to 'Creating replica' to simulate stuck state", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			// Patch the status to simulate a stuck phase
			cluster.Status.Phase = apiv1.PhaseCreatingReplica
			cluster.Status.PhaseReason = "Simulated stuck phase for testing"
			err = env.Client.Status().Update(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("verifying the safety net clears the stuck phase", func() {
			// The reconciler should detect that instances == desired and clear the phase
			Eventually(func(g Gomega) {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseHealthy),
					"safety net should clear stuck phase when instances match desired")
			}, 120, 5).Should(Succeed())
		})
	})
})
