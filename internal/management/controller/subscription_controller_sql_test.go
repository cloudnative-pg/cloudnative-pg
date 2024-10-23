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

// nolint: dupl
package controller

import (
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// nolint: dupl
var _ = Describe("subscription sql", func() {
	var (
		dbMock sqlmock.Sqlmock
		db     *sql.DB
	)

	BeforeEach(func() {
		var err error
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	It("drops the subscription successfully", func(ctx SpecContext) {
		dbMock.ExpectExec(fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", pgx.Identifier{"subscription_name"}.Sanitize())).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := executeDropSubscription(ctx, db, "subscription_name")
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns an error when dropping the subscription fails", func(ctx SpecContext) {
		dbMock.ExpectExec(fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", pgx.Identifier{"subscription_name"}.Sanitize())).
			WillReturnError(fmt.Errorf("drop subscription error"))

		err := executeDropSubscription(ctx, db, "subscription_name")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("while dropping subscription: drop subscription error"))
	})

	It("sanitizes the subscription name correctly", func(ctx SpecContext) {
		dbMock.ExpectExec(fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", pgx.Identifier{"sanitized_name"}.Sanitize())).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := executeDropSubscription(ctx, db, "sanitized_name")
		Expect(err).ToNot(HaveOccurred())
	})
})
