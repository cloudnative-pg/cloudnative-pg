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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Job patch", Label(tests.LabelSmoke, tests.LabelBasic), func() {
	const (
		sampleFile  = fixturesDir + "/base/cluster-storage-class.yaml.template"
		clusterName = "postgresql-storage-class"
		level       = tests.Lowest
	)

	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("uses the initdbJobPatch annotation to customize initdb jobs", func(_ SpecContext) {
		const namespacePrefix = "job-patch-e2e"
		var err error

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("adding the initdbJobPatch annotation", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			patchedCluster := cluster.DeepCopy()

			patchedCluster.SetAnnotations(map[string]string{
				utils.InitDBJobPatchAnnotationName: `[
					{
						"op": "add",
						"path": "/spec/template/spec/terminationGracePeriodSeconds",
						"value": 60
					}
				]`,
			})
			err = env.Client.Patch(env.Ctx, patchedCluster, client.MergeFrom(cluster))
			Expect(err).ToNot(HaveOccurred())
		})

		By("deleting existing Jobs to trigger recreation", func() {
			jobList := &batchv1.JobList{}
			err = env.Client.List(env.Ctx, jobList, client.InNamespace(namespace))
			Expect(err).ToNot(HaveOccurred())

			for _, job := range jobList.Items {
				err := env.Client.Delete(env.Ctx, &job)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		By("waiting for new initdb Job to have the patched configuration", func() {
			timeout := 120
			Eventually(func(g Gomega) {
				jobList := &batchv1.JobList{}
				err = env.Client.List(env.Ctx, jobList, client.InNamespace(namespace))
				g.Expect(err).ToNot(HaveOccurred())

				// Find an initdb job
				var initdbJob *batchv1.Job
				for i := range jobList.Items {
					if jobList.Items[i].Labels[utils.JobRoleLabelName] == "initdb" {
						initdbJob = &jobList.Items[i]
						break
					}
				}
				g.Expect(initdbJob).ToNot(BeNil(), "initdb job should exist")

				// Verify the patch was applied
				g.Expect(initdbJob.Spec.Template.Spec.TerminationGracePeriodSeconds).ToNot(BeNil())
				g.Expect(*initdbJob.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(60)))
			}, timeout).Should(Succeed())
		})
	})

	It("isolates patches between different job types", func(_ SpecContext) {
		const namespacePrefix = "job-patch-isolation"
		var err error

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		// Create initial cluster
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("annotating with initdb patch but expecting join job unaffected", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			// Scale up to trigger join job creation
			cluster.Spec.Instances = 3
			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Wait for the cluster to scale up
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
		})

		By("adding different patches for different job types", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			patchedCluster := cluster.DeepCopy()
			patchedCluster.SetAnnotations(map[string]string{
				utils.InitDBJobPatchAnnotationName: `[
					{
						"op": "add",
						"path": "/metadata/annotations/cnpg.io/test-initdb",
						"value": "initdb-patch-applied"
					}
				]`,
				utils.JoinJobPatchAnnotationName: `[
					{
						"op": "add",
						"path": "/metadata/annotations/cnpg.io/test-join",
						"value": "join-patch-applied"
					}
				]`,
			})
			err = env.Client.Patch(env.Ctx, patchedCluster, client.MergeFrom(cluster))
			Expect(err).ToNot(HaveOccurred())
		})

		By("triggering new jobs by scaling down and up", func() {
			// Delete the join job by removing a replica
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			cluster.Spec.Instances = 1
			err = env.Client.Update(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Wait for single instance
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
		})

		By("verifying initdb patch is isolated from join patch", func() {
			timeout := 120
			Eventually(func(g Gomega) {
				jobList := &batchv1.JobList{}
				err = env.Client.List(env.Ctx, jobList, client.InNamespace(namespace))
				g.Expect(err).ToNot(HaveOccurred())

				// Verify initdb job doesn't have join annotation
				for _, job := range jobList.Items {
					if job.Labels[utils.JobRoleLabelName] == "initdb" {
						g.Expect(job.Annotations).To(HaveKeyWithValue("cnpg.io/test-initdb", "initdb-patch-applied"))
						g.Expect(job.Annotations).ToNot(HaveKey("cnpg.io/test-join"))
					}
					if job.Labels[utils.JobRoleLabelName] == "join" {
						g.Expect(job.Annotations).To(HaveKeyWithValue("cnpg.io/test-join", "join-patch-applied"))
						g.Expect(job.Annotations).ToNot(HaveKey("cnpg.io/test-initdb"))
					}
				}
			}, timeout).Should(Succeed())
		})
	})
})
