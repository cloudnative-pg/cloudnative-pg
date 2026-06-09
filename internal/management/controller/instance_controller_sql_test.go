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

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("reconcileExtensions search_path", func() {
	const existenceQuery = "SELECT COUNT(*) > 0 FROM pg_catalog.pg_extension WHERE extname = $1"

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
		_ = db.Close()
	})

	It("brackets CREATE EXTENSION with the standard \"$user\", public search_path", func(ctx SpecContext) {
		// Pick a managed extension that the operator creates explicitly
		// (not shared-preload only) so the CREATE EXTENSION branch runs.
		var target postgres.ManagedExtension
		for _, ext := range postgres.ManagedExtensions {
			if !ext.SkipCreateExtension && len(ext.Namespaces) > 0 {
				target = ext
				break
			}
		}
		Expect(target.Name).ToNot(BeEmpty(), "expected at least one creatable managed extension")

		// Enable only the target extension via its configuration namespace.
		userSettings := map[string]string{target.Namespaces[0] + ".max": "1000"}

		dbMock.ExpectBegin()
		for _, ext := range postgres.ManagedExtensions {
			// Report every extension as not yet installed.
			dbMock.ExpectQuery(existenceQuery).WithArgs(ext.Name).
				WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(false))

			if ext.Name == target.Name {
				// The fix: search_path is set to the standard resolution
				// immediately before CREATE EXTENSION so the relocatable
				// extension is not created against the pinned pg_catalog.
				dbMock.ExpectExec(`SET LOCAL search_path TO "$user", public`).
					WillReturnResult(sqlmock.NewResult(0, 0))
				dbMock.ExpectExec(fmt.Sprintf("CREATE EXTENSION %s", ext.Name)).
					WillReturnResult(sqlmock.NewResult(0, 0))
			}
		}
		dbMock.ExpectCommit()

		r := &InstanceReconciler{}
		Expect(r.reconcileExtensions(ctx, db, userSettings)).To(Succeed())
	})

	It("does not touch search_path when no extension needs to be created", func(ctx SpecContext) {
		// No user settings -> no managed extension is in use. Each extension is
		// only probed for existence; no SET LOCAL / CREATE EXTENSION is issued.
		dbMock.ExpectBegin()
		for _, ext := range postgres.ManagedExtensions {
			dbMock.ExpectQuery(existenceQuery).WithArgs(ext.Name).
				WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(false))
		}
		dbMock.ExpectCommit()

		r := &InstanceReconciler{}
		Expect(r.reconcileExtensions(ctx, db, map[string]string{})).To(Succeed())
	})
})
