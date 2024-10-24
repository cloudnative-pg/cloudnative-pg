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
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

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
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())

			By("setting up cluster and declarative database CRD", func() {
				AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			})
		})

		assertDatabaseHasExpectedFields := func(namespace, primaryPod string, db apiv1.Database) {
			query := fmt.Sprintf("select count(*) from pg_database where datname = '%s' "+
				"and encoding = pg_char_to_encoding('%s') and datctype = '%s' and datcollate = '%s'",
				db.Spec.Name, db.Spec.Encoding, db.Spec.LcCtype, db.Spec.LcCollate)
			Eventually(func(g Gomega) {
				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
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
		) {
			var (
				database           apiv1.Database
				databaseObjectName string
			)
			By("applying Database CRD manifest", func() {
				CreateResourceFromFile(namespace, databaseManifest)
				databaseObjectName, err = yaml.GetResourceNameFromYAML(env.Scheme, databaseManifest)
				Expect(err).NotTo(HaveOccurred())
			})
			By("ensuring the Database CRD succeeded reconciliation", func() {
				// get database object
				database = apiv1.Database{}
				databaseNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      databaseObjectName,
				}

				Eventually(func(g Gomega) {
					err := env.Client.Get(env.Ctx, databaseNamespacedName, &database)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(database.Status.Ready).Should(BeTrue())
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("verifying new database has been created with the expected fields", func() {
				primaryPodInfo, err := clusterutils.GetClusterPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				AssertDatabaseExists(primaryPodInfo, dbname, true)

				assertDatabaseHasExpectedFields(namespace, primaryPodInfo.Name, database)
			})

			By("removing the Database object", func() {
				Expect(objects.DeleteObject(env.Ctx, env.Client, &database)).To(Succeed())
			})

			By("verifying the retention policy in the postgres database", func() {
				primaryPodInfo, err := clusterutils.GetClusterPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				AssertDatabaseExists(primaryPodInfo, dbname, retainOnDeletion)
			})
		}

		When("Database CRD reclaim policy is set to delete", func() {
			It("can manage a declarative database and delete it in Postgres", func() {
				databaseManifest := fixturesDir +
					"/declarative_databases/database-with-delete-reclaim-policy.yaml.template"
				assertTestDeclarativeDatabase(databaseManifest,
					false)
			})
		})

		When("Database CRD reclaim policy is set to retain", func() {
			It("can manage a declarative database and release it", func() {
				databaseManifest := fixturesDir + "/declarative_databases/database.yaml.template"
				assertTestDeclarativeDatabase(databaseManifest, true)
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
				err = namespaces.CreateNamespace(env.Ctx, env.Client, namespace)
				Expect(err).ToNot(HaveOccurred())

				clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
				Expect(err).ToNot(HaveOccurred())

				AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			})
			By("creating the database", func() {
				databaseManifest := fixturesDir +
					"/declarative_databases/database-with-delete-reclaim-policy.yaml.template"
				databaseObjectName, err = yaml.GetResourceNameFromYAML(env.Scheme, databaseManifest)
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
				err := namespaces.DeleteNamespaceAndWait(env.Ctx, env.Client, namespace, 60)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})
})
