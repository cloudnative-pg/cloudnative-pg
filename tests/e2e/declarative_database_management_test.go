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
	"time"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// - spinning up a cluster, apply a declarative database on it

// Set of tests in which we use the declarative database CRD to add new databases on an existing cluster
var _ = Describe("Declarative databases management test", Label(tests.LabelSmoke, tests.LabelBasic), func() {
	const (
		clusterManifest  = fixturesDir + "/declarative_databases/cluster.yaml.template"
		databaseManifest = fixturesDir + "/declarative_databases/database.yaml.template"
		level            = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("plain vanilla cluster", Ordered, func() {
		const (
			namespacePrefix = "declarative-db"
			databaseCrdName = "db-declarative"
			dbname          = "declarative"
		)
		var (
			clusterName, namespace string
			database               *apiv1.Database
		)

		BeforeAll(func() {
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err = env.GetResourceNameFromYAML(clusterManifest)
			Expect(err).ToNot(HaveOccurred())

			By("setting up cluster and declarative database CRD", func() {
				AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			})
		})

		assertDatabaseExists := func(namespace, primaryPod, dbname string, shouldContain bool) {
			Eventually(func(g Gomega) {
				stdout, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod,
					},
					"postgres",
					"\\l")
				g.Expect(err).ToNot(HaveOccurred())
				if shouldContain {
					g.Expect(stdout).Should(ContainSubstring(dbname))
				} else {
					g.Expect(stdout).ShouldNot(ContainSubstring(dbname))
				}
			}, 300).Should(Succeed())
		}

		When("Database CRD reclaim policy is set to retain (default) inside spec", func() {
			It("can add a declarative database", func() {
				By("applying Database CRD manifest", func() {
					CreateResourceFromFile(namespace, databaseManifest)
					_, err := env.GetResourceNameFromYAML(databaseManifest)
					Expect(err).NotTo(HaveOccurred())
				})
				By("ensuring the Database CRD succeeded reconciliation", func() {
					// get database object
					database = &apiv1.Database{}
					databaseNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      databaseCrdName,
					}

					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, databaseNamespacedName, database)
						Expect(err).ToNot(HaveOccurred())
						g.Expect(database.Status.Ready).Should(BeTrue())
					}, 300).WithPolling(10 * time.Second).Should(Succeed())
				})

				By("verifying new database has been added", func() {
					primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					assertDatabaseExists(namespace, primaryPodInfo.Name, dbname, true)
				})
			})

			It("keeps the db when Database CRD is removed", func() {
				By("remove Database CRD", func() {
					Expect(utils.DeleteObject(env, database)).To(Succeed())
				})

				By("verifying database is still existing", func() {
					primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					assertDatabaseExists(namespace, primaryPodInfo.Name, dbname, true)
				})
			})
		})
	})
})
