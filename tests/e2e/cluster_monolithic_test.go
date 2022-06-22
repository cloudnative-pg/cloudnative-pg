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
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// On the source cluster we
// 1. have some different roles, and one of then should be a superuser
// 2. have multiple databases, owned by different role
// we should check on the target cluster :
// 1. contains any database in the source database, owned by the respective role
// 2. the superuser role should have been downgraded to a normal user
// and testData :
// Taking two database i.e. db1 and db2 and two roles testuserone and testusertwo
var _ = Describe("Monolithic Approach To Cluster Import", func() {
	const (
		level                       = tests.Medium
		sourceClusterFile           = fixturesDir + "/base/cluster-storage-class.yaml"
		targetClusterName           = "cluster-target"
		tableName                   = "to_import"
		databaseUserAsSuperUserRole = "testuserone"
		databaseUserTwo             = "testusertwo"
		databaseOne                 = "db1"
		databaseTwo                 = "db2"
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
			env.DumpNamespaceObjects(namespace,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})
	It("can import data from a PostgreSQL cluster with a different major version "+
		"using monolithic approach", func() {
		var primaryPod *corev1.Pod
		var targetDatabasePrimaryPod *corev1.Pod
		var err error
		expectedDatabases := []string{databaseOne, databaseTwo}
		expectedRoles := []string{databaseUserAsSuperUserRole, databaseUserTwo}
		commandTimeout := time.Second * 5

		By("creating source cluster", func() {
			namespace = "cluster-monolith"
			sourceClusterName, err = env.GetResourceNameFromYAML(sourceClusterFile)
			Expect(err).ToNot(HaveOccurred())
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, sourceClusterName, sourceClusterFile, env)
		})

		By("creating some different roles, and one of then should be a superuser", func() {
			primaryPod, err = env.GetClusterPrimary(namespace, sourceClusterName)
			Expect(err).ToNot(HaveOccurred())
			// create 1st user with superuser role
			createSuperUserQuery := fmt.Sprintf("create user %v with superuser password '123';",
				databaseUserAsSuperUserRole)
			_, _, err = env.EventuallyExecCommand(env.Ctx, *primaryPod, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", "postgres", "-tAc", createSuperUserQuery)
			Expect(err).ToNot(HaveOccurred())

			// create 2nd user
			createUserQuery := fmt.Sprintf("create user %v;", databaseUserTwo)
			_, _, err = env.EventuallyExecCommand(env.Ctx, *primaryPod, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", "postgres", "-tAc", createUserQuery)
			Expect(err).ToNot(HaveOccurred())
		})

		By("have multiple databases, owned by different role", func() {
			// create 1st database
			createDatabaseQuery := fmt.Sprintf("create database %v;", databaseOne)
			_, _, err = env.EventuallyExecCommand(env.Ctx, *primaryPod, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", "postgres", "-tAc", createDatabaseQuery)
			Expect(err).ToNot(HaveOccurred())

			// assign ownership as superuser role
			alterDatabaseRoleQuery := fmt.Sprintf("alter database %v owner to %v;",
				databaseOne, databaseUserAsSuperUserRole)
			_, _, err = env.EventuallyExecCommand(env.Ctx, *primaryPod, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", "postgres", "-tAc", alterDatabaseRoleQuery)
			Expect(err).ToNot(HaveOccurred())

			// create 2nd database
			createDatabaseQuery = fmt.Sprintf("create database %v", databaseTwo)
			_, _, err = env.EventuallyExecCommand(env.Ctx, *primaryPod, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", "postgres", "-tAc", createDatabaseQuery)
			Expect(err).ToNot(HaveOccurred())

			// assign ownership as normal user
			alterDatabaseRoleQuery = fmt.Sprintf("alter database %v owner to %v;", databaseTwo, databaseUserTwo)
			_, _, err = env.EventuallyExecCommand(env.Ctx, *primaryPod, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", "postgres", "-tAc", alterDatabaseRoleQuery)
			Expect(err).ToNot(HaveOccurred())

			// create test data and insert some records in both databases
			for _, database := range expectedDatabases {
				query := fmt.Sprintf("CREATE TABLE %v AS VALUES (1), (2);", tableName)
				_, _, err = env.EventuallyExecCommand(env.Ctx, *primaryPod, specs.PostgresContainerName,
					&commandTimeout, "psql", "-U", "postgres", fmt.Sprintf("%v", database),
					"-tAc", query)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		By("creating target cluster", func() {
			postgresImage := os.Getenv("POSTGRES_IMG")
			Expect(postgresImage).ShouldNot(BeEmpty(), "POSTGRES_IMG env could not be empty")
			expectedImageName, err := getExpectedPostgresImageNameForTargetCluster(postgresImage)
			Expect(err).ToNot(HaveOccurred())
			Expect(expectedImageName).ShouldNot(BeEmpty(), "imageName could not be empty")
			err = createTargetCluster(namespace,
				sourceClusterName,
				targetClusterName,
				expectedImageName,
				expectedDatabases,
				expectedRoles)
			Expect(err).ToNot(HaveOccurred())
			AssertClusterIsReady(namespace, targetClusterName, 600, env)
		})

		By("verify that any database in the source database, owned by the respective role", func() {
			targetDatabasePrimaryPod, err = env.GetClusterPrimary(namespace, targetClusterName)
			Expect(err).ToNot(HaveOccurred())
			for _, database := range expectedDatabases {
				databaseEntryQuery := fmt.Sprintf("SELECT datname FROM pg_database where datname='%v'", database)
				stdOut, _, err := env.EventuallyExecCommand(env.Ctx, *targetDatabasePrimaryPod,
					specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "postgres", "-tAc", databaseEntryQuery)
				Expect(err).ToNot(HaveOccurred())
				Expect(strings.Contains(stdOut, database)).Should(BeTrue())
			}
		})

		By(fmt.Sprintf("verify that superuser '%v' role should downgraded to a normal user",
			databaseUserAsSuperUserRole), func() {
			getSuperUserQuery := "select * from pg_user where usesuper"
			stdOut, _, err := env.EventuallyExecCommand(env.Ctx, *targetDatabasePrimaryPod, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", "postgres", "-tAc", getSuperUserQuery)
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Contains(stdOut, databaseUserAsSuperUserRole)).Should(BeFalse())
		})

		By("verifying test data in databases on targeted cluster", func() {
			for _, database := range expectedDatabases {
				selectQuery := fmt.Sprintf("select count(*) from %v", tableName)
				stdOut, _, err := env.EventuallyExecCommand(env.Ctx, *targetDatabasePrimaryPod,
					specs.PostgresContainerName, &commandTimeout, "psql", "-U", "postgres",
					fmt.Sprintf("%v", database), "-tAc", selectQuery)
				Expect(err).ToNot(HaveOccurred())
				Expect(strings.TrimSpace(stdOut)).Should(BeEquivalentTo("2"))
			}
		})
	})
})

func getExpectedPostgresImageNameForTargetCluster(postgresImage string) (string, error) {
	var expectedPostgresVersion int
	var imageRepo, imageVersion, imageName string
	var err error
	// split the postgres imageName with ':' and store in imageRepo and imageVersion
	splitPostgresImageInfo := strings.Split(postgresImage, ":")
	if len(splitPostgresImageInfo) == 2 {
		imageRepo, imageVersion = splitPostgresImageInfo[0], splitPostgresImageInfo[1]
		// if minor version will present then split using "." and get major version value
		if strings.Contains(imageVersion, ".") {
			imageVersionAfterSplit := strings.Split(imageVersion, ".")
			expectedPostgresVersion, err = strconv.Atoi(imageVersionAfterSplit[0])
		} else {
			expectedPostgresVersion, err = strconv.Atoi(imageVersion)
		}
		if err != nil {
			return "", err
		}
		// if major version value is 14 then keep as same otherwise increase this with 1
		// e.g. if major version of source cluster is 11, then target cluster would be 12
		if expectedPostgresVersion != 14 {
			expectedPostgresVersion++
		}
		imageName = imageRepo + ":" + fmt.Sprintf("%v", expectedPostgresVersion)
		return imageName, err
	}
	return "", nil
}

func createTargetCluster(
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
					Import: &apiv1.LogicalSnapshot{
						Type:      "monolith",
						Databases: databaseNames,
						Roles:     roles,
						Source: apiv1.LogicalSnapshotSource{
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
