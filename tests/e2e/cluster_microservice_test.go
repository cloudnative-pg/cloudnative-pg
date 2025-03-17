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
	"fmt"
	"os"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/importdb"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Tests using a microservice approach to import a database from external cluster
// It covers five scenarios:
// 1. With large object
// 2. Normal use case
// 3. Different database names
// 4. Failure
// 5. Different versions of Postgres
var _ = Describe("Imports with Microservice Approach", Label(tests.LabelImportingDatabases), func() {
	const (
		level            = tests.Medium
		sourceSampleFile = fixturesDir + "/cluster_microservice/cluster-base.yaml.template"
		tableName        = "to_import"
	)

	var namespace, sourceClusterName, importedClusterName string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("can import a database with large objects", func() {
		var err error
		const namespacePrefix = "microservice-large-object"
		sourceClusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sourceSampleFile)
		Expect(err).ToNot(HaveOccurred())

		oid := 16393
		data := "large object test"
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, sourceClusterName, sourceSampleFile, env)
		tableLocator := TableLocator{
			Namespace:    namespace,
			ClusterName:  sourceClusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		AssertCreateTestData(env, tableLocator)
		AssertCreateTestDataLargeObject(namespace, sourceClusterName, oid, data)

		importedClusterName = "cluster-pgdump-large-object"
		cluster := AssertClusterImport(namespace, importedClusterName, sourceClusterName, "app")
		tableLocator = TableLocator{
			Namespace:    namespace,
			ClusterName:  importedClusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		AssertDataExpectedCount(env, tableLocator, 2)
		AssertLargeObjectValue(namespace, importedClusterName, oid, data)
		By("deleting the imported database", func() {
			Expect(objects.Delete(env.Ctx, env.Client, cluster)).To(Succeed())
		})
	})

	It("can import a database", func() {
		var err error
		const namespacePrefix = "microservice"
		sourceClusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sourceSampleFile)
		Expect(err).ToNot(HaveOccurred())

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, sourceClusterName, sourceSampleFile, env)
		assertCreateTableWithDataOnSourceCluster(namespace, tableName, sourceClusterName)

		importedClusterName = "cluster-pgdump"
		AssertClusterImport(namespace, importedClusterName, sourceClusterName, "app")
		tableLocator := TableLocator{
			Namespace:    namespace,
			ClusterName:  importedClusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		AssertDataExpectedCount(env, tableLocator, 2)
		assertTableAndDataOnImportedCluster(namespace, tableName, importedClusterName)
	})

	It("can select one from several databases to import", func() {
		var err error
		const namespacePrefix = "microservice-different-db"
		importedClusterName = "cluster-pgdump-different-db"
		// create namespace
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		assertImportRenamesSelectedDatabase(namespace, sourceSampleFile,
			importedClusterName, tableName, "")
	})

	It("fails importing when db does not exist in source cluster", func() {
		// Test case which will check cluster is not created when we use a
		// nonexistent database in cluster definition while importing
		var err error
		const namespacePrefix = "cnpg-microservice-error"
		sourceClusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sourceSampleFile)
		Expect(err).ToNot(HaveOccurred())
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, sourceClusterName, sourceSampleFile, env)

		importedClusterName = "cluster-pgdump-error"
		importClusterNonexistentDB := fixturesDir + "/cluster_microservice/cluster_microservice.yaml"
		CreateResourceFromFile(namespace, importClusterNonexistentDB)
		By("having a imported Cluster in failed state", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      importedClusterName + "-1-import",
			}
			// Eventually the number of failed job should be greater than 1
			// which will ensure the cluster not getting created
			job := &batchv1.Job{}
			Eventually(func(g Gomega) int32 {
				err := env.Client.Get(env.Ctx, namespacedName, job)
				g.Expect(err).ToNot(HaveOccurred())
				return job.Status.Failed
			}, 100).Should(BeEquivalentTo(1))
		})
	})

	It("can import to a cluster with a different major version", func() {
		const namespacePrefix = "microservice-different-db-version"
		importedClusterName = "cluster-pgdump-different-db-version"

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

		By(fmt.Sprintf("import cluster with different major, target version is %s", targetImage), func() {
			var err error
			// create namespace
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			assertImportRenamesSelectedDatabase(namespace, sourceSampleFile, importedClusterName,
				tableName, targetImage)
		})
	})
})

// assertCreateTableWithDataOnSourceCluster will create on the source Cluster, as postgres superUser:
// 1. a new user `micro`
// 2. a new table with 2 records owned by `micro` in the `app` database
// 3. grant select permission on the table to the `app` user (needed during the import)
func assertCreateTableWithDataOnSourceCluster(
	namespace,
	tableName,
	clusterName string,
) {
	By("create user, insert record in new table, assign new user as owner "+
		"and grant read only to app user", func() {
		pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		query := fmt.Sprintf(
			"DROP USER IF EXISTS micro; "+
				"CREATE USER micro; "+
				"CREATE TABLE IF NOT EXISTS %[1]v AS VALUES (1),(2); "+
				"ALTER TABLE %[1]v OWNER TO micro; "+
				"GRANT SELECT ON %[1]v TO app;",
			tableName)

		_, _, err = exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: pod.Namespace,
				PodName:   pod.Name,
			},
			postgres.AppDBName,
			query)
		Expect(err).ToNot(HaveOccurred())
	})
}

// assertTableAndDataOnImportedCluster verifies the data created in source was imported
func assertTableAndDataOnImportedCluster(
	namespace,
	tableName,
	importedClusterName string,
) {
	By("verifying presence of table and data from source in imported cluster", func() {
		pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, importedClusterName)
		Expect(err).ToNot(HaveOccurred())

		By("Verifying imported table has owner app user", func() {
			queryImported := fmt.Sprintf(
				"select * from pg_catalog.pg_tables where tablename = '%v' and tableowner = '%v'",
				tableName,
				postgres.AppUser,
			)
			out, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: pod.Namespace,
					PodName:   pod.Name,
				},
				postgres.AppDBName,
				queryImported)
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Contains(out, tableName), err).Should(BeTrue())
		})

		By("verifying the user named 'micro' on source is not in imported database", func() {
			Eventually(QueryMatchExpectationPredicate(pod, postgres.PostgresDBName,
				roleExistsQuery("micro"), "f"), 30).Should(Succeed())
		})
	})
}

// assertImportRenamesSelectedDatabase verifies that a single DB from a source cluster
// with several DB's can be imported, and that in the imported cluster, the table is
// called 'app'
func assertImportRenamesSelectedDatabase(
	namespace,
	sampleFile,
	importedClusterName,
	tableName,
	imageName string,
) {
	dbList := []string{"db1", "db2", "db3"}
	dbToImport := dbList[1]
	clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
	Expect(err).ToNot(HaveOccurred())

	AssertCreateCluster(namespace, clusterName, sampleFile, env)
	primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())

	By("creating multiple dbs on source and set ownership to app", func() {
		for _, db := range dbList {
			// Create database
			createDBQuery := fmt.Sprintf("CREATE DATABASE %v OWNER app", db)
			_, _, err = exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryPod.Namespace,
					PodName:   primaryPod.Name,
				},
				postgres.PostgresDBName,
				createDBQuery)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	By(fmt.Sprintf("creating table '%s' and insert records on selected db %v", tableName, dbToImport), func() {
		// create a table with two records
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s AS VALUES (1),(2);", tableName)
		_, err = postgres.RunExecOverForward(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, clusterName, dbToImport,
			apiv1.ApplicationUserSecretSuffix, query)
		Expect(err).ToNot(HaveOccurred())
	})

	var importedCluster *apiv1.Cluster
	By("importing Database with microservice approach in a new cluster", func() {
		importedCluster, err = importdb.ImportDatabaseMicroservice(env.Ctx, env.Client, namespace, clusterName,
			importedClusterName, imageName, dbToImport)
		Expect(err).ToNot(HaveOccurred())
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, importedClusterName, 1000, env)
		AssertClusterStandbysAreStreaming(namespace, importedClusterName, 140)
	})

	tableLocator := TableLocator{
		Namespace:    namespace,
		ClusterName:  importedClusterName,
		DatabaseName: postgres.AppDBName,
		TableName:    tableName,
	}
	AssertDataExpectedCount(env, tableLocator, 2)

	By("verifying that only 'app' DB exists in the imported cluster", func() {
		importedPrimaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, importedClusterName)
		Expect(err).ToNot(HaveOccurred())

		Eventually(QueryMatchExpectationPredicate(importedPrimaryPod, postgres.PostgresDBName,
			roleExistsQuery("db2"), "f"), 30).Should(Succeed())
		Eventually(QueryMatchExpectationPredicate(importedPrimaryPod, postgres.PostgresDBName,
			roleExistsQuery("app"), "t"), 30).Should(Succeed())
	})

	By("cleaning up the clusters", func() {
		err = DeleteResourcesFromFile(namespace, sampleFile)
		Expect(err).ToNot(HaveOccurred())

		Expect(objects.Delete(env.Ctx, env.Client, importedCluster)).To(Succeed())
	})
}
