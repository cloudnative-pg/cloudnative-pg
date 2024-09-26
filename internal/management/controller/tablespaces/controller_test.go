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

package tablespaces

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/DATA-DOG/go-sqlmock"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/tablespaces/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mockTablespaceStorageManager struct {
	unavailableStorageLocations []string
}

func (mst mockTablespaceStorageManager) storageExists(tablespaceName string) (bool, error) {
	return !slices.Contains(
		mst.unavailableStorageLocations,
		mst.getStorageLocation(tablespaceName),
	), nil
}

func (mst mockTablespaceStorageManager) getStorageLocation(tablespaceName string) string {
	return fmt.Sprintf("/%s", tablespaceName)
}

var _ = Describe("Tablespace synchronizer tests", func() {
	tablespaceReconciler := TablespaceReconciler{
		instance: postgres.NewInstance().WithNamespace("myPod"),
	}
	expectedListStmt := `
	SELECT
		pg_tablespace.spcname spcname,
		COALESCE(pg_roles.rolname, '') rolname
	FROM pg_tablespace
	LEFT JOIN pg_roles ON pg_tablespace.spcowner = pg_roles.oid
	WHERE spcname NOT LIKE $1
	`
	expectedCreateStmt := "CREATE TABLESPACE \"%s\" OWNER \"%s\" " +
		"LOCATION '%s'"

	expectedUpdateStmt := "ALTER TABLESPACE \"%s\" OWNER TO \"%s\""

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

	When("tablespace configurations are realizable", func() {
		It("will do nothing if the DB contains the tablespaces in spec", func(ctx context.Context) {
			tablespacesSpec := []apiv1.TablespaceConfiguration{
				{
					Name: "foo",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
					Owner: apiv1.DatabaseRoleRef{
						Name: "app",
					},
				},
			}
			rows := sqlmock.NewRows(
				[]string{"spcname", "rolname"}).
				AddRow("foo", "app")
			dbMock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
			tbsInDatabase, err := infrastructure.List(ctx, db)
			Expect(err).ShouldNot(HaveOccurred())
			tbsSteps := evaluateNextSteps(ctx, tbsInDatabase, tablespacesSpec)
			result := tablespaceReconciler.applySteps(ctx, db,
				mockTablespaceStorageManager{}, tbsSteps)
			Expect(result).To(ConsistOf(apiv1.TablespaceState{
				Name:  "foo",
				Owner: "app",
				State: apiv1.TablespaceStatusReconciled,
				Error: "",
			}))
		})

		It("will change the owner when needed", func(ctx context.Context) {
			tablespacesSpec := []apiv1.TablespaceConfiguration{
				{
					Name: "foo",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
					Owner: apiv1.DatabaseRoleRef{
						Name: "new_user",
					},
				},
			}
			rows := sqlmock.NewRows(
				[]string{"spcname", "rolname"}).
				AddRow("foo", "app")
			dbMock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
			stmt := fmt.Sprintf(expectedUpdateStmt, "foo", "new_user")
			dbMock.ExpectExec(stmt).
				WillReturnResult(sqlmock.NewResult(2, 1))
			tbsInDatabase, err := infrastructure.List(ctx, db)
			Expect(err).ShouldNot(HaveOccurred())
			tbsByAction := evaluateNextSteps(ctx, tbsInDatabase, tablespacesSpec)
			result := tablespaceReconciler.applySteps(ctx, db,
				mockTablespaceStorageManager{}, tbsByAction)
			Expect(result).To(ConsistOf(
				apiv1.TablespaceState{
					Name:  "foo",
					Owner: "new_user",
					State: apiv1.TablespaceStatusReconciled,
					Error: "",
				},
			))
		})

		It("will create a tablespace in spec that is missing from DB", func(ctx context.Context) {
			tablespacesSpec := []apiv1.TablespaceConfiguration{
				{
					Name: "foo",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
				{
					Name: "bar",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
					Owner: apiv1.DatabaseRoleRef{
						Name: "new_user",
					},
				},
			}
			rows := sqlmock.NewRows(
				[]string{"spcname", "rolname"}).
				AddRow("foo", "")
			dbMock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
			stmt := fmt.Sprintf(expectedCreateStmt, "bar", "new_user", "/var/lib/postgresql/tablespaces/bar/data")
			dbMock.ExpectExec(stmt).
				WillReturnResult(sqlmock.NewResult(2, 1))
			tbsInDatabase, err := infrastructure.List(ctx, db)
			Expect(err).ShouldNot(HaveOccurred())
			tbsSteps := evaluateNextSteps(ctx, tbsInDatabase, tablespacesSpec)
			result := tablespaceReconciler.applySteps(ctx, db,
				mockTablespaceStorageManager{}, tbsSteps)
			Expect(result).To(ConsistOf(
				apiv1.TablespaceState{
					Name:  "foo",
					Owner: "",
					State: apiv1.TablespaceStatusReconciled,
				},
				apiv1.TablespaceState{
					Name:  "bar",
					Owner: "new_user",
					State: apiv1.TablespaceStatusReconciled,
				},
			))
		})

		It("will requeue the tablespace creation if the mount path doesn't exist", func(ctx context.Context) {
			tablespacesSpec := []apiv1.TablespaceConfiguration{
				{
					Name: "foo",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
			}
			rows := sqlmock.NewRows(
				[]string{"spcname", "rolname"})
			dbMock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
			tbsInDatabase, err := infrastructure.List(ctx, db)
			Expect(err).ShouldNot(HaveOccurred())
			tbsByAction := evaluateNextSteps(ctx, tbsInDatabase, tablespacesSpec)
			result := tablespaceReconciler.applySteps(ctx, db,
				mockTablespaceStorageManager{
					unavailableStorageLocations: []string{
						"/foo",
					},
				}, tbsByAction)
			Expect(result).To(ConsistOf(
				apiv1.TablespaceState{
					Name:  "foo",
					Owner: "",
					State: apiv1.TablespaceStatusPendingReconciliation,
					Error: "deferred until mount point is created",
				},
			))
		})
	})
})
