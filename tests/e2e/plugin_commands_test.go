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
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/promote"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("plugin commands tests", Label(tests.LabelSmoke), func() {
	const (
		level           = tests.Medium
		namespacePrefix = "plugin-commands"
	)
	var clusterName, namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	clusterSetup := func(namespace, clusterManifest string) {
		var err error

		clusterName, err = env.GetResourceNameFromYAML(clusterManifest)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster and having it be ready", func() {
			AssertCreateCluster(namespace, clusterName, clusterManifest, env)
		})
	}

	Context("on a plain new cluster", Ordered, func() {
		var err error
		const (
			clusterManifest = fixturesDir +
				"/base/cluster-storage-class.yaml.template"
		)
		BeforeAll(func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterSetup(namespace, clusterManifest)
		})

		It("can promote an instance via plugin", func(ctx SpecContext) {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).NotTo(HaveOccurred())

			initialPrimary := cluster.Status.CurrentPrimary
			GinkgoWriter.Println("initial primary", initialPrimary)
			firstIsPrimary := strings.HasSuffix(initialPrimary, "-1")
			if firstIsPrimary {
				err = promote.Promote(ctx, env.Client, namespace, clusterName, clusterName+"-2")
			} else {
				err = promote.Promote(ctx, env.Client, namespace, clusterName, clusterName+"-1")
			}
			Expect(err).ShouldNot(HaveOccurred())
			updatedCluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedCluster.Status.TargetPrimary).NotTo(BeEquivalentTo(initialPrimary))
			GinkgoWriter.Println("target primary", updatedCluster.Status.TargetPrimary)
			AssertClusterIsReady(namespace, clusterName, testTimeouts[testUtils.ClusterIsReadyQuick], env)
		})
	})
})
