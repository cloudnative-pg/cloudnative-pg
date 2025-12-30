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

package controller

import (
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Managed Database SQL", func() {
	var (
		dbMock   sqlmock.Sqlmock
		db       *sql.DB
		database *apiv1.Database
		err      error
	)

	BeforeEach(func() {
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		database = &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name: "db-one",
			},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: "cluster-example",
				},
				Name:  "db-one",
				Owner: "app",
			},
		}
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	Context("detectDatabase", func() {
		It("returns true when it detects an existing Database", func(ctx SpecContext) {
			expectedValue := sqlmock.NewRows([]string{""}).AddRow("1")
			dbMock.ExpectQuery(`SELECT count(*)
		FROM pg_catalog.pg_database
		WHERE datname = $1`).WithArgs(database.Spec.Name).WillReturnRows(expectedValue)

			dbExists, err := detectDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
			Expect(dbExists).To(BeTrue())
		})

		It("returns false when a Database is missing", func(ctx SpecContext) {
			expectedValue := sqlmock.NewRows([]string{""}).AddRow("0")
			dbMock.ExpectQuery(`SELECT count(*)
		FROM pg_catalog.pg_database
		WHERE datname = $1`).WithArgs(database.Spec.Name).WillReturnRows(expectedValue)

			dbExists, err := detectDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
			Expect(dbExists).To(BeFalse())
		})
	})

	Context("createDatabase", func() {
		It("should create a new Database", func(ctx SpecContext) {
			database.Spec.IsTemplate = ptr.To(true)
			database.Spec.Template = "myTemplate"
			database.Spec.Tablespace = "myTablespace"
			database.Spec.AllowConnections = ptr.To(true)
			database.Spec.ConnectionLimit = ptr.To(-1)

			expectedValue := sqlmock.NewResult(0, 1)
			expectedQuery := fmt.Sprintf(
				"CREATE DATABASE %s OWNER %s TEMPLATE %s TABLESPACE %s "+
					"ALLOW_CONNECTIONS %t CONNECTION LIMIT %d IS_TEMPLATE %t",
				pgx.Identifier{database.Spec.Name}.Sanitize(), pgx.Identifier{database.Spec.Owner}.Sanitize(),
				pgx.Identifier{database.Spec.Template}.Sanitize(), pgx.Identifier{database.Spec.Tablespace}.Sanitize(),
				*database.Spec.AllowConnections, *database.Spec.ConnectionLimit, *database.Spec.IsTemplate,
			)
			dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedValue)

			err = createDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should create a new Database with locale and encoding kind fields", func(ctx SpecContext) {
			database.Spec.Locale = "POSIX"
			database.Spec.LocaleProvider = "icu"
			database.Spec.LcCtype = "en_US.utf8"
			database.Spec.LcCollate = "C"
			database.Spec.Encoding = "LATIN1"
			database.Spec.IcuLocale = "en"
			database.Spec.IcuRules = "fr"

			expectedValue := sqlmock.NewResult(0, 1)
			expectedQuery := fmt.Sprintf(
				"CREATE DATABASE %s OWNER %s "+
					"ENCODING %s LOCALE %s LOCALE_PROVIDER %s LC_COLLATE %s LC_CTYPE %s "+
					"ICU_LOCALE %s ICU_RULES %s",
				pgx.Identifier{database.Spec.Name}.Sanitize(), pgx.Identifier{database.Spec.Owner}.Sanitize(),
				pgx.Identifier{database.Spec.Encoding}.Sanitize(), pgx.Identifier{database.Spec.Locale}.Sanitize(),
				pgx.Identifier{database.Spec.LocaleProvider}.Sanitize(),
				pgx.Identifier{database.Spec.LcCollate}.Sanitize(),
				pgx.Identifier{database.Spec.LcCtype}.Sanitize(),
				pgx.Identifier{database.Spec.IcuLocale}.Sanitize(), pgx.Identifier{database.Spec.IcuRules}.Sanitize(),
			)
			dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedValue)

			err = createDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should create a new Database with builtin locale", func(ctx SpecContext) {
			database.Spec.LocaleProvider = "builtin"
			database.Spec.BuiltinLocale = "C"
			database.Spec.CollationVersion = "1.2.3"

			expectedValue := sqlmock.NewResult(0, 1)
			expectedQuery := fmt.Sprintf(
				"CREATE DATABASE %s OWNER %s "+
					"LOCALE_PROVIDER %s BUILTIN_LOCALE %s COLLATION_VERSION %s",
				pgx.Identifier{database.Spec.Name}.Sanitize(), pgx.Identifier{database.Spec.Owner}.Sanitize(),
				pgx.Identifier{database.Spec.LocaleProvider}.Sanitize(),
				pgx.Identifier{database.Spec.BuiltinLocale}.Sanitize(),
				pgx.Identifier{database.Spec.CollationVersion}.Sanitize(),
			)
			dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedValue)

			err = createDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("updateDatabase", func() {
		It("should reconcile an existing Database", func(ctx SpecContext) {
			database.Spec.Owner = "newOwner"
			database.Spec.IsTemplate = ptr.To(true)
			database.Spec.AllowConnections = ptr.To(true)
			database.Spec.ConnectionLimit = ptr.To(-1)
			database.Spec.Tablespace = "newTablespace"

			expectedValue := sqlmock.NewResult(0, 1)

			// Mock AllowConnections DDL
			allowConnectionsExpectedQuery := fmt.Sprintf(
				"ALTER DATABASE %s WITH ALLOW_CONNECTIONS %v",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
				*database.Spec.AllowConnections,
			)
			dbMock.ExpectExec(allowConnectionsExpectedQuery).WillReturnResult(expectedValue)

			// Mock ConnectionLimit DDL
			connectionLimitExpectedQuery := fmt.Sprintf(
				"ALTER DATABASE %s WITH CONNECTION LIMIT %v",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
				*database.Spec.ConnectionLimit,
			)
			dbMock.ExpectExec(connectionLimitExpectedQuery).WillReturnResult(expectedValue)

			// Mock IsTemplate DDL
			isTemplateExpectedQuery := fmt.Sprintf(
				"ALTER DATABASE %s WITH IS_TEMPLATE %v",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
				*database.Spec.IsTemplate,
			)
			dbMock.ExpectExec(isTemplateExpectedQuery).WillReturnResult(expectedValue)

			// Mock Owner DDL
			ownerExpectedQuery := fmt.Sprintf(
				"ALTER DATABASE %s OWNER TO %s",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
				pgx.Identifier{database.Spec.Owner}.Sanitize(),
			)
			dbMock.ExpectExec(ownerExpectedQuery).WillReturnResult(expectedValue)

			// Mock Tablespace DDL
			tablespaceExpectedQuery := fmt.Sprintf(
				"ALTER DATABASE %s SET TABLESPACE %s",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
				pgx.Identifier{database.Spec.Tablespace}.Sanitize(),
			)
			dbMock.ExpectExec(tablespaceExpectedQuery).WillReturnResult(expectedValue)

			err = updateDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("dropDatabase", func() {
		It("should drop an existing Database", func(ctx SpecContext) {
			expectedValue := sqlmock.NewResult(0, 1)
			expectedQuery := fmt.Sprintf(
				"DROP DATABASE IF EXISTS %s",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
			)
			dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedValue)

			err = dropDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("Managed Extensions SQL", func() {
	var (
		dbMock sqlmock.Sqlmock
		db     *sql.DB
		ext    apiv1.ExtensionSpec
		err    error

		testError error
	)

	BeforeEach(func() {
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		ext = apiv1.ExtensionSpec{
			DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
				Name:   "testext",
				Ensure: "present",
			},
			Version: "1.0",
			Schema:  "default",
		}

		testError = fmt.Errorf("test error")
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	Context("getDatabaseExtensionInfo", func() {
		It("returns info when the extension exists", func(ctx SpecContext) {
			dbMock.
				ExpectQuery(detectDatabaseExtensionSQL).
				WithArgs(ext.Name).
				WillReturnRows(
					sqlmock.NewRows([]string{"extname", "extversion", "nspname"}).
						AddRow("testext", "1.0", "default"),
				)
			extInfo, err := getDatabaseExtensionInfo(ctx, db, ext)
			Expect(err).ToNot(HaveOccurred())
			Expect(extInfo).ToNot(BeNil())
			Expect(extInfo.Name).To(Equal("testext"))
			Expect(extInfo.Schema).To(Equal("default"))
			Expect(extInfo.Version).To(Equal("1.0"))
		})

		It("returns nil info when the extension does not exist", func(ctx SpecContext) {
			dbMock.
				ExpectQuery(detectDatabaseExtensionSQL).
				WithArgs(ext.Name).
				WillReturnRows(
					sqlmock.NewRows([]string{"extname", "extversion", "nspname"}),
				)
			extInfo, err := getDatabaseExtensionInfo(ctx, db, ext)
			Expect(err).ToNot(HaveOccurred())
			Expect(extInfo).To(BeNil())
		})
	})

	Context("createDatabaseExtension", func() {
		createExtensionSQL := "CREATE EXTENSION \"testext\" VERSION \"1.0\" SCHEMA \"default\""

		It("returns success when the extension has been created", func(ctx SpecContext) {
			dbMock.
				ExpectExec(createExtensionSQL).
				WillReturnResult(sqlmock.NewResult(0, 1))
			Expect(createDatabaseExtension(ctx, db, ext)).Error().NotTo(HaveOccurred())
		})

		It("fails when the extension could not be created", func(ctx SpecContext) {
			dbMock.
				ExpectExec(createExtensionSQL).
				WillReturnError(testError)
			Expect(createDatabaseExtension(ctx, db, ext)).Error().To(Equal(testError))
		})
	})

	Context("dropDatabaseExtension", func() {
		dropExtensionSQL := "DROP EXTENSION IF EXISTS \"testext\""

		It("returns success when the extension has been dropped", func(ctx SpecContext) {
			dbMock.
				ExpectExec(dropExtensionSQL).
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(dropDatabaseExtension(ctx, db, ext)).Error().NotTo(HaveOccurred())
		})

		It("returns an error when the DROP statement failed", func(ctx SpecContext) {
			dbMock.
				ExpectExec(dropExtensionSQL).
				WillReturnError(testError)

			Expect(dropDatabaseExtension(ctx, db, ext)).Error().To(Equal(testError))
		})
	})

	Context("updateDatabaseExtension", func() {
		It("does nothing when the extension is already at the correct version", func(ctx SpecContext) {
			Expect(updateDatabaseExtension(ctx, db, ext, &extInfo{
				Name:    ext.Name,
				Version: ext.Version,
				Schema:  ext.Schema,
			})).Error().NotTo(HaveOccurred())
		})

		It("updates the extension version", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER EXTENSION \"testext\" UPDATE TO \"1.0\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseExtension(ctx, db, ext,
				&extInfo{Name: ext.Name, Version: "0.9", Schema: ext.Schema})).Error().NotTo(HaveOccurred())
		})

		It("updates the schema", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER EXTENSION \"testext\" SET SCHEMA \"default\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseExtension(ctx, db, ext,
				&extInfo{Name: ext.Name, Version: ext.Version, Schema: "old"})).Error().NotTo(HaveOccurred())
		})

		It("sets the schema and the extension version", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER EXTENSION \"testext\" SET SCHEMA \"default\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("ALTER EXTENSION \"testext\" UPDATE TO \"1.0\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseExtension(ctx, db, ext, &extInfo{
				Name: ext.Name, Version: "0.9",
				Schema: "old",
			})).Error().NotTo(HaveOccurred())
		})

		It("fail when setting the schema failed", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER EXTENSION \"testext\" SET SCHEMA \"default\"").
				WillReturnError(testError)

			Expect(updateDatabaseExtension(ctx, db, ext,
				&extInfo{Name: ext.Name, Version: ext.Version, Schema: "old"})).Error().To(MatchError(testError))
		})

		It("fail when setting the version failed", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER EXTENSION \"testext\" SET SCHEMA \"default\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("ALTER EXTENSION \"testext\" UPDATE TO \"1.0\"").
				WillReturnError(testError)

			Expect(updateDatabaseExtension(ctx, db, ext, &extInfo{
				Name: ext.Name, Version: "0.9",
				Schema: "old",
			})).Error().To(MatchError(testError))
		})
	})
})

var _ = Describe("Managed schema SQL", func() {
	var (
		dbMock sqlmock.Sqlmock
		db     *sql.DB
		schema apiv1.SchemaSpec
		err    error

		testError error
	)

	BeforeEach(func() {
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		schema = apiv1.SchemaSpec{
			DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
				Name:   "testschema",
				Ensure: "present",
			},
			Owner: "owner",
		}

		testError = fmt.Errorf("test error")
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	Context("getDatabaseSchemaInfo", func() {
		It("returns info when the extension exits", func(ctx SpecContext) {
			dbMock.
				ExpectQuery(detectDatabaseSchemaSQL).
				WithArgs(schema.Name).
				WillReturnRows(
					sqlmock.NewRows([]string{"name", "owner"}).
						AddRow("name", "owner"),
				)
			schemaInfo, err := getDatabaseSchemaInfo(ctx, db, schema)
			Expect(err).ToNot(HaveOccurred())
			Expect(schemaInfo).ToNot(BeNil())
			Expect(schemaInfo.Name).To(Equal("name"))
			Expect(schemaInfo.Owner).To(Equal("owner"))
		})

		It("returns nil info when the extension does not exist", func(ctx SpecContext) {
			dbMock.
				ExpectQuery(detectDatabaseSchemaSQL).
				WithArgs(schema.Name).
				WillReturnRows(
					sqlmock.NewRows([]string{"name", "owner"}),
				)
			schemaInfo, err := getDatabaseSchemaInfo(ctx, db, schema)
			Expect(err).ToNot(HaveOccurred())
			Expect(schemaInfo).To(BeNil())
		})
	})

	Context("createDatabaseSchema", func() {
		createSchemaSQL := "CREATE SCHEMA \"testschema\" AUTHORIZATION \"owner\""

		It("returns success when the schema has been created", func(ctx SpecContext) {
			dbMock.
				ExpectExec(createSchemaSQL).
				WillReturnResult(sqlmock.NewResult(0, 1))
			Expect(createDatabaseSchema(ctx, db, schema)).Error().NotTo(HaveOccurred())
		})

		It("fails when the schema has not been created", func(ctx SpecContext) {
			dbMock.
				ExpectExec(createSchemaSQL).
				WillReturnError(testError)
			Expect(createDatabaseSchema(ctx, db, schema)).Error().To(Equal(testError))
		})
	})

	Context("updateDatabaseSchema", func() {
		It("does nothing when the schema has been correctly reconciled", func(ctx SpecContext) {
			Expect(updateDatabaseSchema(ctx, db, schema, &schemaInfo{
				Name:  schema.Name,
				Owner: schema.Owner,
			})).Error().NotTo(HaveOccurred())
		})

		It("updates the schema owner", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER SCHEMA \"testschema\" OWNER TO \"owner\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseSchema(ctx, db, schema,
				&schemaInfo{Name: schema.Name, Owner: "old"})).Error().NotTo(HaveOccurred())
		})

		It("fail when setting the owner failed", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER SCHEMA \"testschema\" OWNER TO \"owner\"").
				WillReturnError(testError)

			Expect(updateDatabaseSchema(ctx, db, schema,
				&schemaInfo{Name: schema.Name, Owner: "old"})).Error().To(MatchError(testError))
		})
	})

	Context("dropDatabaseSchema", func() {
		dropSchemaSQL := "DROP SCHEMA IF EXISTS \"testschema\""

		It("returns success when the extension has been dropped", func(ctx SpecContext) {
			dbMock.
				ExpectExec(dropSchemaSQL).
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(dropDatabaseSchema(ctx, db, schema)).Error().NotTo(HaveOccurred())
		})

		It("returns an error when the DROP statement failed", func(ctx SpecContext) {
			dbMock.
				ExpectExec(dropSchemaSQL).
				WillReturnError(testError)

			Expect(dropDatabaseSchema(ctx, db, schema)).Error().To(Equal(testError))
		})
	})

	Context("createDatabaseSchema with privileges", func() {
		It("creates schema and grants CREATE privilege", func(ctx SpecContext) {
			schemaWithPrivileges := apiv1.SchemaSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testschema",
					Ensure: "present",
				},
				Owner: "owner",
				Create: []apiv1.UsageSpec{
					{Name: "appuser", Type: apiv1.GrantUsageSpecType},
				},
			}

			dbMock.
				ExpectExec("CREATE SCHEMA \"testschema\"  AUTHORIZATION \"owner\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("GRANT CREATE ON SCHEMA \"testschema\" TO \"appuser\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(createDatabaseSchema(ctx, db, schemaWithPrivileges)).Error().NotTo(HaveOccurred())
		})

		It("creates schema and grants USAGE privilege", func(ctx SpecContext) {
			schemaWithPrivileges := apiv1.SchemaSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testschema",
					Ensure: "present",
				},
				Owner: "owner",
				Usage: []apiv1.UsageSpec{
					{Name: "appuser", Type: apiv1.GrantUsageSpecType},
				},
			}

			dbMock.
				ExpectExec("CREATE SCHEMA \"testschema\"  AUTHORIZATION \"owner\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("GRANT USAGE ON SCHEMA \"testschema\" TO \"appuser\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(createDatabaseSchema(ctx, db, schemaWithPrivileges)).Error().NotTo(HaveOccurred())
		})

		It("creates schema and applies both CREATE and USAGE privileges", func(ctx SpecContext) {
			schemaWithPrivileges := apiv1.SchemaSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testschema",
					Ensure: "present",
				},
				Owner: "owner",
				Create: []apiv1.UsageSpec{
					{Name: "developer", Type: apiv1.GrantUsageSpecType},
				},
				Usage: []apiv1.UsageSpec{
					{Name: "reader", Type: apiv1.GrantUsageSpecType},
				},
			}

			dbMock.
				ExpectExec("CREATE SCHEMA \"testschema\"  AUTHORIZATION \"owner\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("GRANT CREATE ON SCHEMA \"testschema\" TO \"developer\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("GRANT USAGE ON SCHEMA \"testschema\" TO \"reader\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(createDatabaseSchema(ctx, db, schemaWithPrivileges)).Error().NotTo(HaveOccurred())
		})
	})

	Context("updateDatabaseSchema with privileges", func() {
		It("updates schema privileges with GRANT CREATE", func(ctx SpecContext) {
			schemaWithPrivileges := apiv1.SchemaSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testschema",
					Ensure: "present",
				},
				Owner: "owner",
				Create: []apiv1.UsageSpec{
					{Name: "appuser", Type: apiv1.GrantUsageSpecType},
				},
			}

			dbMock.
				ExpectExec("GRANT CREATE ON SCHEMA \"testschema\" TO \"appuser\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseSchema(ctx, db, schemaWithPrivileges,
				&schemaInfo{Name: schemaWithPrivileges.Name, Owner: schemaWithPrivileges.Owner})).Error().NotTo(HaveOccurred())
		})

		It("updates schema privileges with REVOKE CREATE", func(ctx SpecContext) {
			schemaWithPrivileges := apiv1.SchemaSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testschema",
					Ensure: "present",
				},
				Owner: "owner",
				Create: []apiv1.UsageSpec{
					{Name: "appuser", Type: apiv1.RevokeUsageSpecType},
				},
			}

			dbMock.
				ExpectExec("REVOKE CREATE ON SCHEMA \"testschema\" FROM \"appuser\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseSchema(ctx, db, schemaWithPrivileges,
				&schemaInfo{Name: schemaWithPrivileges.Name, Owner: schemaWithPrivileges.Owner})).Error().NotTo(HaveOccurred())
		})

		It("updates schema privileges with GRANT USAGE", func(ctx SpecContext) {
			schemaWithPrivileges := apiv1.SchemaSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testschema",
					Ensure: "present",
				},
				Owner: "owner",
				Usage: []apiv1.UsageSpec{
					{Name: "reader", Type: apiv1.GrantUsageSpecType},
				},
			}

			dbMock.
				ExpectExec("GRANT USAGE ON SCHEMA \"testschema\" TO \"reader\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseSchema(ctx, db, schemaWithPrivileges,
				&schemaInfo{Name: schemaWithPrivileges.Name, Owner: schemaWithPrivileges.Owner})).Error().NotTo(HaveOccurred())
		})

		It("updates schema privileges with REVOKE USAGE", func(ctx SpecContext) {
			schemaWithPrivileges := apiv1.SchemaSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testschema",
					Ensure: "present",
				},
				Owner: "owner",
				Usage: []apiv1.UsageSpec{
					{Name: "reader", Type: apiv1.RevokeUsageSpecType},
				},
			}

			dbMock.
				ExpectExec("REVOKE USAGE ON SCHEMA \"testschema\" FROM \"reader\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseSchema(ctx, db, schemaWithPrivileges,
				&schemaInfo{Name: schemaWithPrivileges.Name, Owner: schemaWithPrivileges.Owner})).Error().NotTo(HaveOccurred())
		})

		It("updates owner and applies privileges", func(ctx SpecContext) {
			schemaWithPrivileges := apiv1.SchemaSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testschema",
					Ensure: "present",
				},
				Owner: "newowner",
				Create: []apiv1.UsageSpec{
					{Name: "developer", Type: apiv1.GrantUsageSpecType},
				},
				Usage: []apiv1.UsageSpec{
					{Name: "reader", Type: apiv1.GrantUsageSpecType},
				},
			}

			dbMock.
				ExpectExec("ALTER SCHEMA \"testschema\" OWNER TO \"newowner\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("GRANT CREATE ON SCHEMA \"testschema\" TO \"developer\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("GRANT USAGE ON SCHEMA \"testschema\" TO \"reader\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseSchema(ctx, db, schemaWithPrivileges,
				&schemaInfo{Name: schemaWithPrivileges.Name, Owner: "oldowner"})).Error().NotTo(HaveOccurred())
		})

		It("fails when GRANT CREATE fails", func(ctx SpecContext) {
			schemaWithPrivileges := apiv1.SchemaSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testschema",
					Ensure: "present",
				},
				Owner: "owner",
				Create: []apiv1.UsageSpec{
					{Name: "appuser", Type: apiv1.GrantUsageSpecType},
				},
			}

			dbMock.
				ExpectExec("GRANT CREATE ON SCHEMA \"testschema\" TO \"appuser\"").
				WillReturnError(testError)

			Expect(updateDatabaseSchema(ctx, db, schemaWithPrivileges,
				&schemaInfo{Name: schemaWithPrivileges.Name, Owner: schemaWithPrivileges.Owner})).Error().To(MatchError(testError))
		})

		It("applies multiple privileges to multiple roles", func(ctx SpecContext) {
			schemaWithPrivileges := apiv1.SchemaSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testschema",
					Ensure: "present",
				},
				Owner: "owner",
				Create: []apiv1.UsageSpec{
					{Name: "developer1", Type: apiv1.GrantUsageSpecType},
					{Name: "developer2", Type: apiv1.GrantUsageSpecType},
					{Name: "revoked_dev", Type: apiv1.RevokeUsageSpecType},
				},
				Usage: []apiv1.UsageSpec{
					{Name: "reader1", Type: apiv1.GrantUsageSpecType},
					{Name: "reader2", Type: apiv1.RevokeUsageSpecType},
				},
			}

			dbMock.
				ExpectExec("GRANT CREATE ON SCHEMA \"testschema\" TO \"developer1\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("GRANT CREATE ON SCHEMA \"testschema\" TO \"developer2\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("REVOKE CREATE ON SCHEMA \"testschema\" FROM \"revoked_dev\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("GRANT USAGE ON SCHEMA \"testschema\" TO \"reader1\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.
				ExpectExec("REVOKE USAGE ON SCHEMA \"testschema\" FROM \"reader2\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseSchema(ctx, db, schemaWithPrivileges,
				&schemaInfo{Name: schemaWithPrivileges.Name, Owner: schemaWithPrivileges.Owner})).Error().NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("Managed Foreign Data Wrapper SQL", func() {
	var (
		dbMock sqlmock.Sqlmock
		db     *sql.DB
		fdw    apiv1.FDWSpec
		err    error

		testError error
	)

	BeforeEach(func() {
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		fdw = apiv1.FDWSpec{
			DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
				Name:   "testfdw",
				Ensure: "present",
			},
			Handler:   "testhandler",
			Validator: "testvalidator",
			Options: []apiv1.OptionSpec{
				{
					Name:  "testoption",
					Value: "testvalue",
				},
				{
					Name:  "testoption2",
					Value: "testvalue2",
				},
			},
			Owner: "owner",
		}

		testError = fmt.Errorf("test error")
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	Context("getDatabaseFDWInfo", func() {
		It("returns info when the fdw exists", func(ctx SpecContext) {
			dbMock.
				ExpectQuery(detectDatabaseFDWSQL).
				WithArgs(fdw.Name).
				WillReturnRows(
					sqlmock.NewRows([]string{"fdwname", "fdwhandler", "fdwvalidator", "options", "fdwowner"}).
						AddRow("testfdw", "testhandler", "testvalidator", nil, "testowner"),
				)
			fdwInfo, err := getDatabaseFDWInfo(ctx, db, fdw)
			Expect(err).ToNot(HaveOccurred())
			Expect(fdwInfo).ToNot(BeNil())
			Expect(fdwInfo.Name).To(Equal("testfdw"))
			Expect(fdwInfo.Handler).To(Equal("testhandler"))
			Expect(fdwInfo.Validator).To(Equal("testvalidator"))
			Expect(fdwInfo.Owner).To(Equal("testowner"))
		})

		It("returns nil info when the fdw does not exist", func(ctx SpecContext) {
			dbMock.
				ExpectQuery(detectDatabaseFDWSQL).
				WithArgs(fdw.Name).
				WillReturnRows(
					sqlmock.NewRows([]string{"fdwname", "fdwhandler", "fdwvalidator", "options", "fdwowner"}),
				)
			fdwInfo, err := getDatabaseFDWInfo(ctx, db, fdw)
			Expect(err).ToNot(HaveOccurred())
			Expect(fdwInfo).To(BeNil())
		})
	})

	Context("createDatabaseFDW", func() {
		createFDWSQL := "CREATE FOREIGN DATA WRAPPER \"testfdw\" HANDLER \"testhandler\" " +
			"VALIDATOR \"testvalidator\" OPTIONS (\"testoption\" 'testvalue', \"testoption2\" 'testvalue2')"

		It("returns success when the fdw has been created", func(ctx SpecContext) {
			dbMock.
				ExpectExec(createFDWSQL).
				WillReturnResult(sqlmock.NewResult(0, 1))
			Expect(createDatabaseFDW(ctx, db, fdw)).Error().NotTo(HaveOccurred())
		})

		It("fails when the fdw could not be created", func(ctx SpecContext) {
			dbMock.
				ExpectExec(createFDWSQL).
				WillReturnError(testError)
			Expect(createDatabaseFDW(ctx, db, fdw)).Error().To(Equal(testError))
		})

		It("success with NO HANDLER and NO VALIDATOR", func(ctx SpecContext) {
			dbMock.
				ExpectExec("CREATE FOREIGN DATA WRAPPER \"testfdw\" NO HANDLER NO VALIDATOR").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(createDatabaseFDW(ctx, db, apiv1.FDWSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   "testfdw",
					Ensure: "present",
				},
				Handler:   "-",
				Validator: "-",
				Owner:     "owner",
			})).Error().NotTo(HaveOccurred())
		})
	})

	Context("updateDatabaseFDW", func() {
		It("does nothing when the fdw has been correctly reconciled", func(ctx SpecContext) {
			Expect(updateDatabaseFDW(ctx, db, fdw, &fdwInfo{
				Name:      fdw.Name,
				Handler:   fdw.Handler,
				Validator: fdw.Validator,
				Owner:     fdw.Owner,
			})).Error().NotTo(HaveOccurred())
		})

		It("updates the fdw handler", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER FOREIGN DATA WRAPPER \"testfdw\" HANDLER \"testhandler\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseFDW(ctx, db, fdw,
				&fdwInfo{Name: fdw.Name, Handler: "oldhandler", Validator: fdw.Validator, Owner: fdw.Owner})).
				Error().NotTo(HaveOccurred())
		})

		It("handles removal of handler when not specified", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER FOREIGN DATA WRAPPER \"testfdw\" NO HANDLER").
				WillReturnResult(sqlmock.NewResult(0, 1))

			fdw.Handler = "-"
			Expect(updateDatabaseFDW(ctx, db, fdw,
				&fdwInfo{Name: fdw.Name, Handler: "oldhandler", Validator: fdw.Validator, Owner: fdw.Owner})).
				Error().NotTo(HaveOccurred())
		})

		It("fail when setting the handler failed", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER FOREIGN DATA WRAPPER \"testfdw\" HANDLER \"testhandler\"").
				WillReturnError(testError)

			Expect(updateDatabaseFDW(ctx, db, fdw,
				&fdwInfo{Name: fdw.Name, Handler: "oldhandler", Validator: fdw.Validator, Owner: fdw.Owner})).
				Error().To(MatchError(testError))
		})

		It("updates the fdw validator", func(ctx SpecContext) {
			dbMock.ExpectExec(
				"ALTER FOREIGN DATA WRAPPER \"testfdw\" VALIDATOR \"testvalidator\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseFDW(ctx, db, fdw,
				&fdwInfo{Name: fdw.Name, Handler: fdw.Handler, Validator: "oldvalidator", Owner: fdw.Owner})).
				Error().NotTo(HaveOccurred())
		})

		It("handles removal of validator when not specified", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER FOREIGN DATA WRAPPER \"testfdw\" NO VALIDATOR").
				WillReturnResult(sqlmock.NewResult(0, 1))

			fdw.Validator = "-"
			Expect(updateDatabaseFDW(ctx, db, fdw,
				&fdwInfo{Name: fdw.Name, Handler: fdw.Handler, Validator: "oldvalidator", Owner: fdw.Owner})).
				Error().NotTo(HaveOccurred())
		})

		It("fail when setting the validator failed", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER FOREIGN DATA WRAPPER \"testfdw\" VALIDATOR \"testvalidator\"").
				WillReturnError(testError)

			Expect(updateDatabaseFDW(ctx, db, fdw,
				&fdwInfo{Name: fdw.Name, Handler: fdw.Handler, Validator: "oldvalidator", Owner: fdw.Owner})).
				Error().To(MatchError(testError))
		})

		It("add new fdw options", func(ctx SpecContext) {
			fdw.Options = []apiv1.OptionSpec{
				{
					Name:   "add_option",
					Value:  "value",
					Ensure: apiv1.EnsurePresent,
				},
			}
			info := &fdwInfo{
				Name:      fdw.Name,
				Handler:   fdw.Handler,
				Validator: fdw.Validator,
				Options: map[string]string{
					"modify_option": "old_value",
					"remove_option": "value",
				},
				Owner: fdw.Owner,
			}

			expectedSQL := "ALTER FOREIGN DATA WRAPPER \"testfdw\" OPTIONS (ADD \"add_option\" 'value')"
			dbMock.ExpectExec(expectedSQL).WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseFDW(ctx, db, fdw, info)).Error().NotTo(HaveOccurred())
		})

		It("modify the fdw options", func(ctx SpecContext) {
			fdw.Options = []apiv1.OptionSpec{
				{
					Name:   "modify_option",
					Value:  "new_value",
					Ensure: apiv1.EnsurePresent,
				},
			}
			info := &fdwInfo{
				Name:      fdw.Name,
				Handler:   fdw.Handler,
				Validator: fdw.Validator,
				Options: map[string]string{
					"modify_option": "old_value",
					"remove_option": "value",
				},
				Owner: fdw.Owner,
			}

			expectedSQL := "ALTER FOREIGN DATA WRAPPER \"testfdw\" OPTIONS (SET \"modify_option\" 'new_value')"
			dbMock.ExpectExec(expectedSQL).WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseFDW(ctx, db, fdw, info)).Error().NotTo(HaveOccurred())
		})

		It("remove new fdw options", func(ctx SpecContext) {
			fdw.Options = []apiv1.OptionSpec{
				{
					Name:   "remove_option",
					Value:  "value",
					Ensure: apiv1.EnsureAbsent,
				},
			}
			info := &fdwInfo{
				Name:      fdw.Name,
				Handler:   fdw.Handler,
				Validator: fdw.Validator,
				Options: map[string]string{
					"modify_option": "old_value",
					"remove_option": "value",
				},
				Owner: fdw.Owner,
			}

			expectedSQL := "ALTER FOREIGN DATA WRAPPER \"testfdw\" OPTIONS (DROP \"remove_option\")"
			dbMock.ExpectExec(expectedSQL).WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseFDW(ctx, db, fdw, info)).Error().NotTo(HaveOccurred())
		})

		It("updates the fdw owner", func(ctx SpecContext) {
			dbMock.ExpectExec(
				"ALTER FOREIGN DATA WRAPPER \"testfdw\" OWNER TO \"owner\"").
				WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseFDW(ctx, db, fdw,
				&fdwInfo{Name: fdw.Name, Handler: fdw.Handler, Validator: fdw.Validator, Owner: "oldowner"})).
				Error().NotTo(HaveOccurred())
		})

		It("fail when setting the owner failed", func(ctx SpecContext) {
			dbMock.
				ExpectExec("ALTER FOREIGN DATA WRAPPER \"testfdw\" OWNER TO \"owner\"").
				WillReturnError(testError)

			Expect(updateDatabaseFDW(ctx, db, fdw,
				&fdwInfo{Name: fdw.Name, Handler: fdw.Handler, Validator: fdw.Validator, Owner: "old"})).
				Error().To(MatchError(testError))
		})

		It("updates the usages permissions of the fdw", func(ctx SpecContext) {
			dbMock.ExpectExec(
				"GRANT USAGE ON FOREIGN DATA WRAPPER \"testfdw\" TO \"owner\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			fdw.Usages = []apiv1.UsageSpec{
				{
					Name: "owner",
					Type: "grant",
				},
			}
			Expect(updateDatabaseFDWUsage(ctx, db, &fdw)).Error().NotTo(HaveOccurred())
		})
	})

	Context("dropDatabaseFDW", func() {
		dropFDWSQL := "DROP FOREIGN DATA WRAPPER IF EXISTS \"testfdw\""

		It("returns success when the foreign data wrapper has been dropped", func(ctx SpecContext) {
			dbMock.
				ExpectExec(dropFDWSQL).
				WillReturnResult(sqlmock.NewResult(0, 1))
			Expect(dropDatabaseFDW(ctx, db, fdw)).Error().NotTo(HaveOccurred())
		})

		It("returns an error when the DROP statement failed", func(ctx SpecContext) {
			dbMock.
				ExpectExec(dropFDWSQL).
				WillReturnError(testError)

			Expect(dropDatabaseFDW(ctx, db, fdw)).Error().To(Equal(testError))
		})
	})
})

var _ = Describe("Managed Foreign Server SQL", func() {
	var (
		dbMock sqlmock.Sqlmock
		db     *sql.DB
		server apiv1.ServerSpec
		err    error

		testError error
	)

	BeforeEach(func() {
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		server = apiv1.ServerSpec{
			DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
				Name:   "testserver",
				Ensure: "present",
			},
			FdwName: "testfdw",
			Options: []apiv1.OptionSpec{
				{Name: "host", Value: "localhost"},
				{Name: "port", Value: "5432"},
			},
		}

		testError = fmt.Errorf("test error")
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	Context("getDatabaseForeignServerInfo", func() {
		It("returns info when the foreign server exists", func(ctx SpecContext) {
			dbMock.
				ExpectQuery(detectDatabaseForeignServerSQL).
				WithArgs(server.Name).
				WillReturnRows(
					sqlmock.NewRows([]string{"servername", "fdwname", "options"}).
						AddRow("testserver", "testfdw", nil),
				)
			serverInfo, err := getDatabaseForeignServerInfo(ctx, db, server)
			Expect(err).ToNot(HaveOccurred())
			Expect(serverInfo).ToNot(BeNil())
			Expect(serverInfo.Name).To(Equal("testserver"))
			Expect(serverInfo.FDWName).To(Equal("testfdw"))
		})

		It("returns nil info when the foreign server does not exiest", func(ctx SpecContext) {
			dbMock.
				ExpectQuery(detectDatabaseForeignServerSQL).
				WithArgs(server.Name).
				WillReturnRows(
					sqlmock.NewRows([]string{"servername", "fdwname"}),
				)
			serverInfo, err := getDatabaseForeignServerInfo(ctx, db, server)
			Expect(err).ToNot(HaveOccurred())
			Expect(serverInfo).To(BeNil())
		})
	})

	Context("createDatabaseForeignServer", func() {
		createForeignServerSQL := "CREATE SERVER \"testserver\" FOREIGN DATA WRAPPER \"testfdw\"" +
			" OPTIONS (\"host\" 'localhost', \"port\" '5432')"

		It("returns success when the foreign server has been created", func(ctx SpecContext) {
			dbMock.
				ExpectExec(createForeignServerSQL).
				WillReturnResult(sqlmock.NewResult(0, 1))
			Expect(createDatabaseForeignServer(ctx, db, server)).Error().NotTo(HaveOccurred())
		})

		It("fails when the foreign server could not be created", func(ctx SpecContext) {
			dbMock.
				ExpectExec(createForeignServerSQL).
				WillReturnError(testError)
			Expect(createDatabaseForeignServer(ctx, db, server)).Error().To(Equal(testError))
		})
	})

	Context("updateDatabaseForeignServer", func() {
		It("does nothing when the foreign server does not exist", func(ctx SpecContext) {
			Expect(updateDatabaseForeignServer(ctx, db, server, &serverInfo{
				Name:    server.Name,
				FDWName: server.FdwName,
			})).Error().NotTo(HaveOccurred())
		})

		It("add new foreign server options", func(ctx SpecContext) {
			server.Options = []apiv1.OptionSpec{
				{
					Name:   "add_option",
					Value:  "value",
					Ensure: apiv1.EnsurePresent,
				},
			}
			info := &serverInfo{
				Name:    server.Name,
				FDWName: server.FdwName,
				Options: map[string]string{
					"modify_option": "old_value",
					"remove_option": "value",
				},
			}

			expectedSQL := "ALTER SERVER \"testserver\" OPTIONS (ADD \"add_option\" 'value')"
			dbMock.ExpectExec(expectedSQL).WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseForeignServer(ctx, db, server, info)).Error().NotTo(HaveOccurred())
		})

		It("modify the foreign server options", func(ctx SpecContext) {
			server.Options = []apiv1.OptionSpec{
				{
					Name:   "modify_option",
					Value:  "new_value",
					Ensure: apiv1.EnsurePresent,
				},
			}
			info := &serverInfo{
				Name:    server.Name,
				FDWName: server.FdwName,
				Options: map[string]string{
					"modify_option": "old_value",
					"remove_option": "value",
				},
			}

			expectedSQL := "ALTER SERVER \"testserver\" OPTIONS (SET \"modify_option\" 'new_value')"
			dbMock.ExpectExec(expectedSQL).WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseForeignServer(ctx, db, server, info)).Error().NotTo(HaveOccurred())
		})

		It("updates the usages permissions of the foreign server", func(ctx SpecContext) {
			dbMock.ExpectExec(
				"GRANT USAGE ON FOREIGN SERVER \"testserver\" TO \"owner\"").
				WillReturnResult(sqlmock.NewResult(0, 1))
			server.Usages = []apiv1.UsageSpec{
				{
					Name: "owner",
					Type: "grant",
				},
			}
			Expect(updateDatabaseForeignServerUsage(ctx, db, &server)).Error().NotTo(HaveOccurred())
		})

		It("remove foreign server options", func(ctx SpecContext) {
			server.Options = []apiv1.OptionSpec{
				{
					Name:   "remove_option",
					Value:  "value",
					Ensure: apiv1.EnsureAbsent,
				},
			}
			info := &serverInfo{
				Name:    server.Name,
				FDWName: server.FdwName,
				Options: map[string]string{
					"modify_option": "old_value",
					"remove_option": "value",
				},
			}

			expectedSQL := "ALTER SERVER \"testserver\" OPTIONS (DROP \"remove_option\")"
			dbMock.ExpectExec(expectedSQL).WillReturnResult(sqlmock.NewResult(0, 1))

			Expect(updateDatabaseForeignServer(ctx, db, server, info)).Error().NotTo(HaveOccurred())
		})
	})

	Context("dropDatabaseForeignServer", func() {
		dropForeignServerSQL := "DROP SERVER IF EXISTS \"testserver\""

		It("returns success when the foreign server has been dropped", func(ctx SpecContext) {
			dbMock.
				ExpectExec(dropForeignServerSQL).
				WillReturnResult(sqlmock.NewResult(0, 1))
			Expect(dropDatabaseForeignServer(ctx, db, server)).Error().NotTo(HaveOccurred())
		})

		It("returns an error when the DROP statement failed", func(ctx SpecContext) {
			dbMock.
				ExpectExec(dropForeignServerSQL).
				WillReturnError(testError)

			Expect(dropDatabaseForeignServer(ctx, db, server)).Error().To(Equal(testError))
		})
	})
})
