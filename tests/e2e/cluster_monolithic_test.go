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
	"database/sql"
	"fmt"
	"os"

	"github.com/lib/pq"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/importdb"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

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
		sourceClusterFile = fixturesDir + "/cluster_monolith/cluster-monolith.yaml.template"
		targetClusterName = "cluster-target"
		tableName         = "to_import"
		databaseSuperUser = "testuserone" // one of the DB users should be a superuser
		databaseUserTwo   = "testusertwo"
		databaseOne       = "db1"
		databaseTwo       = "db2"
	)

	var namespace, sourceClusterName string
	var forwardTarget *postgres.PSQLForwardConnection
	var connTarget *sql.DB

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("can import data from a cluster with a different major version", func() {
		var err error
		sourceDatabases := []string{databaseOne, databaseTwo}
		sourceRoles := []string{databaseSuperUser, databaseUserTwo}

		By("creating the source cluster", func() {
			const namespacePrefix = "cluster-monolith"
			sourceClusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sourceClusterFile)
			Expect(err).ToNot(HaveOccurred())
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, sourceClusterName, sourceClusterFile, env)
		})

		By("creating several roles, one of them a superuser and source databases", func() {
			forward, conn, err := postgres.ForwardPSQLConnection(
				env.Ctx,
				env.Client,
				env.Interface,
				env.RestClientConfig,
				namespace,
				sourceClusterName,
				postgres.PostgresDBName,
				apiv1.SuperUserSecretSuffix,
			)
			defer func() {
				_ = conn.Close()
				forward.Close()
			}()
			Expect(err).ToNot(HaveOccurred())

			// create 1st user with superuser role
			createSuperUserQuery := fmt.Sprintf("create user %v with superuser password '123';",
				databaseSuperUser)
			_, err = conn.Exec(createSuperUserQuery)
			Expect(err).ToNot(HaveOccurred())

			// create 2nd user
			createUserQuery := fmt.Sprintf("create user %v;", databaseUserTwo)
			_, err = conn.Exec(createUserQuery)
			Expect(err).ToNot(HaveOccurred())

			queries := []string{
				fmt.Sprintf("create database %v;", databaseOne),
				fmt.Sprintf("alter database %v owner to %v;", databaseOne, databaseSuperUser),
				fmt.Sprintf("create database %v", databaseTwo),
				fmt.Sprintf("alter database %v owner to %v;", databaseTwo, databaseUserTwo),
			}

			for _, query := range queries {
				_, err := conn.Exec(query)
				Expect(err).ToNot(HaveOccurred())
			}

			// create test data and insert some records in both databases
			for _, database := range sourceDatabases {
				query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s AS VALUES (1),(2);", tableName)
				conn, err := forward.GetPooler().Connection(database)
				Expect(err).ToNot(HaveOccurred())
				// We need to set the max idle connection back to a higher number
				// otherwise the conn.Exec() will close the connection
				// and that will produce a RST packet from PostgreSQL that will kill the
				// port-forward tunnel
				// More about the RST packet here https://www.postgresql.org/message-id/165ba87e-fa48-4eae-b1f3-f9a831b4890b%40Spark
				conn.SetMaxIdleConns(3)
				_, err = conn.Exec(query)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		By("creating target cluster", func() {
			postgresImage := os.Getenv("POSTGRES_IMG")
			Expect(postgresImage).ShouldNot(BeEmpty(), "POSTGRES_IMG env should not be empty")
			expectedImageName, err := postgres.BumpPostgresImageMajorVersion(postgresImage)
			Expect(err).ToNot(HaveOccurred())
			Expect(expectedImageName).ShouldNot(BeEmpty(), "imageName could not be empty")

			_, err = importdb.ImportDatabasesMonolith(
				env.Ctx,
				env.Client,
				namespace,
				sourceClusterName,
				targetClusterName,
				expectedImageName,
				sourceDatabases,
				sourceRoles,
			)
			Expect(err).ToNot(HaveOccurred())
			AssertClusterIsReady(namespace, targetClusterName, testTimeouts[timeouts.ClusterIsReady], env)
		})

		By("connect to the imported cluster", func() {
			forwardTarget, connTarget, err = postgres.ForwardPSQLConnection(
				env.Ctx,
				env.Client,
				env.Interface,
				env.RestClientConfig,
				namespace,
				targetClusterName,
				postgres.PostgresDBName,
				apiv1.SuperUserSecretSuffix,
			)
			Expect(err).ToNot(HaveOccurred())
		})

		By("verifying that the specified source databases were imported", func() {
			stmt, err := connTarget.Prepare("SELECT datname FROM pg_catalog.pg_database WHERE datname IN ($1)")
			Expect(err).ToNot(HaveOccurred())
			rows, err := stmt.QueryContext(env.Ctx, pq.Array(sourceDatabases))
			Expect(err).ToNot(HaveOccurred())
			var datName string
			for rows.Next() {
				err = rows.Scan(&datName)
				Expect(err).ToNot(HaveOccurred())
				Expect(sourceDatabases).Should(ContainElement(datName))
			}
		})

		By("verifying that no extra application database or owner were created", func() {
			stmt, err := connTarget.Prepare("SELECT count(*) FROM pg_catalog.pg_database WHERE datname = $1")
			Expect(err).ToNot(HaveOccurred())
			var matchCount int
			err = stmt.QueryRowContext(env.Ctx, "app").Scan(&matchCount)
			Expect(err).ToNot(HaveOccurred())
			Expect(matchCount).To(BeZero(), "app database should not exist")
			stmt, err = connTarget.Prepare("SELECT count(*) from pg_catalog.pg_user WHERE usename = $1")
			Expect(err).ToNot(HaveOccurred())
			err = stmt.QueryRowContext(env.Ctx, "app").Scan(&matchCount)
			Expect(err).ToNot(HaveOccurred())
			Expect(matchCount).To(BeZero(), "app user should not exist")
		})

		By(fmt.Sprintf("verifying that the source superuser '%s' became a normal user in target",
			databaseSuperUser), func() {
			row := connTarget.QueryRow(fmt.Sprintf(
				"SELECT usesuper FROM pg_catalog.pg_user WHERE usename='%s'",
				databaseSuperUser))
			var superUser bool
			err := row.Scan(&superUser)
			Expect(err).ToNot(HaveOccurred())
			Expect(superUser).Should(BeFalse())
		})

		By("verifying the test data was imported from the source databases", func() {
			for _, database := range sourceDatabases {
				selectQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
				connTemp, err := forwardTarget.GetPooler().Connection(database)
				Expect(err).ToNot(HaveOccurred())
				// We need to set the max idle connection back to a higher number
				// otherwise the conn.Exec() will close the connection
				// and that will produce a RST packet from PostgreSQL that will kill the
				// port-forward tunnel
				// More about the RST packet here https://www.postgresql.org/message-id/165ba87e-fa48-4eae-b1f3-f9a831b4890b%40Spark
				connTemp.SetMaxIdleConns(3)
				row := connTemp.QueryRow(selectQuery)
				var count int
				err = row.Scan(&count)
				Expect(err).ToNot(HaveOccurred())
				Expect(count).To(BeEquivalentTo(2))
			}
		})

		By("close connection to imported and the source cluster", func() {
			forwardTarget.Close()
		})
	})
})
