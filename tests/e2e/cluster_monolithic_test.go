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

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// On the source cluster we
// 1. have several roles, and one of them should be a superuser
// 2. have multiple databases, owned by different roles
// we should check on the target cluster :
// 1. imported all the specified databases, keeping the correct owner
// 2. the superuser role should have been downgraded to a normal user
// and testData :
// Taking two database i.e. db1 and db2 and two roles testuserone and testusertwo
var _ = Describe("Imports with Monolithic Approach", Label(tests.LabelImportingDatabases), func() {
	const (
		level             = tests.Medium
		sourceClusterFile = fixturesDir + "/base/cluster-storage-class.yaml.template"
		targetClusterName = "cluster-target"
		tableName         = "to_import"
		databaseSuperUser = "testuserone" // one of the DB users should be a superuser
		databaseUserTwo   = "testusertwo"
		databaseOne       = "db1"
		databaseTwo       = "db2"
	)

	var namespace, sourceClusterName string

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

	It("can import data from a cluster with a different major version", func() {
		var err error
		var sourceClusterHost, sourceClusterPass, targetClusterHost, targetClusterPass string
		sourceDatabases := []string{databaseOne, databaseTwo}
		sourceRoles := []string{databaseSuperUser, databaseUserTwo}

		By("creating the source cluster", func() {
			namespace = "cluster-monolith"
			sourceClusterName, err = env.GetResourceNameFromYAML(sourceClusterFile)
			Expect(err).ToNot(HaveOccurred())
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})
			AssertCreateCluster(namespace, sourceClusterName, sourceClusterFile, env)
		})

		By("creating several roles, one of them a superuser", func() {
			// create 1st user with superuser role
			createSuperUserQuery := fmt.Sprintf("create user %v with superuser password '123';",
				databaseSuperUser)
			sourceClusterHost, err = testsUtils.GetHostName(namespace, sourceClusterName, env)
			Expect(err).ToNot(HaveOccurred())
			sourceClusterPass, err = testsUtils.GetPassword(sourceClusterName, namespace, testsUtils.Superuser, env)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = testsUtils.RunQueryFromPod(
				psqlClientPod,
				sourceClusterHost,
				testsUtils.PostgresDBName,
				testsUtils.PostgresUser,
				sourceClusterPass,
				createSuperUserQuery,
				env,
			)
			Expect(err).ToNot(HaveOccurred())

			// create 2nd user
			createUserQuery := fmt.Sprintf("create user %v;", databaseUserTwo)
			_, _, err = testsUtils.RunQueryFromPod(
				psqlClientPod,
				sourceClusterHost,
				testsUtils.PostgresDBName,
				testsUtils.PostgresUser,
				sourceClusterPass,
				createUserQuery,
				env,
			)
			Expect(err).ToNot(HaveOccurred())
		})

		By("creating the source databases", func() {
			queries := []string{
				fmt.Sprintf("create database %v;", databaseOne),
				fmt.Sprintf("alter database %v owner to %v;", databaseOne, databaseSuperUser),
				fmt.Sprintf("create database %v", databaseTwo),
				fmt.Sprintf("alter database %v owner to %v;", databaseTwo, databaseUserTwo),
			}

			for _, query := range queries {
				_, _, err = testsUtils.RunQueryFromPod(
					psqlClientPod,
					sourceClusterHost,
					testsUtils.PostgresDBName,
					testsUtils.PostgresUser,
					sourceClusterPass,
					query,
					env,
				)
				Expect(err).ToNot(HaveOccurred())
			}

			// create test data and insert some records in both databases
			for _, database := range sourceDatabases {
				query := fmt.Sprintf("CREATE TABLE %v AS VALUES (1), (2);", tableName)
				_, _, err = testsUtils.RunQueryFromPod(
					psqlClientPod,
					sourceClusterHost,
					database,
					testsUtils.PostgresUser,
					sourceClusterPass,
					query,
					env,
				)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		By("creating target cluster", func() {
			postgresImage := os.Getenv("POSTGRES_IMG")
			Expect(postgresImage).ShouldNot(BeEmpty(), "POSTGRES_IMG env should not be empty")
			expectedImageName, err := testsUtils.BumpPostgresImageMajorVersion(postgresImage)
			Expect(err).ToNot(HaveOccurred())
			Expect(expectedImageName).ShouldNot(BeEmpty(), "imageName could not be empty")
			err = importDatabasesMonolith(namespace,
				sourceClusterName,
				targetClusterName,
				expectedImageName,
				sourceDatabases,
				sourceRoles)
			Expect(err).ToNot(HaveOccurred())
			AssertClusterIsReady(namespace, targetClusterName, 600, env)
		})

		By("verifying that the specified source databases were imported", func() {
			targetClusterHost, err = testsUtils.GetHostName(namespace, targetClusterName, env)
			Expect(err).ToNot(HaveOccurred())
			targetClusterPass, err = testsUtils.GetPassword(targetClusterName, namespace, testsUtils.Superuser, env)
			Expect(err).ToNot(HaveOccurred())
			for _, database := range sourceDatabases {
				databaseEntryQuery := fmt.Sprintf("SELECT datname FROM pg_database where datname='%v'", database)
				stdOut, _, err := testsUtils.RunQueryFromPod(
					psqlClientPod,
					targetClusterHost,
					testsUtils.PostgresDBName,
					testsUtils.PostgresUser,
					targetClusterPass,
					databaseEntryQuery,
					env,
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(strings.Contains(stdOut, database)).Should(BeTrue())
			}
		})

		By(fmt.Sprintf("verifying that the source superuser '%s' became a normal user in target",
			databaseSuperUser), func() {
			getSuperUserQuery := "select * from pg_user where usesuper"
			stdOut, _, err := testsUtils.RunQueryFromPod(
				psqlClientPod,
				targetClusterHost,
				testsUtils.PostgresDBName,
				testsUtils.PostgresUser,
				targetClusterPass,
				getSuperUserQuery,
				env,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Contains(stdOut, databaseSuperUser)).Should(BeFalse())
		})

		By("verifying the test data was imported from the source databases", func() {
			for _, database := range sourceDatabases {
				selectQuery := fmt.Sprintf("select count(*) from %v", tableName)
				stdOut, _, err := testsUtils.RunQueryFromPod(
					psqlClientPod,
					targetClusterHost,
					database,
					testsUtils.PostgresUser,
					targetClusterPass,
					selectQuery,
					env,
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(strings.TrimSpace(stdOut)).Should(BeEquivalentTo("2"))
			}
		})
	})
})

// importDatabasesMonolith creates a new cluster spec importing from a sourceCluster
// using the Monolith approach.
// Imports all the specified `databaseNames` and `roles` from the source cluster
func importDatabasesMonolith(
	namespace,
	sourceClusterName,
	importedClusterName,
	imageName string,
	databaseNames []string,
	roles []string,
) error {
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	host := fmt.Sprintf("%v-rw.%v.svc", sourceClusterName, namespace)
	superUserSecretName := fmt.Sprintf("%v", sourceClusterName) + "-superuser"
	targetCluster := &apiv1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name:      importedClusterName,
			Namespace: namespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances: 3,
			ImageName: imageName,

			StorageConfiguration: apiv1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClassName,
			},

			Bootstrap: &apiv1.BootstrapConfiguration{
				InitDB: &apiv1.BootstrapInitDB{
					Import: &apiv1.Import{
						Type:      "monolith",
						Databases: databaseNames,
						Roles:     roles,
						Source: apiv1.ImportSource{
							ExternalCluster: sourceClusterName,
						},
					},
				},
			},
			ExternalClusters: []apiv1.ExternalCluster{
				{
					Name:                 sourceClusterName,
					ConnectionParameters: map[string]string{"host": host, "user": "postgres", "dbname": "postgres"},
					Password: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: superUserSecretName,
						},
						Key: "password",
					},
				},
			},
		},
	}

	return testsUtils.CreateObject(env, targetCluster)
}
