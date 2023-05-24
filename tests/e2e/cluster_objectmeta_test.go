/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster object metadata", Label(tests.LabelClusterMetadata), func() {
	const (
		level                 = tests.Low
		clusterWithObjectMeta = fixturesDir + "/cluster_objectmeta/cluster-level-objectMeta.yaml.template"
		namespacePrefix       = "objectmeta-inheritance"
	)
	var namespace string
	var err error
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("verify label's and annotation's inheritance when per-cluster objectmeta changed ", func() {
		clusterName := "objectmeta-inheritance"
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})
		AssertCreateCluster(namespace, clusterName, clusterWithObjectMeta, env)

		By("checking the pods have the expected labels", func() {
			expectedLabels := map[string]string{
				"environment":      "qaEnv",
				"example.com/qa":   "qa",
				"example.com/prod": "prod",
			}
			Eventually(func() (bool, error) {
				return utils.AllClusterPodsHaveLabels(env, namespace, clusterName, expectedLabels)
			}, 180).Should(BeTrue())
		})
		By("checking the pods have the expected annotations", func() {
			expectedAnnotations := map[string]string{
				"categories":       "DatabaseApplication",
				"example.com/qa":   "qa",
				"example.com/prod": "prod",
			}
			Eventually(func() (bool, error) {
				return utils.AllClusterPodsHaveAnnotations(env, namespace, clusterName, expectedAnnotations)
			}, 180).Should(BeTrue())
		})
	})
})
