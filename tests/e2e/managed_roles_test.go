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
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// - spinning up a cluster with some post-init-sql query and verifying that they are really executed

// Set of tests in which we check that the initdb options are really applied
var _ = Describe("Managed roles tests", Label(tests.LabelSmoke, tests.LabelBasic), func() {
	const (
		clusterManifest = fixturesDir + "/managed_roles/cluster-managed-roles.yaml"
		level           = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("plain vanilla cluster", func() {
		const (
			clusterName = "cluster-example-with-roles"
		)

		var namespace string

		It("can create roles specified in the managed roles stanza", func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace = "managed-roles"
			username := "edb_admin"
			password := "edb_admin"
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})

			AssertCreateCluster(namespace, clusterName, clusterManifest, env)

			By("ensuring the role created in the managed stanza is in the database", func() {
				primaryDst := clusterName + "-1"
				cmd := `psql -U postgres postgres -tAc '\du'`
				stdout, _, err := utils.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					primaryDst,
					cmd))
				Expect(err).ToNot(HaveOccurred())
				Expect(stdout).To(ContainSubstring(username))
				rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
				// assert connectable use username and password defined in secrets
				AssertConnection(rwService, username, "postgres", password, *psqlClientPod, 10, env)
			})
		})
	})
})
