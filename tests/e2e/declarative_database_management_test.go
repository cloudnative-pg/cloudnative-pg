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
var _ = Describe("Declarative database management", Label(tests.LabelSmoke, tests.LabelBasic,
	tests.LabelDeclarativeDatabases), func() {
	const (
		clusterManifest = fixturesDir + "/declarative_databases/cluster.yaml.template"
		level           = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("in a plain vanilla cluster", Ordered, func() {
		const (
			namespacePrefix = "declarative-db"
			dbname          = "declarative"
		)
		var (
			clusterName, namespace string
			err                    error
		)

		BeforeAll(func() {
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

		assertDatabaseHasExpectedFields := func(namespace, primaryPod string, db apiv1.Database) {
			query := fmt.Sprintf("select count(*) from pg_database where datname = '%s' "+
				"and encoding = %s and datctype = '%s' and datcollate = '%s'",
				db.Spec.Name, db.Spec.Encoding, db.Spec.LcCtype, db.Spec.LcCollate)
			Eventually(func(g Gomega) {
				stdout, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod,
					},
					"postgres",
					query)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(stdout).Should(ContainSubstring("1"), "expected database not found")
			}, 30).Should(Succeed())
		}

		assertTestDeclarativeDatabase := func(
			databaseManifest string,
			retainOnDeletion bool,
			expectedDatabaseFields apiv1.Database,
		) {
			var (
				database           *apiv1.Database
				databaseObjectName string
			)
			By("applying Database CRD manifest", func() {
				CreateResourceFromFile(namespace, databaseManifest)
				databaseObjectName, err = env.GetResourceNameFromYAML(databaseManifest)
				Expect(err).NotTo(HaveOccurred())
			})
			By("ensuring the Database CRD succeeded reconciliation", func() {
				// get database object
				database = &apiv1.Database{}
				databaseNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      databaseObjectName,
				}

				Eventually(func(g Gomega) {
					err := env.Client.Get(env.Ctx, databaseNamespacedName, database)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(database.Status.Ready).Should(BeTrue())
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("verifying new database has been created with the expected fields", func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				assertDatabaseExists(namespace, primaryPodInfo.Name, dbname, true)

				assertDatabaseHasExpectedFields(namespace, primaryPodInfo.Name, expectedDatabaseFields)
			})

			By("removing the Database object", func() {
				Expect(utils.DeleteObject(env, database)).To(Succeed())
			})

			By("verifying the retention policy in the postgres database", func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				assertDatabaseExists(namespace, primaryPodInfo.Name, dbname, retainOnDeletion)
			})
		}

		When("Database CRD reclaim policy is set to retain", func() {
			It("can manage a declarative database and release it", func() {
				databaseManifest := fixturesDir + "/declarative_databases/database.yaml.template"
				shouldPgDatabaseBeRetained := true
				// NOTE: the `pg_database` table in Postgres does not contain fields
				// for the owner nor the template.
				// Its fields are dependent on the version of Postgres, so we pick
				// a subset that is available to check even on PG v12
				expectedDatabaseFields := apiv1.Database{
					Spec: apiv1.DatabaseSpec{
						Name:      "declarative",
						LcCtype:   "en_US.utf8",
						LcCollate: "C", // this is the default value
						Encoding:  "0", // corresponds to SQL_ASCII
					},
				}

				assertTestDeclarativeDatabase(databaseManifest,
					shouldPgDatabaseBeRetained, expectedDatabaseFields)
			})
		})

		When("Database CRD reclaim policy is set to delete", func() {
			It("can manage a declarative database and delete it in Postgres", func() {
				// NOTE: the Postgres database 'declarative' created in the previous spec
				// was retained after deletion.
				// This manifest adopts the existing database and only changes the retention policy
				databaseManifest := fixturesDir +
					"/declarative_databases/database-with-delete-reclaim-policy.yaml.template"
				shouldPgDatabaseBeRetained := false
				expectedDatabaseFields := apiv1.Database{
					Spec: apiv1.DatabaseSpec{
						Name:      "declarative",
						LcCtype:   "en_US.utf8",
						LcCollate: "C", // this is the default value
						Encoding:  "0", // corresponds to SQL_ASCII
					},
				}

				assertTestDeclarativeDatabase(databaseManifest,
					shouldPgDatabaseBeRetained, expectedDatabaseFields)
			})
		})
	})

	Context("in a Namespace to be deleted manually", func() {
		const (
			namespace = "declarative-db-finalizers"
		)
		var (
			err                error
			clusterName        string
			databaseObjectName string
		)
		It("will not prevent the deletion of the namespace with lagging finalizers", func() {
			By("setting up the new namespace and cluster", func() {
				err = env.CreateNamespace(namespace)
				Expect(err).ToNot(HaveOccurred())

				clusterName, err = env.GetResourceNameFromYAML(clusterManifest)
				Expect(err).ToNot(HaveOccurred())

				AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			})
			By("creating the database", func() {
				databaseManifest := fixturesDir +
					"/declarative_databases/database-with-delete-reclaim-policy.yaml.template"
				databaseObjectName, err = env.GetResourceNameFromYAML(databaseManifest)
				Expect(err).NotTo(HaveOccurred())
				CreateResourceFromFile(namespace, databaseManifest)
			})
			By("ensuring the database is reconciled successfully", func() {
				// get database object
				dbObj := &apiv1.Database{}
				databaseNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      databaseObjectName,
				}
				Eventually(func(g Gomega) {
					err := env.Client.Get(env.Ctx, databaseNamespacedName, dbObj)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(dbObj.Status.Ready).Should(BeTrue())
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})
			By("deleting the namespace and making sure it succeeds before timeout", func() {
				err := env.DeleteNamespaceAndWait(namespace, 30)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})
})
