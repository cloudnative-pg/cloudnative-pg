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

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("setupMetricsExporterRole", func() {
	const (
		existsQuery = `SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = $1`
		attrsQuery  = `SELECT rolinherit AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
		        AND NOT rolreplication AND NOT rolbypassrls AND rolcanlogin,
		        pg_catalog.pg_has_role(rolname, 'pg_monitor', 'member'),
		        rolpassword IS NULL
		 FROM pg_catalog.pg_authid WHERE rolname = $1`
		createSQL = `CREATE ROLE cnpg_metrics_exporter WITH LOGIN PASSWORD NULL`
		alterSQL  = "ALTER ROLE cnpg_metrics_exporter WITH " +
			"NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS INHERIT LOGIN"
		grantSQL         = `GRANT pg_monitor TO cnpg_metrics_exporter`
		clearPasswordSQL = `ALTER ROLE cnpg_metrics_exporter PASSWORD NULL`
	)

	var (
		dbMock sqlmock.Sqlmock
		db     *sql.DB
		err    error
	)

	BeforeEach(func() {
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	It("creates the role and grants pg_monitor when missing", func(ctx SpecContext) {
		dbMock.ExpectBegin()
		dbMock.ExpectQuery(existsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
		dbMock.ExpectExec(createSQL).WillReturnResult(sqlmock.NewResult(0, 1))
		// after CREATE ROLE … WITH LOGIN PASSWORD NULL, defaults satisfy the
		// attribute check and the password is NULL; only pg_monitor is missing.
		dbMock.ExpectQuery(attrsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"attrs", "membership", "no_password"}).AddRow(true, false, true))
		dbMock.ExpectExec(grantSQL).WillReturnResult(sqlmock.NewResult(0, 1))
		dbMock.ExpectCommit()

		Expect(setupMetricsExporterRole(ctx, db)).To(Succeed())
	})

	It("is a no-op when the role exists with correct attributes, pg_monitor and no password", func(ctx SpecContext) {
		dbMock.ExpectBegin()
		dbMock.ExpectQuery(existsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		dbMock.ExpectQuery(attrsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"attrs", "membership", "no_password"}).AddRow(true, true, true))
		dbMock.ExpectCommit()

		Expect(setupMetricsExporterRole(ctx, db)).To(Succeed())
	})

	It("repairs attributes when the role drifts", func(ctx SpecContext) {
		dbMock.ExpectBegin()
		dbMock.ExpectQuery(existsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		dbMock.ExpectQuery(attrsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"attrs", "membership", "no_password"}).AddRow(false, true, true))
		dbMock.ExpectExec(alterSQL).WillReturnResult(sqlmock.NewResult(0, 1))
		dbMock.ExpectCommit()

		Expect(setupMetricsExporterRole(ctx, db)).To(Succeed())
	})

	It("grants pg_monitor when missing on an existing role", func(ctx SpecContext) {
		dbMock.ExpectBegin()
		dbMock.ExpectQuery(existsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		dbMock.ExpectQuery(attrsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"attrs", "membership", "no_password"}).AddRow(true, false, true))
		dbMock.ExpectExec(grantSQL).WillReturnResult(sqlmock.NewResult(0, 1))
		dbMock.ExpectCommit()

		Expect(setupMetricsExporterRole(ctx, db)).To(Succeed())
	})

	It("clears the password when the role has one", func(ctx SpecContext) {
		dbMock.ExpectBegin()
		dbMock.ExpectQuery(existsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		dbMock.ExpectQuery(attrsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"attrs", "membership", "no_password"}).AddRow(true, true, false))
		dbMock.ExpectExec(clearPasswordSQL).WillReturnResult(sqlmock.NewResult(0, 1))
		dbMock.ExpectCommit()

		Expect(setupMetricsExporterRole(ctx, db)).To(Succeed())
	})

	It("repairs everything when attributes, pg_monitor and password all drift", func(ctx SpecContext) {
		dbMock.ExpectBegin()
		dbMock.ExpectQuery(existsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		dbMock.ExpectQuery(attrsQuery).
			WithArgs("cnpg_metrics_exporter").
			WillReturnRows(sqlmock.NewRows([]string{"attrs", "membership", "no_password"}).AddRow(false, false, false))
		dbMock.ExpectExec(alterSQL).WillReturnResult(sqlmock.NewResult(0, 1))
		dbMock.ExpectExec(grantSQL).WillReturnResult(sqlmock.NewResult(0, 1))
		dbMock.ExpectExec(clearPasswordSQL).WillReturnResult(sqlmock.NewResult(0, 1))
		dbMock.ExpectCommit()

		Expect(setupMetricsExporterRole(ctx, db)).To(Succeed())
	})
})
