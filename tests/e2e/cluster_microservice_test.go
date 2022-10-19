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
	"os"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

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
var _ = Describe("Imports with Microservice Approach", Label(tests.LabelBackupRestore), func() {
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

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	It("can import a database with large objects", func() {
		var err error
		namespace = "microservice-large-object"
		sourceClusterName, err = env.GetResourceNameFromYAML(sourceSampleFile)
		Expect(err).ToNot(HaveOccurred())

		oid := 16393
		data := "large object test"
		err = env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})
		AssertCreateCluster(namespace, sourceClusterName, sourceSampleFile, env)
		AssertCreateTestData(namespace, sourceClusterName, tableName)
		AssertCreateTestDataLargeObject(namespace, sourceClusterName, oid, data)

		importedClusterName = "cluster-pgdump-large-object"
		AssertClusterImport(namespace, importedClusterName, sourceClusterName, "app")
		primary := importedClusterName + "-1"
		AssertDataExpectedCount(namespace, primary, tableName, 2)
		AssertLargeObjectValue(namespace, primary, oid, data)
	})

	It("can import a database", func() {
		var err error
		namespace = "microservice"
		sourceClusterName, err = env.GetResourceNameFromYAML(sourceSampleFile)
		Expect(err).ToNot(HaveOccurred())

		err = env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})
		AssertCreateCluster(namespace, sourceClusterName, sourceSampleFile, env)
		assertCreateTableWithDataOnSourceCluster(namespace, tableName, sourceClusterName)

		importedClusterName = "cluster-pgdump"
		AssertClusterImport(namespace, importedClusterName, sourceClusterName, "app")
		primary := importedClusterName + "-1"
		AssertDataExpectedCount(namespace, primary, tableName, 2)
		assertTableAndDataOnImportedCluster(namespace, tableName, importedClusterName)
	})

	It("can select one from several databases to import", func() {
		namespace = "microservice-different-db"
		importedClusterName = "cluster-pgdump-different-db"
		// create namespace
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})
		assertImportRenamesSelectedDatabase(namespace, sourceSampleFile,
			importedClusterName, tableName, "")
	})

	It("fails importing when db does not exist in source cluster", func() {
		// Test case which will check cluster is not created when we use a
		// nonexistent database in cluster definition while importing
		var err error
		namespace = "cnpg-microservice-error"
		sourceClusterName, err = env.GetResourceNameFromYAML(sourceSampleFile)
		Expect(err).ToNot(HaveOccurred())
		err = env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})
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
		desiredSourceVersion := "11"
		namespace = "microservice-different-db-version"
		importedClusterName = "cluster-pgdump-different-db-version"

		// this test case only applicable postgres version 11
		if shouldSkip(desiredSourceVersion) {
			Skip("This test is only applicable for PostgreSQL " + desiredSourceVersion)
		}

		targetImage := versions.DefaultImageName

		By(fmt.Sprintf("import cluster with different major, target version is %s", targetImage), func() {
			// create namespace
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})
			assertImportRenamesSelectedDatabase(namespace, sourceSampleFile, importedClusterName,
				tableName, targetImage)
		})
	})
})

// check if current postgres version is pg 11
func shouldSkip(expectedSourceVersion string) bool {
	return !strings.Contains(os.Getenv("POSTGRES_IMG"), ":11"+expectedSourceVersion)
}

// assertCreateTableWithDataOnSourceCluster creates a new user `micro` in the source cluster,
// and uses the `postgres` superuser to generate a new table and assign ownership to `micro`
func assertCreateTableWithDataOnSourceCluster(
	namespace,
	tableName,
	clusterName string,
) {
	By("generate super user password,rw service name on source cluster", func() {
		// Fetching Primary
		primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		generatedSuperuserPassword, err := testsUtils.GetPassword(clusterName, namespace, "superuser", env)
		Expect(err).ToNot(HaveOccurred())
		rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
		By("create user, insert record in new table, assign new user as owner "+
			"and grant read only to app user", func() {
			query := fmt.Sprintf("CREATE USER micro;CREATE TABLE %v AS VALUES (1), "+
				"(2);ALTER TABLE %v OWNER TO micro;grant select on %v to app;", tableName, tableName, tableName)
			_, _, err = testsUtils.RunQueryFromPod(
				primaryPod, rwService, "app", "postgres", generatedSuperuserPassword, query, env)
			Expect(err).ToNot(HaveOccurred())
		})
	})
}

// assertTableAndDataOnImportedCluster  verifies the data created in source was imported
func assertTableAndDataOnImportedCluster(
	namespace,
	tableName,
	importedClusterName string,
) {
	By("verifying presence of table and data from source in imported cluster", func() {
		// Fetch import cluster name
		importedPrimaryPod, err := env.GetClusterPrimary(namespace, importedClusterName)
		Expect(err).ToNot(HaveOccurred())
		generatedSuperuserPassword, err := testsUtils.GetPassword(importedClusterName,
			namespace, "superuser", env)
		Expect(err).ToNot(HaveOccurred())
		importedrwService := fmt.Sprintf("%v-rw.%v.svc", importedClusterName, namespace)
		By("Verifying imported table has owner app user", func() {
			queryImported := fmt.Sprintf("select * from pg_tables where tablename = '%v' "+
				"and tableowner = 'app';", tableName)

			out, _, err := testsUtils.RunQueryFromPod(
				importedPrimaryPod, importedrwService, "app", "postgres",
				generatedSuperuserPassword, queryImported, env)
			Expect(strings.Contains(out, tableName), err).Should(BeTrue())
		})
		By("verifying the user named 'micro' on source is not in imported database", func() {
			outUser, _, err := testsUtils.RunQueryFromPod(
				importedPrimaryPod, importedrwService, "app", "postgres",
				generatedSuperuserPassword, "\\du", env)
			Expect(strings.Contains(outUser, "micro"), err).Should(BeFalse())
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
	var sourceClusterPrimaryInfo, importedClusterPrimaryInfo *corev1.Pod
	clusterName, err := env.GetResourceNameFromYAML(sampleFile)
	Expect(err).ToNot(HaveOccurred())

	AssertCreateCluster(namespace, clusterName, sampleFile, env)

	By("creating multiple dbs on source and set ownership to app", func() {
		sourceClusterPrimaryInfo, err = env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		timeout := time.Second * 2
		for _, db := range dbList {
			// Create database
			createDBQuery := fmt.Sprintf("create database %v;", db)
			_, _, err = env.ExecCommand(env.Ctx, *sourceClusterPrimaryInfo, specs.PostgresContainerName, &timeout,
				"psql", "-U", "postgres", "-tAc", createDBQuery)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = env.ExecCommand(env.Ctx, *sourceClusterPrimaryInfo, specs.PostgresContainerName, &timeout,
				"psql", "-U", "postgres", "-tAc",
				fmt.Sprintf("ALTER DATABASE %v OWNER TO app;", db))
			Expect(err).ToNot(HaveOccurred())
		}
	})

	By(fmt.Sprintf("creating table '%s' and insert records on selected db %v", tableName, dbToImport), func() {
		var getSuperUserPassword string
		getSuperUserPassword, err = testsUtils.GetPassword(clusterName, namespace, "superuser", env)
		Expect(err).ToNot(HaveOccurred())
		rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
		// set role app on db2
		_, _, err = testsUtils.RunQueryFromPod(sourceClusterPrimaryInfo, rwService,
			dbToImport, "postgres", getSuperUserPassword,
			"set role app;", env)
		Expect(err).ToNot(HaveOccurred())
		// create test data and insert records
		_, _, err = testsUtils.RunQueryFromPod(sourceClusterPrimaryInfo, rwService,
			dbToImport, "postgres", getSuperUserPassword,
			fmt.Sprintf("CREATE TABLE %s AS VALUES (1),(2);", tableName), env)
		Expect(err).ToNot(HaveOccurred())
	})

	By("importing Database with microservice approach in a new cluster", func() {
		err = testsUtils.ImportDatabaseMicroservice(namespace, importedClusterName,
			clusterName, dbToImport, env, imageName)
		Expect(err).ToNot(HaveOccurred())
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, importedClusterName, 1000, env)
		assertClusterStandbysAreStreaming(namespace, importedClusterName)
	})

	importedClusterPrimaryInfo, err = env.GetClusterPrimary(namespace, importedClusterName)
	Expect(err).ToNot(HaveOccurred())
	AssertDataExpectedCount(namespace, importedClusterPrimaryInfo.Name, tableName, 2)

	By("verifying that only 'app' DB exists in the imported cluster", func() {
		var getSuperUserPassword string
		getSuperUserPassword, err = testsUtils.GetPassword(importedClusterName, namespace, "superuser", env)
		Expect(err).ToNot(HaveOccurred())
		rwService := fmt.Sprintf("%v-rw.%v.svc", importedClusterName, namespace)
		dbList, _, err := testsUtils.RunQueryFromPod(importedClusterPrimaryInfo, rwService,
			"postgres", "postgres", getSuperUserPassword, "\\l", env)
		Expect(err).ToNot(HaveOccurred(), err)
		Expect(strings.Contains(dbList, "db2"), err).Should(BeFalse())
		Expect(strings.Contains(dbList, "app"), err).Should(BeTrue())
	})
}
