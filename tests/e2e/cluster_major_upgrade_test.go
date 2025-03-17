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
	"os"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	"github.com/cloudnative-pg/cloudnative-pg/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Postgres Major Upgrade", Label(tests.LabelPostgresMajorUpgrade), func() {
	const (
		level = tests.Medium
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// TODO: remove me once the test is complete
	BeforeAll(func() {
		Skip("wip")
	})

	Context("in-place upgrade", func() {
		const (
			namespacePrefix = "cluster-major-upgrade"
			clusterManifest = fixturesDir + "/pg_major_upgrade/cluster-major-upgrade.yaml.template"
		)

		It("can upgrade a Cluster to a newer major version", func() {
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, clusterManifest, env)

			// Gather the current image
			postgresImage := os.Getenv("POSTGRES_IMG")
			Expect(postgresImage).ShouldNot(BeEmpty(), "POSTGRES_IMG env should not be empty")

			// this test case is only applicable if we are not already on the latest major
			if postgres.IsLatestMajor(postgresImage) {
				Skip("Already running on the latest major. This test is not applicable for PostgreSQL " + postgresImage)
			}

			// Gather the target image
			targetImage, err := postgres.BumpPostgresImageMajorVersion(postgresImage)
			Expect(err).ToNot(HaveOccurred())
			Expect(targetImage).ShouldNot(BeEmpty(), "targetImage could not be empty")

			// Upgrade imageName in Cluster

			// Check that
			// 1. All Cluster's pods are gone
			// 2. A new major version upgrade job started and completed
			// 3. The primary is on the new major version
			// 4. Primary has same PVC but different pod
			// 5. Replicas are on different pods/PVCs
			// 6. there are no leftover files/directory from the upgrade

		})

	})
})
