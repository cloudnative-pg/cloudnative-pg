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
		FROM pg_database
		WHERE datname = $1`).WithArgs(database.Spec.Name).WillReturnRows(expectedValue)

			dbExists, err := detectDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
			Expect(dbExists).To(BeTrue())
		})

		It("returns false when a Database is missing", func(ctx SpecContext) {
			expectedValue := sqlmock.NewRows([]string{""}).AddRow("0")
			dbMock.ExpectQuery(`SELECT count(*)
		FROM pg_database
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
				pgx.Identifier{database.Spec.LocaleProvider}.Sanitize(), pgx.Identifier{database.Spec.LcCollate}.Sanitize(),
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
				pgx.Identifier{database.Spec.LocaleProvider}.Sanitize(), pgx.Identifier{database.Spec.BuiltinLocale}.Sanitize(),
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
