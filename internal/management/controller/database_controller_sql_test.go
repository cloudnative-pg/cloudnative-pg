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
			Owner:     "testowner",
		}

		testError = fmt.Errorf("test error")
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	Context("getDatabaseFDWInfo", func() {
		It("returns info when the fdw exits", func(ctx SpecContext) {
			dbMock.
				ExpectQuery(detectDatabaseFDWSQL).
				WithArgs(fdw.Name).
				WillReturnRows(
					sqlmock.NewRows([]string{"fdwname", "fdwhandler", "fdwvalidator", "fdwowner"}).
						AddRow("testfdw", "testhandler", "testvalidator", "testowner"),
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
					sqlmock.NewRows([]string{"fdwname", "fdwhandler", "fdwvalidator", "fdwowner"}),
				)
			fdwInfo, err := getDatabaseFDWInfo(ctx, db, fdw)
			Expect(err).ToNot(HaveOccurred())
			Expect(fdwInfo).To(BeNil())
		})

	})

	Context("createDatabaseFDW", func() {
		createFDWSQL := "CREATE FOREIGN DATA WRAPPER \"testfdw\" HANDLER \"testhandler\" VALIDATOR \"testvalidator\""

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
