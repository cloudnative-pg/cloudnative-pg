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

package roles

import (
	"context"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Postgres RoleManager implementation test", func() {
	It("can read the list of non-system roles from the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		rows := sqlmock.NewRows([]string{"rolname", "rolcreatedb", "rolsuper",
			"rolcanlogin", "rolbypassrls", "rolpassword", "rolvaliduntil"}).
			AddRow("postgres", true, true, true, true, []byte("12345"), nil)
		mock.ExpectQuery(fmt.Sprintf(`SELECT rolname, rolcreatedb, rolsuper, rolcanlogin, rolbypassrls,
		rolpassword, rolvaliduntil
	  FROM pg_catalog.pg_roles where rolname not like 'pg_%%';`)).WillReturnRows(rows)
		_, err = prm.List(ctx, nil)
		Expect(err).ShouldNot(HaveOccurred())
	})
})
