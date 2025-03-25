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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod patch", Label(tests.LabelSmoke, tests.LabelBasic), func() {
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

	It("use the podPatch annotation to generate Pods", func(_ SpecContext) {
		const namespacePrefix = "cluster-patch-e2e"
		var err error

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("adding the podPatch annotation", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			patchedCluster := cluster.DeepCopy()

			patchedCluster.SetAnnotations(map[string]string{
				utils.PodPatchAnnotationName: `
					[
						{
							"op": "add",
							"path": "/metadata/annotations/e2e.cnpg.io",
							"value": "this-test"
						}
					]
				`,
			})
			err = env.Client.Patch(env.Ctx, patchedCluster, client.MergeFrom(cluster))
			Expect(err).ToNot(HaveOccurred())
		})

		By("deleting all the Pods", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			for i := range podList.Items {
				err := env.Client.Delete(env.Ctx, &podList.Items[i])
				Expect(err).ToNot(HaveOccurred())
			}
		})

		By("waiting for the new annotation to be applied to the new Pods", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			timeout := 120
			Eventually(func(g Gomega) {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(podList.Items).To(HaveLen(cluster.Spec.Instances))

				for _, pod := range podList.Items {
					g.Expect(pod.Annotations).To(HaveKeyWithValue("e2e.cnpg.io", "this-test"))
				}
			}, timeout).Should(Succeed())
		})
	})
})
