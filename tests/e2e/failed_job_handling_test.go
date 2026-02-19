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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Failed job handling", Serial, Label(tests.LabelSelfHealing), func() {
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

	// This test verifies the full flow for failed snapshot-recovery jobs:
	// 1. The reconciler detects the failed job
	// 2. It reads the VolumeSnapshot name from the PGDATA PVC's dataSource
	// 3. It records the snapshot name in cluster.Status.ExcludedSnapshots
	// 4. The failed job is retained (TTL controller handles cleanup)
	// 5. The cluster stays healthy (failed jobs don't block the reconciler)
	It("excludes the snapshot from future use when a snapshot-recovery job fails", func() {
		const namespacePrefix = "failed-job-handling"
		const snapshotName = "test-snapshot-pgdata"
		var err error

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster with 3 instances", func() {
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
		})

		By("verifying cluster is healthy", func() {
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
		})

		// Pick a replica instance name for the synthetic job and PVC
		var instanceName string
		var cluster *apiv1.Cluster

		By("choosing a replica instance to simulate the failed job for", func() {
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			replicas, err := clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(replicas.Items).ToNot(BeEmpty(), "cluster should have at least one replica")
			instanceName = replicas.Items[0].Name
		})

		By("creating a PGDATA PVC with a VolumeSnapshot dataSource for the instance", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      instanceName + "-snap-pgdata",
					Namespace: namespace,
					Labels: map[string]string{
						utils.ClusterLabelName:      clusterName,
						utils.InstanceNameLabelName: instanceName,
						utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
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
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
					DataSource: &corev1.TypedLocalObjectReference{
						APIGroup: ptr.To("snapshot.storage.k8s.io"),
						Kind:     "VolumeSnapshot",
						Name:     snapshotName,
					},
				},
			}
			err = env.Client.Create(env.Ctx, pvc)
			Expect(err).ToNot(HaveOccurred())
		})

		By("creating a synthetic failed snapshot-recovery job owned by the cluster", func() {
			failedJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-snap-recovery-failed",
					Namespace: namespace,
					Labels: map[string]string{
						utils.ClusterLabelName:      clusterName,
						utils.JobRoleLabelName:      "snapshot-recovery",
						utils.InstanceNameLabelName: instanceName,
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
					// Create suspended so the job controller never starts pods.
					// This keeps active=0, which is required for K8s 1.35+
					// validation of finished jobs.
					Suspend: ptr.To(true),
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

			// Set the job status to Failed with a retry loop to handle
			// conflicts from the reconciler updating the job concurrently.
			// K8s 1.35+ validates:
			// - FailureTarget condition must appear before Failed
			// - active == 0 for finished jobs (guaranteed by Suspend: true)
			// - startTime must be set for finished jobs
			// - uncountedTerminatedPods must be nil for finished jobs
			Eventually(func(g Gomega) {
				g.Expect(env.Client.Get(env.Ctx, ctrlclient.ObjectKeyFromObject(failedJob), failedJob)).
					To(Succeed())

				now := metav1.Now()
				failedJob.Status.StartTime = &now
				failedJob.Status.Failed = 1
				failedJob.Status.UncountedTerminatedPods = nil
				failedJob.Status.Conditions = []batchv1.JobCondition{
					{
						Type:               batchv1.JobFailureTarget,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: now,
						Reason:             "BackoffLimitExceeded",
						Message:            "Synthetic failed job for E2E test",
					},
					{
						Type:               batchv1.JobFailed,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: now,
						Reason:             "BackoffLimitExceeded",
						Message:            "Synthetic failed job for E2E test",
					},
				}
				g.Expect(env.Client.Status().Update(env.Ctx, failedJob)).To(Succeed())
			}, 30, 1).Should(Succeed())
		})

		By("verifying the failed job is retained (TTL controller handles cleanup)", func() {
			var job batchv1.Job
			err = env.Client.Get(env.Ctx, ctrlclient.ObjectKey{
				Namespace: namespace,
				Name:      clusterName + "-snap-recovery-failed",
			}, &job)
			Expect(err).ToNot(HaveOccurred(), "failed job should still exist")
		})

		By("verifying cluster.Status.ExcludedSnapshots contains the snapshot name", func() {
			Eventually(func(g Gomega) {
				cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cluster.Status.ExcludedSnapshots).To(ContainElement(snapshotName),
					"the snapshot used by the failed job's PVC should be recorded in ExcludedSnapshots")
			}, 120, 5).Should(Succeed())
		})

		By("verifying the cluster remains healthy", func() {
			Eventually(func(g Gomega) {
				cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseHealthy),
					"cluster should stay healthy since failed jobs don't block the reconciler")
			}, 120, 5).Should(Succeed())
		})
	})
})
