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

	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
		sourceClusterName, err = env.GetResourceNameFromYAML(sourceSampleFile)
		Expect(err).ToNot(HaveOccurred())

		oid := 16393
		data := "large object test"
		namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, sourceClusterName, sourceSampleFile, env)
		AssertCreateTestData(namespace, sourceClusterName, tableName, psqlClientPod)
		AssertCreateTestDataLargeObject(namespace, sourceClusterName, oid, data, psqlClientPod)

		importedClusterName = "cluster-pgdump-large-object"
		cluster := AssertClusterImport(namespace, importedClusterName, sourceClusterName, "app")
		AssertDataExpectedCount(namespace, importedClusterName, tableName, 2, psqlClientPod)
		AssertLargeObjectValue(namespace, importedClusterName, oid, data, psqlClientPod)
		By("deleting the imported database", func() {
			Expect(testsUtils.DeleteObject(env, cluster)).To(Succeed())
		})
	})

	It("can import a database", func() {
		var err error
		const namespacePrefix = "microservice"
		sourceClusterName, err = env.GetResourceNameFromYAML(sourceSampleFile)
		Expect(err).ToNot(HaveOccurred())

		namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, sourceClusterName, sourceSampleFile, env)
		assertCreateTableWithDataOnSourceCluster(namespace, tableName, sourceClusterName)

		importedClusterName = "cluster-pgdump"
		AssertClusterImport(namespace, importedClusterName, sourceClusterName, "app")
		AssertDataExpectedCount(namespace, importedClusterName, tableName, 2, psqlClientPod)
		assertTableAndDataOnImportedCluster(namespace, tableName, importedClusterName)
	})

	It("can select one from several databases to import", func() {
		var err error
		const namespacePrefix = "microservice-different-db"
		importedClusterName = "cluster-pgdump-different-db"
		// create namespace
		namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		assertImportRenamesSelectedDatabase(namespace, sourceSampleFile,
			importedClusterName, tableName, "")
	})

	It("fails importing when db does not exist in source cluster", func() {
		// Test case which will check cluster is not created when we use a
		// nonexistent database in cluster definition while importing
		var err error
		const namespacePrefix = "cnpg-microservice-error"
		sourceClusterName, err = env.GetResourceNameFromYAML(sourceSampleFile)
		Expect(err).ToNot(HaveOccurred())
		namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
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
		if shouldSkip(postgresImage) {
			Skip("Already running on the latest major. This test is not applicable for PostgreSQL " + postgresImage)
		}

		// Gather the target image
		targetImage, err := testsUtils.BumpPostgresImageMajorVersion(postgresImage)
		Expect(err).ToNot(HaveOccurred())
		Expect(targetImage).ShouldNot(BeEmpty(), "targetImage could not be empty")

		By(fmt.Sprintf("import cluster with different major, target version is %s", targetImage), func() {
			var err error
			// create namespace
			namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			assertImportRenamesSelectedDatabase(namespace, sourceSampleFile, importedClusterName,
				tableName, targetImage)
		})
	})
})

// shouldSkip skip this test if the current POSTGRES_IMG is already the latest major
func shouldSkip(postgresImage string) bool {
	// Get the current tag
	currentImageReference := reference.New(postgresImage)
	currentImageVersion, err := version.FromTag(currentImageReference.Tag)
	Expect(err).ToNot(HaveOccurred())
	// Get the default tag
	defaultImageReference := reference.New(versions.DefaultImageName)
	defaultImageVersion, err := version.FromTag(defaultImageReference.Tag)
	Expect(err).ToNot(HaveOccurred())

	return currentImageVersion.Major() >= defaultImageVersion.Major()
}

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
		pod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		commandTimeout := time.Second * 10

		query := fmt.Sprintf(
			"DROP USER IF EXISTS micro; "+
				"CREATE USER micro; "+
				"CREATE TABLE IF NOT EXISTS %[1]v AS VALUES (1),(2); "+
				"ALTER TABLE %[1]v OWNER TO micro; "+
				"GRANT SELECT ON %[1]v TO app;",
			tableName)

		_, _, err = env.ExecCommand(
			env.Ctx,
			*pod,
			specs.PostgresContainerName,
			&commandTimeout,
			"psql", "-U", "postgres", "app", "-tAc", query)
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
		pod, err := env.GetClusterPrimary(namespace, importedClusterName)
		Expect(err).ToNot(HaveOccurred())

		By("Verifying imported table has owner app user", func() {
			queryImported := fmt.Sprintf(
				"select * from pg_tables where tablename = '%v' and tableowner = '%v'",
				tableName,
				testsUtils.AppUser,
			)
			out, _, err := env.ExecCommandWithPsqlClient(
				namespace,
				importedClusterName,
				pod,
				apiv1.ApplicationUserSecretSuffix,
				testsUtils.AppDBName,
				queryImported,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Contains(out, tableName), err).Should(BeTrue())
		})

		By("verifying the user named 'micro' on source is not in imported database", func() {
			outUser, _, err := env.ExecCommandWithPsqlClient(
				namespace,
				importedClusterName,
				pod,
				apiv1.ApplicationUserSecretSuffix,
				testsUtils.AppDBName,
				"\\du",
			)
			Expect(err).ToNot(HaveOccurred())
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
	clusterName, err := env.GetResourceNameFromYAML(sampleFile)
	Expect(err).ToNot(HaveOccurred())

	AssertCreateCluster(namespace, clusterName, sampleFile, env)
	primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	commandTimeout := time.Second * 10

	By("creating multiple dbs on source and set ownership to app", func() {
		for _, db := range dbList {
			// Create database
			createDBQuery := fmt.Sprintf("CREATE DATABASE %v OWNER app", db)
			_, _, err = env.ExecCommand(
				env.Ctx,
				*primaryPod,
				specs.PostgresContainerName,
				&commandTimeout,
				"psql", "-U", "postgres", "-tAc", createDBQuery)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	By(fmt.Sprintf("creating table '%s' and insert records on selected db %v", tableName, dbToImport), func() {
		// create a table with two records
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s AS VALUES (1),(2);", tableName)
		_, _, err := env.ExecCommandWithPsqlClient(
			namespace,
			clusterName,
			primaryPod,
			apiv1.ApplicationUserSecretSuffix,
			dbToImport,
			query,
		)
		Expect(err).ToNot(HaveOccurred())
	})

	var importedCluster *apiv1.Cluster
	By("importing Database with microservice approach in a new cluster", func() {
		importedCluster, err = testsUtils.ImportDatabaseMicroservice(namespace, clusterName,
			importedClusterName, imageName, dbToImport, env)
		Expect(err).ToNot(HaveOccurred())
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, importedClusterName, 1000, env)
		AssertClusterStandbysAreStreaming(namespace, importedClusterName, 120)
	})

	AssertDataExpectedCount(namespace, importedClusterName, tableName, 2, psqlClientPod)

	By("verifying that only 'app' DB exists in the imported cluster", func() {
		importedPrimaryPod, err := env.GetClusterPrimary(namespace, importedClusterName)
		Expect(err).ToNot(HaveOccurred())
		out, _, err := env.ExecCommandWithPsqlClient(
			namespace,
			importedClusterName,
			importedPrimaryPod,
			apiv1.ApplicationUserSecretSuffix,
			testsUtils.AppDBName,
			"\\l",
		)
		Expect(err).ToNot(HaveOccurred(), err)
		Expect(strings.Contains(out, "db2"), err).Should(BeFalse())
		Expect(strings.Contains(out, "app"), err).Should(BeTrue())
	})

	By("cleaning up the clusters", func() {
		err = DeleteResourcesFromFile(namespace, sampleFile)
		Expect(err).ToNot(HaveOccurred())

		Expect(testsUtils.DeleteObject(env, importedCluster)).To(Succeed())
	})
}
