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

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Importing Database", Label(tests.LabelBackupRestore), func() {
	const (
		level                 = tests.Medium
		sourceSampleFile      = fixturesDir + "/cluster_microservice/cluster-base.yaml"
		ImportClusterFile     = fixturesDir + "/cluster_microservice/cluster_microservice.yaml"
		tableName             = "to_import"
		expectedEpasImageName = "quay.io/enterprisedb/edb-postgres-advanced:14.2"
	)

	var namespace, clusterName, importedClusterName string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
			env.DumpClusterEnv(namespace, importedClusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	//
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("using cnp microservice", func() {
		// This is a set of tests using a cnp microservice to import database
		// from external cluster
		// It cover four scenarios
		// 1. positive with large object 2.Normalization
		// 3. Different db 4. Negative
		It("with large objects", func() {
			// Test case to import DB having large object
			var err error
			// Namespace name
			namespace = "cnp-microservice-large-object"
			// Fetching Source Cluster Name
			clusterName, err = env.GetResourceNameFromYAML(sourceSampleFile)
			Expect(err).ToNot(HaveOccurred())
			// Name of the cluster to be created using source cluster
			importedClusterName = "cluster-pgdump-large-object"
			// Creating NameSpace.
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			// Creating source cluster
			AssertCreateCluster(namespace, clusterName, sourceSampleFile, env)
			// Create required test data on source db
			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)
			// Creating Large Objects on source DB.
			AssertCreateTestDataLargeObject(namespace, clusterName)
			// Creating new cluster using source cluster.
			AssertClusterImport(namespace, importedClusterName, clusterName, "app")
			// Test data should be present on restored primary and on app database
			primary := importedClusterName + "-1"
			AssertDataExpectedCount(namespace, primary, tableName, 2)
			// Large object should be present on restored cluster primary.
			AssertLargeObjectValue(namespace, primary, 16393)
		})

		It("having normalization", func() {
			// This test case includes data insertion on a new table on source db
			// assigning app user as read only rights and importing the same.
			var err error
			// NameSpace Name
			namespace = "cnp-microservice-normalization"
			// Fetching Source Cluster Name
			clusterName, err = env.GetResourceNameFromYAML(sourceSampleFile)
			Expect(err).ToNot(HaveOccurred())
			// Name of the cluster to be created using source cluster
			importedClusterName = "cluster-pgdump-normalization"
			// Creating NameSpace.
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			// Creating source cluster
			AssertCreateCluster(namespace, clusterName, sourceSampleFile, env)
			// Create table user and assign ownership on source DB table and insert records
			assertDataOnSourceCluster(namespace, tableName, clusterName)
			// Creating new cluster using source cluster.
			AssertClusterImport(namespace, importedClusterName, clusterName, "app")
			// Test data should be present on restored primary and on app DB
			primary := importedClusterName + "-1"
			AssertDataExpectedCount(namespace, primary, tableName, 2)
			// Verify Data on Imported cluster Like tables and user not present as on source.
			assertDataOnImportedCluster(namespace, tableName, importedClusterName)
		})

		It("using different database ", func() {
			namespace = "cnp-microservice-different-db"
			importedClusterName = "cluster-pgdump-different-db"
			assertImportDataUsingDifferentDatabases(namespace, sourceSampleFile,
				importedClusterName, tableName, "")
		})

		It("having a non existing db on source", func() {
			// Test case which will check cluster is not created when we use a
			// nonexistent database in cluster definition while importing
			var err error
			// NameSpace Name
			namespace = "cnp-microservice-error"
			// Fetching Source Cluster Name
			clusterName, err = env.GetResourceNameFromYAML(sourceSampleFile)
			Expect(err).ToNot(HaveOccurred())
			// Name of the cluster to be created using source cluster
			importedClusterName = "cluster-pgdump-error"
			// Creating NameSpace.
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			// Creating source cluster
			AssertCreateCluster(namespace, clusterName, sourceSampleFile, env)
			// Creating new cluster using source cluster.
			CreateResourceFromFile(namespace, ImportClusterFile)
			By("having a imported Cluster in failed state", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      importedClusterName + "-1-logicalsnapshot",
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

		// verify the PostgreSQL 11 as source DB, and PostgreSQL 14 as target DB
		It("can import the PostgreSQL 11 as source, and PostgreSQL 14 as target", func() {
			postgresImage := os.Getenv("POSTGRES_IMG")
			desiredSourceVersion := "11"
			if !strings.Contains(postgresImage, ":"+desiredSourceVersion) {
				Skip("This test is only applicable for PostgreSQL " + desiredSourceVersion)
			} else {
				namespace = "cnp-microservice-different-db-version"
				importedClusterName = "cluster-pgdump-different-db-version"
				expectedImageNameForImportedCluster := versions.DefaultImageName
				// if redwood env value is true/false then will take image type as epas
				// else image type as postgreSQL
				if os.Getenv("E2E_ENABLE_REDWOOD") != "" {
					expectedImageNameForImportedCluster = expectedEpasImageName
				}
				assertImportDataUsingDifferentDatabases(namespace, sourceSampleFile, importedClusterName,
					tableName, expectedImageNameForImportedCluster)
			}
		})
	})
})

// assertDataOnSourceCluster will generate super password and necessary assert on db
func assertDataOnSourceCluster(
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

// assertDataOnImportedCluster  will generate super password and necessary assert on imported cluster
func assertDataOnImportedCluster(
	namespace,
	tableName,
	importedClusterName string,
) {
	By("generate super user password,rw service name on Imported cluster", func() {
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
		By("Verify created user on source is not present on imported database", func() {
			outUser, _, err := testsUtils.RunQueryFromPod(
				importedPrimaryPod, importedrwService, "app", "postgres",
				generatedSuperuserPassword, "\\du", env)
			Expect(strings.Contains(outUser, "micro"), err).Should(BeFalse())
		})
	})
}

func assertImportDataUsingDifferentDatabases(
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
	// create namespace
	err = env.CreateNamespace(namespace)
	Expect(err).ToNot(HaveOccurred())
	AssertCreateCluster(namespace, clusterName, sampleFile, env)

	By("create multiple db on source and set ownership to app", func() {
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

	By(fmt.Sprintf("create table '%s' and insert records on db %v", tableName, dbToImport), func() {
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
		err = testsUtils.CreateClusterFromExternalCluster(namespace, importedClusterName,
			clusterName, dbToImport, env, imageName)
		Expect(err).ToNot(HaveOccurred())
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, importedClusterName, 800, env)
		// Restored standby should be attached to restored primary
		assertClusterStandbysAreStreaming(namespace, importedClusterName)
	})

	// Test data should be present on restored primary
	importedClusterPrimaryInfo, err = env.GetClusterPrimary(namespace, importedClusterName)
	Expect(err).ToNot(HaveOccurred())
	// Check the data created in "db2" is now present in "app" in the created cluster
	AssertDataExpectedCount(namespace, importedClusterPrimaryInfo.Name, tableName, 2)

	By("verify that only 'app' DB exists in the imported cluster", func() {
		var getSuperUserPassword string
		getSuperUserPassword, err = testsUtils.GetPassword(importedClusterName, namespace, "superuser", env)
		Expect(err).ToNot(HaveOccurred())
		rwService := fmt.Sprintf("%v-rw.%v.svc", importedClusterName, namespace)
		stdOut, _, err := testsUtils.RunQueryFromPod(importedClusterPrimaryInfo, rwService,
			"postgres", "postgres", getSuperUserPassword, "\\l", env)
		Expect(err).ToNot(HaveOccurred(), err)
		// “db2” database should not exist in the destination cluster
		Expect(strings.Contains(stdOut, "db2"), err).Should(BeFalse())
		// “app” database is the only one existing in the destination cluster
		Expect(strings.Contains(stdOut, "app"), err).Should(BeTrue())
	})
}
