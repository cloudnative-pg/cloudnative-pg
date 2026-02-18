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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"

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
	// It creates a synthetic failed job with the correct CNPG labels and sets the cluster
	// phase to "Creating replica", then verifies the reconciler:
	// 1. Excludes the failed job from runningJobNames()
	// 2. Clears the stuck phase via the safety net
	// 3. Returns the cluster to healthy state
	It("does not get stuck when a failed job is present", func() {
		const namespacePrefix = "failed-job-handling"
		var err error

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster with 3 instances", func() {
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
		})

		By("verifying cluster is healthy", func() {
			AssertClusterIsReady(namespace, clusterName, 300, env)
		})

		By("creating a synthetic failed join job owned by the cluster", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			failedJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-join-failed",
					Namespace: namespace,
					Labels: map[string]string{
						utils.ClusterLabelName: clusterName,
						utils.JobRoleLabelName: "join",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: apiv1.SchemeGroupVersion.String(),
							Kind:       apiv1.ClusterKind,
							Name:       cluster.Name,
							UID:        cluster.UID,
							Controller: ptr.To(true),
						},
					},
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers: []corev1.Container{
								{
									Name:    "fake",
									Image:   "scratch",
									Command: []string{"/bin/false"},
								},
							},
						},
					},
				},
			}
			err = env.Client.Create(env.Ctx, failedJob)
			Expect(err).ToNot(HaveOccurred())

			// Set the job status to Failed
			failedJob.Status.Conditions = []batchv1.JobCondition{
				{
					Type:               batchv1.JobFailed,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "BackoffLimitExceeded",
					Message:            "Synthetic failed job for E2E test",
				},
			}
			err = env.Client.Status().Update(env.Ctx, failedJob)
			Expect(err).ToNot(HaveOccurred())
		})

		By("setting the cluster phase to 'Creating replica' to simulate stuck state", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			cluster.Status.Phase = apiv1.PhaseCreatingReplica
			cluster.Status.PhaseReason = "Simulated stuck phase with failed job"
			err = env.Client.Status().Update(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("verifying the cluster returns to healthy state", func() {
			// If runningJobNames() did NOT exclude failed jobs, the reconciler would
			// see len(runningJobs) > 0 and spin forever. This proves it excludes them.
			Eventually(func(g Gomega) {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseHealthy),
					"reconciler should not get stuck when a failed job is present")
			}, 120, 5).Should(Succeed())
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
