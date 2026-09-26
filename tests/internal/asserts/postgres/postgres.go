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

// Package postgres provides Ginkgo/Gomega assertions over the Postgres data
// plane: test data setup, recovery mode checks, and helper query strings.
//
// Callers that also import tests/utils/postgres should alias one of the two
// (e.g. pgasserts for this package, postgres for the primitive layer).
package postgres

import (
	"database/sql"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	pgutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/services"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// TableLocator identifies a table inside a database within a cluster.
type TableLocator struct {
	Namespace    string
	ClusterName  string
	DatabaseName string
	TableName    string
	Tablespace   string
}

// AssertConnection opens a forwarded psql connection to a service, asserting
// that a basic SELECT 1 succeeds within RetryTimeout.
func AssertConnection(
	env *environment.TestingEnvironment,
	namespace string,
	service string,
	dbname string,
	user string,
	password string,
) {
	GinkgoHelper()
	By(fmt.Sprintf("connecting to the %v service as %v", service, user), func() {
		Eventually(func(g Gomega) {
			forwardConn, conn, err := pgutils.ForwardPSQLServiceConnection(
				env.Ctx, env.Interface, env.RestClientConfig,
				namespace, service, dbname, user, password,
			)
			defer func() {
				_ = conn.Close()
				forwardConn.Close()
			}()
			g.Expect(err).ToNot(HaveOccurred())

			var rawValue string
			row := conn.QueryRow("SELECT 1")
			err = row.Scan(&rawValue)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(strings.TrimSpace(rawValue)).To(BeEquivalentTo("1"))
		}, environment.RetryTimeout).Should(Succeed())
	})
}

// AssertCreateTestData creates a small table at the given TableLocator. When
// DatabaseName or Tablespace are empty they default to AppDBName and the
// default tablespace.
func AssertCreateTestData(env *environment.TestingEnvironment, tl TableLocator) {
	GinkgoHelper()
	if tl.DatabaseName == "" {
		tl.DatabaseName = pgutils.AppDBName
	}
	if tl.Tablespace == "" {
		tl.Tablespace = pgutils.TablespaceDefaultName
	}

	By(fmt.Sprintf("creating test data in table %v (cluster %v, database %v, tablespace %v)",
		tl.TableName, tl.ClusterName, tl.DatabaseName, tl.Tablespace), func() {
		forward, conn, err := pgutils.ForwardPSQLConnection(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			tl.Namespace,
			tl.ClusterName,
			tl.DatabaseName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			_ = conn.Close()
			forward.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %v TABLESPACE %v AS VALUES (1),(2);",
			tl.TableName, tl.Tablespace)

		_, err = conn.Exec(query)
		Expect(err).ToNot(HaveOccurred())
	})
}

// AssertCreateTestDataLargeObject creates an image table containing a large
// object with the given oid and data.
func AssertCreateTestDataLargeObject(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	oid int,
	data string,
) {
	GinkgoHelper()
	By("creating large object", func() {
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS image (name text,raster oid); "+
			"INSERT INTO image (name, raster) VALUES ('beautiful image', lo_from_bytea(%d, '%s'));", oid, data)

		_, err := pgutils.RunExecOverForward(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, clusterName, pgutils.AppDBName,
			apiv1.ApplicationUserSecretSuffix, query,
		)
		Expect(err).ToNot(HaveOccurred())
	})
}

// InsertRecordIntoTable inserts a single integer value into the named table
// over an open *sql.DB connection.
func InsertRecordIntoTable(tableName string, value int, conn *sql.DB) {
	GinkgoHelper()
	_, err := conn.Exec(fmt.Sprintf("INSERT INTO %s VALUES (%d)", tableName, value))
	Expect(err).ToNot(HaveOccurred())
}

// QueryMatchExpectationPredicate returns a Gomega predicate that runs the
// query inside the given pod and checks the trimmed output equals
// expectedOutput.
func QueryMatchExpectationPredicate(
	env *environment.TestingEnvironment,
	pod *corev1.Pod,
	dbname exec.DatabaseName,
	query string,
	expectedOutput string,
) func(g Gomega) {
	return func(g Gomega) {
		stdout, stderr, err := exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{Namespace: pod.Namespace, PodName: pod.Name},
			dbname,
			query,
		)
		if err != nil {
			GinkgoWriter.Printf("stdout: %v\nstderr: %v", stdout, stderr)
		}
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(strings.Trim(stdout, "\n")).To(BeEquivalentTo(expectedOutput),
			fmt.Sprintf("expected query %q to return %q (in database %q)", query, expectedOutput, dbname))
	}
}

// RoleExistsQuery builds a SELECT EXISTS query for the named pg_role.
func RoleExistsQuery(roleName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='%v')", roleName)
}

// DatabaseExistsQuery builds a SELECT EXISTS query for the named database.
func DatabaseExistsQuery(dbName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_database WHERE datname='%v')", dbName)
}

// ExtensionExistsQuery builds a SELECT EXISTS query for the named extension.
func ExtensionExistsQuery(extName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT FROM pg_catalog.pg_extension WHERE extname='%v')", extName)
}

// SchemaExistsQuery builds a SELECT EXISTS query for the named schema.
func SchemaExistsQuery(namespaceName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT FROM pg_catalog.pg_namespace WHERE nspname='%v')", namespaceName)
}

// FDWExistsQuery builds a SELECT EXISTS query for the named foreign data
// wrapper.
func FDWExistsQuery(fdwName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT FROM pg_catalog.pg_foreign_data_wrapper WHERE fdwname='%v')", fdwName)
}

// ForeignServerExistsQuery builds a SELECT EXISTS query for the named
// foreign server.
func ForeignServerExistsQuery(serverName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT FROM pg_catalog.pg_foreign_server WHERE srvname='%v')", serverName)
}

// AssertDataExpectedCount verifies that the named table has exactly
// expectedValue rows.
func AssertDataExpectedCount(
	env *environment.TestingEnvironment,
	tl TableLocator,
	expectedValue int,
) {
	GinkgoHelper()
	By(fmt.Sprintf("verifying test data in table %v (cluster %v, database %v, tablespace %v)",
		tl.TableName, tl.ClusterName, tl.DatabaseName, tl.Tablespace), func() {
		row, err := pgutils.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			tl.Namespace,
			tl.ClusterName,
			tl.DatabaseName,
			apiv1.ApplicationUserSecretSuffix,
			fmt.Sprintf("SELECT COUNT(*) FROM %s", tl.TableName),
		)
		Expect(err).ToNot(HaveOccurred())

		var nRows int
		err = row.Scan(&nRows)
		Expect(err).ToNot(HaveOccurred())
		Expect(nRows).Should(BeEquivalentTo(expectedValue))
	})
}

// AssertLargeObjectValue checks that the large object identified by oid in
// the cluster's app database eventually returns the expected decoded data.
func AssertLargeObjectValue(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, clusterName string,
	oid int,
	data string,
) {
	GinkgoHelper()
	By("verifying large object", func() {
		query := fmt.Sprintf("SELECT encode(lo_get(%v), 'escape');", oid)
		Eventually(func() (string, error) {
			// We keep getting the pod, since there could be a new pod with the same name
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			if err != nil {
				return "", err
			}
			stdout, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryPod.Namespace,
					PodName:   primaryPod.Name,
				},
				pgutils.AppDBName,
				query,
			)
			if err != nil {
				return "", err
			}
			return strings.Trim(stdout, "\n"), nil
		}, testTimeouts[timeouts.LargeObject]).Should(BeEquivalentTo(data))
	})
}

// AssertPgRecoveryMode verifies whether the pod is in recovery mode.
func AssertPgRecoveryMode(env *environment.TestingEnvironment, pod *corev1.Pod, expectedValue bool) {
	GinkgoHelper()
	By(fmt.Sprintf("verifying that postgres recovery mode is %v", expectedValue), func() {
		Eventually(func() (string, error) {
			stdOut, stdErr, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: pod.Namespace,
					PodName:   pod.Name,
				},
				pgutils.PostgresDBName,
				"select pg_catalog.pg_is_in_recovery()",
			)
			if err != nil {
				GinkgoWriter.Printf("stdout: %v\nstderr: %v\n", stdOut, stdErr)
			}
			return strings.Trim(stdOut, "\n"), err
		}, 300, 10).Should(BeEquivalentTo(BoolPGOutput(expectedValue)))
	})
}

// BoolPGOutput translates a Go bool into the textual representation Postgres
// returns from a boolean expression ("t" or "f").
func BoolPGOutput(expectedValue bool) string {
	if expectedValue {
		return "t"
	}
	return "f"
}

// AssertCreationOfTestDataForTargetDB creates a target database with the
// application user as owner, then creates a placeholder table inside it and
// grants SELECT on every public table to pg_monitor.
func AssertCreationOfTestDataForTargetDB(
	env *environment.TestingEnvironment,
	namespace,
	clusterName,
	targetDBName,
	tableName string,
) {
	GinkgoHelper()
	By(fmt.Sprintf("creating target database '%v' and table '%v'", targetDBName, tableName), func() {
		// We need to gather the cluster primary to create the database via superuser
		currentPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		appUser, _, err := secrets.GetCredentials(
			env.Ctx, env.Client,
			clusterName, namespace, apiv1.ApplicationUserSecretSuffix,
		)
		Expect(err).ToNot(HaveOccurred())

		// Create database
		createDBQuery := fmt.Sprintf("CREATE DATABASE %v OWNER %v", targetDBName, appUser)
		_, _, err = exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: currentPrimary.Namespace,
				PodName:   currentPrimary.Name,
			},
			pgutils.PostgresDBName,
			createDBQuery,
		)
		Expect(err).ToNot(HaveOccurred())

		// Open a connection to the newly created database
		forward, conn, err := pgutils.ForwardPSQLConnection(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			clusterName,
			targetDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			_ = conn.Close()
			forward.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		// Create table on target database
		createTableQuery := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %v (id int);", tableName)
		_, err = conn.Exec(createTableQuery)
		Expect(err).ToNot(HaveOccurred())

		// Grant a permission
		grantRoleQuery := "GRANT SELECT ON all tables in schema public to pg_monitor;"
		_, err = conn.Exec(grantRoleQuery)
		Expect(err).ToNot(HaveOccurred())
	})
}

// AssertApplicationDatabaseConnection verifies that the cluster can be
// reached via its read-write service using the application user. When
// appPassword is empty, it is read from appSecretName (or the auto-generated
// <cluster>-app secret if appSecretName is empty).
func AssertApplicationDatabaseConnection(
	env *environment.TestingEnvironment,
	namespace,
	clusterName,
	appUser,
	appDB,
	appPassword,
	appSecretName string,
) {
	GinkgoHelper()
	By("checking cluster can connect with application database user and password", func() {
		// Get the app user password from the auto generated -app secret if appPassword is not provided
		if appPassword == "" {
			if appSecretName == "" {
				appSecretName = clusterName + "-app"
			}
			appSecret := &corev1.Secret{}
			appSecretNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      appSecretName,
			}
			err := env.Client.Get(env.Ctx, appSecretNamespacedName, appSecret)
			Expect(err).ToNot(HaveOccurred())
			appPassword = string(appSecret.Data["password"])
		}
		rwService := services.GetReadWriteServiceName(clusterName)

		AssertConnection(env, namespace, rwService, appDB, appUser, appPassword)
	})
}
