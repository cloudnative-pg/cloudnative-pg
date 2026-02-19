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

package tablespaces

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/DATA-DOG/go-sqlmock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mockTablespaceStorageManager is a storage manager where storage exists by
// default unless explicitly mounted as unavailable
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

type fakeInstance struct {
	*postgres.Instance
	db *sql.DB
}

func (f fakeInstance) GetSuperUserDB() (*sql.DB, error) {
	return f.db, nil
}

func (f fakeInstance) CanCheckReadiness() bool {
	return true
}

func (f fakeInstance) IsPrimary() (bool, error) {
	return true, nil
}

const (
	expectedListStmt = `
	SELECT
		pg_tablespace.spcname spcname,
		COALESCE(pg_roles.rolname, '') rolname
	FROM pg_catalog.pg_tablespace
	LEFT JOIN pg_catalog.pg_roles ON pg_tablespace.spcowner = pg_roles.oid
	WHERE spcname NOT LIKE $1
	`
	expectedCreateStmt = "CREATE TABLESPACE \"%s\" OWNER \"%s\" " +
		"LOCATION '%s'"

	expectedUpdateStmt = "ALTER TABLESPACE \"%s\" OWNER TO \"%s\""

	expectedReadinessCheck = `
		SELECT
			NOT pg_catalog.pg_is_in_recovery()
			OR (SELECT coalesce(setting, '') = '' FROM pg_catalog.pg_settings WHERE name = 'primary_conninfo')
			OR pg_catalog.pg_last_wal_replay_lsn() IS NOT NULL
		`
)

func getCluster(ctx context.Context, c client.Client, cluster *apiv1.Cluster) (*apiv1.Cluster, error) {
	var updatedCluster apiv1.Cluster
	err := c.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}, &updatedCluster)
	return &updatedCluster, err
}

// tablespaceTest represents all the variable bits that go into a test of the
// tablespace reconciler
type tablespaceTest struct {
	tablespacesInSpec        []apiv1.TablespaceConfiguration
	clusterStatus            apiv1.ClusterStatus
	postgresExpectations     func(sqlmock.Sqlmock)
	shouldRequeue            bool
	storageManager           tablespaceStorageManager
	expectedTablespaceStatus []apiv1.TablespaceState
}

// assertTablespaceReconciled is the full test, going from setting up the mocks
// and the cluster to verifying all expectations are met
func assertTablespaceReconciled(ctx context.Context, tt tablespaceTest) {
	db, dbMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual), sqlmock.MonitorPingsOption(true))
	Expect(err).ToNot(HaveOccurred())

	DeferCleanup(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-example",
			Namespace: "default",
		},
	}
	cluster.Spec.Tablespaces = tt.tablespacesInSpec
	cluster.Status = tt.clusterStatus

	fakeClient := fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()

	pgInstance := postgres.NewInstance().
		WithNamespace("default").
		WithClusterName("cluster-example")

	instance := fakeInstance{
		Instance: pgInstance,
		db:       db,
	}

	tablespaceReconciler := TablespaceReconciler{
		instance:       &instance,
		client:         fakeClient,
		storageManager: tt.storageManager,
	}

	// these bits happen because the reconciler checks for instance readiness
	dbMock.ExpectPing()
	expectedReadiness := sqlmock.NewRows([]string{""}).AddRow("t")
	dbMock.ExpectQuery(expectedReadinessCheck).WillReturnRows(expectedReadiness)

	tt.postgresExpectations(dbMock)

	results, err := tablespaceReconciler.Reconcile(ctx, reconcile.Request{})
	Expect(err).ShouldNot(HaveOccurred())
	if tt.shouldRequeue {
		Expect(results).NotTo(BeZero())
	} else {
		Expect(results).To(BeZero())
	}

	updatedCluster, err := getCluster(ctx, fakeClient, cluster)
	Expect(err).ToNot(HaveOccurred())
	Expect(updatedCluster.Status.TablespacesStatus).To(Equal(tt.expectedTablespaceStatus))
}

var _ = Describe("Tablespace synchronizer tests", func() {
	When("tablespace configurations are realizable", func() {
		It("will do nothing if the DB contains the tablespaces in spec", func(ctx context.Context) {
			assertTablespaceReconciled(ctx, tablespaceTest{
				tablespacesInSpec: []apiv1.TablespaceConfiguration{
					{
						Name: "foo",
						Storage: apiv1.StorageConfiguration{
							Size: "1Gi",
						},
						Owner: apiv1.DatabaseRoleRef{
							Name: "app",
						},
					},
				},
				postgresExpectations: func(mock sqlmock.Sqlmock) {
					// we expect the reconciler to list the tablespaces on the DB
					rows := sqlmock.NewRows(
						[]string{"spcname", "rolname"}).
						AddRow("foo", "app")
					mock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
				},
				shouldRequeue: false,
				expectedTablespaceStatus: []apiv1.TablespaceState{
					{
						Name:  "foo",
						Owner: "app",
						State: "reconciled",
					},
				},
			})
		})

		It("will change the owner when needed", func(ctx context.Context) {
			assertTablespaceReconciled(ctx, tablespaceTest{
				tablespacesInSpec: []apiv1.TablespaceConfiguration{
					{
						Name: "foo",
						Storage: apiv1.StorageConfiguration{
							Size: "1Gi",
						},
						Owner: apiv1.DatabaseRoleRef{
							Name: "new_user",
						},
					},
				},
				postgresExpectations: func(mock sqlmock.Sqlmock) {
					rows := sqlmock.NewRows(
						[]string{"spcname", "rolname"}).
						AddRow("foo", "app")
					mock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
					stmt := fmt.Sprintf(expectedUpdateStmt, "foo", "new_user")
					mock.ExpectExec(stmt).
						WillReturnResult(sqlmock.NewResult(2, 1))
				},
				shouldRequeue: false,
				expectedTablespaceStatus: []apiv1.TablespaceState{
					{
						Name:  "foo",
						Owner: "new_user",
						State: "reconciled",
					},
				},
			})
		})

		It("will create a tablespace in spec that is missing from DB if mount point exists", func(ctx context.Context) {
			assertTablespaceReconciled(ctx, tablespaceTest{
				tablespacesInSpec: []apiv1.TablespaceConfiguration{
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
				},
				clusterStatus: apiv1.ClusterStatus{
					InstanceNames: []string{"cluster-example-1", "cluster-example-2"},
					HealthyPVC: []string{
						"cluster-example-1-tbs-foo", "cluster-example-1-tbs-bar",
						"cluster-example-2-tbs-foo", "cluster-example-2-tbs-bar",
					},
				},
				postgresExpectations: func(mock sqlmock.Sqlmock) {
					// we expect the reconciler to list the tablespaces on DB, and to
					// create a new tablespace
					rows := sqlmock.NewRows(
						[]string{"spcname", "rolname"}).
						AddRow("foo", "")
					mock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
					stmt := fmt.Sprintf(expectedCreateStmt, "bar", "new_user", "/var/lib/postgresql/tablespaces/bar/data")
					mock.ExpectExec(stmt).
						WillReturnResult(sqlmock.NewResult(2, 1))
				},
				shouldRequeue: false,
				storageManager: mockTablespaceStorageManager{
					unavailableStorageLocations: []string{},
				},
				expectedTablespaceStatus: []apiv1.TablespaceState{
					{
						Name:  "foo",
						Owner: "",
						State: "reconciled",
					},
					{
						Name:  "bar",
						Owner: "new_user",
						State: "reconciled",
					},
				},
			})
		})

		It("will skip creating a tablespace if any required PVCs are not healthy", func(ctx context.Context) {
			assertTablespaceReconciled(ctx, tablespaceTest{
				tablespacesInSpec: []apiv1.TablespaceConfiguration{
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
				},
				clusterStatus: apiv1.ClusterStatus{
					InstanceNames: []string{"cluster-example-1", "cluster-example-2"},
					HealthyPVC: []string{
						"cluster-example-1-tbs-foo", "cluster-example-1-tbs-bar",
						"cluster-example-2-tbs-foo",
					},
				},
				postgresExpectations: func(mock sqlmock.Sqlmock) {
					// we expect the reconciler to list the tablespaces on DB, and NOT to
					// create a new tablespace
					rows := sqlmock.NewRows(
						[]string{"spcname", "rolname"}).
						AddRow("foo", "")
					mock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
				},
				shouldRequeue: true,
				storageManager: mockTablespaceStorageManager{
					unavailableStorageLocations: []string{},
				},
				expectedTablespaceStatus: []apiv1.TablespaceState{
					{
						Name:  "foo",
						Owner: "",
						State: "reconciled",
					},
					{
						Name:  "bar",
						Owner: "new_user",
						State: "pending",
						Error: "deferred until all required PVCs are healthy",
					},
				},
			})
		})

		It("will mark tablespace status as pending with error when the DB CREATE fails", func(ctx context.Context) {
			assertTablespaceReconciled(ctx, tablespaceTest{
				tablespacesInSpec: []apiv1.TablespaceConfiguration{
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
				},
				postgresExpectations: func(mock sqlmock.Sqlmock) {
					// we expect the reconciler to list the tablespaces on DB, and to
					// create a new tablespace
					rows := sqlmock.NewRows(
						[]string{"spcname", "rolname"}).
						AddRow("foo", "")
					mock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
					// we simulate DB command failure
					stmt := fmt.Sprintf(expectedCreateStmt, "bar", "new_user", "/var/lib/postgresql/tablespaces/bar/data")
					mock.ExpectExec(stmt).
						WillReturnError(errors.New("boom"))
				},
				shouldRequeue: true,
				storageManager: mockTablespaceStorageManager{
					unavailableStorageLocations: []string{},
				},
				expectedTablespaceStatus: []apiv1.TablespaceState{
					{
						Name:  "foo",
						Owner: "",
						State: "reconciled",
					},
					{
						Name:  "bar",
						Owner: "new_user",
						State: "pending",
						Error: "while creating tablespace bar: boom",
					},
				},
			})
		})

		It("will requeue the tablespace creation if the mount path doesn't exist", func(ctx context.Context) {
			assertTablespaceReconciled(ctx, tablespaceTest{
				tablespacesInSpec: []apiv1.TablespaceConfiguration{
					{
						Name: "foo",
						Storage: apiv1.StorageConfiguration{
							Size: "1Gi",
						},
					},
				},
				postgresExpectations: func(mock sqlmock.Sqlmock) {
					rows := sqlmock.NewRows(
						[]string{"spcname", "rolname"})
					mock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
				},
				shouldRequeue: true,
				storageManager: mockTablespaceStorageManager{
					unavailableStorageLocations: []string{
						"/foo",
					},
				},
				expectedTablespaceStatus: []apiv1.TablespaceState{
					{
						Name:  "foo",
						Owner: "",
						State: "pending",
						Error: "deferred until mount point is created",
					},
				},
			})
		})
	})
})

var _ = Describe("buildPVCChecker", func() {
	It("returns true when all PVCs for a tablespace are healthy", func(ctx context.Context) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{"cluster-1", "cluster-2"},
				HealthyPVC: []string{
					"cluster-1-tbs-foo", "cluster-2-tbs-foo",
				},
			},
		}
		checker := buildPVCChecker(ctx, cluster)
		Expect(checker("foo")).To(BeTrue())
	})

	It("returns false when a PVC for a tablespace is missing from the healthy list", func(ctx context.Context) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{"cluster-1", "cluster-2"},
				HealthyPVC: []string{
					"cluster-1-tbs-foo",
					// cluster-2-tbs-foo is missing
				},
			},
		}
		checker := buildPVCChecker(ctx, cluster)
		Expect(checker("foo")).To(BeFalse())
	})

	It("returns true when the cluster has no instances", func(ctx context.Context) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{},
				HealthyPVC:    []string{},
			},
		}
		checker := buildPVCChecker(ctx, cluster)
		Expect(checker("foo")).To(BeTrue())
	})

	It("returns false when there are instances but no healthy PVCs", func(ctx context.Context) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{"cluster-1"},
				HealthyPVC:    []string{},
			},
		}
		checker := buildPVCChecker(ctx, cluster)
		Expect(checker("foo")).To(BeFalse())
	})
})
