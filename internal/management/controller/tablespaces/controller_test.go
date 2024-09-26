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

var _ = Describe("Tablespace synchronizer tests", func() {
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

	expectedReadinessCheck := `
		SELECT
			NOT pg_is_in_recovery()
			OR (SELECT coalesce(setting, '') = '' FROM pg_settings WHERE name = 'primary_conninfo')
			OR pg_last_wal_replay_lsn() IS NOT NULL
		`

	var (
		dbMock               sqlmock.Sqlmock
		db                   *sql.DB
		err                  error
		fakeClient           client.Client
		cluster              *apiv1.Cluster
		tablespaceReconciler TablespaceReconciler
		instance             fakeInstance
	)

	tablespaceReconciler = TablespaceReconciler{
		instance: postgres.NewInstance().WithNamespace("myPod"),
	}

	BeforeEach(func() {
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual), sqlmock.MonitorPingsOption(true))
		Expect(err).ToNot(HaveOccurred())
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
		}
		fakeClient = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(&apiv1.Cluster{}).
			Build()
		instance = fakeInstance{
			Instance: &postgres.Instance{
				Namespace:   "default",
				ClusterName: "cluster-example",
			},
			db: db,
		}
		tablespaceReconciler = TablespaceReconciler{
			instance: &instance,
			client:   fakeClient,
		}

		// these bits happen since the reconciler checks for instance readiness
		dbMock.ExpectPing()
		expectedReadiness := sqlmock.NewRows([]string{""}).AddRow("t")
		dbMock.ExpectQuery(expectedReadinessCheck).WillReturnRows(expectedReadiness)
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	When("tablespace configurations are realizable", func() {
		It("will do nothing if the DB contains the tablespaces in spec", func(ctx context.Context) {
			initialCluster := cluster.DeepCopy()
			cluster.Spec.Tablespaces = []apiv1.TablespaceConfiguration{
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
			err := fakeClient.Patch(ctx, cluster, client.MergeFrom(initialCluster))
			Expect(err).NotTo(HaveOccurred())

			// we expect the reconciler to list the tablespaces on the DB
			rows := sqlmock.NewRows(
				[]string{"spcname", "rolname"}).
				AddRow("foo", "app")
			dbMock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)

			results, err := tablespaceReconciler.Reconcile(ctx, reconcile.Request{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(results).To(BeZero())

			var updatedCluster apiv1.Cluster
			err = fakeClient.Get(ctx, client.ObjectKey{
				Namespace: cluster.Namespace,
				Name:      cluster.Name,
			}, &updatedCluster)
			Expect(err).ToNot(HaveOccurred())
			expectedTablespaceStatus := []apiv1.TablespaceState{
				{
					Name:  "foo",
					Owner: "app",
					State: "reconciled",
				},
			}
			Expect(updatedCluster.Status.TablespacesStatus).To(Equal(expectedTablespaceStatus))
		})

		It("will change the owner when needed", func(ctx context.Context) {
			initialCluster := cluster.DeepCopy()
			cluster.Spec.Tablespaces = []apiv1.TablespaceConfiguration{
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
			err := fakeClient.Patch(ctx, cluster, client.MergeFrom(initialCluster))
			Expect(err).NotTo(HaveOccurred())

			rows := sqlmock.NewRows(
				[]string{"spcname", "rolname"}).
				AddRow("foo", "app")
			dbMock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
			stmt := fmt.Sprintf(expectedUpdateStmt, "foo", "new_user")
			dbMock.ExpectExec(stmt).
				WillReturnResult(sqlmock.NewResult(2, 1))

			results, err := tablespaceReconciler.Reconcile(ctx, reconcile.Request{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(results).To(BeZero())

			var updatedCluster apiv1.Cluster
			err = fakeClient.Get(ctx, client.ObjectKey{
				Namespace: cluster.Namespace,
				Name:      cluster.Name,
			}, &updatedCluster)
			Expect(err).ToNot(HaveOccurred())
			expectedTablespaceStatus := []apiv1.TablespaceState{
				{
					Name:  "foo",
					Owner: "new_user",
					State: "reconciled",
				},
			}
			Expect(updatedCluster.Status.TablespacesStatus).To(Equal(expectedTablespaceStatus))
		})

		It("will create a tablespace in spec that is missing from DB if mount point exists", func(ctx context.Context) {
			initialCluster := cluster.DeepCopy()
			cluster.Spec.Tablespaces = []apiv1.TablespaceConfiguration{
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
			err := fakeClient.Patch(ctx, cluster, client.MergeFrom(initialCluster))
			Expect(err).NotTo(HaveOccurred())

			// we expect the reconciler to list the tablespaces on DB, and to
			// create a new tablespace
			rows := sqlmock.NewRows(
				[]string{"spcname", "rolname"}).
				AddRow("foo", "")
			dbMock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
			stmt := fmt.Sprintf(expectedCreateStmt, "bar", "new_user", "/var/lib/postgresql/tablespaces/bar/data")
			dbMock.ExpectExec(stmt).
				WillReturnResult(sqlmock.NewResult(2, 1))

			tablespaceReconciler = TablespaceReconciler{
				instance: instance,
				client:   fakeClient,
				storageManager: mockTablespaceStorageManager{
					unavailableStorageLocations: []string{
						"/foo",
					},
				},
			}

			results, err := tablespaceReconciler.Reconcile(ctx, reconcile.Request{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(results).To(BeZero())

			var updatedCluster apiv1.Cluster
			err = fakeClient.Get(ctx, client.ObjectKey{
				Namespace: cluster.Namespace,
				Name:      cluster.Name,
			}, &updatedCluster)
			Expect(err).ToNot(HaveOccurred())
			expectedTablespaceStatus := []apiv1.TablespaceState{
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
			}
			Expect(updatedCluster.Status.TablespacesStatus).To(Equal(expectedTablespaceStatus))
		})

		It("will mark tablespace status as pending with error when the DB CREATE fails", func(ctx context.Context) {
			initialCluster := cluster.DeepCopy()
			cluster.Spec.Tablespaces = []apiv1.TablespaceConfiguration{
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
			err := fakeClient.Patch(ctx, cluster, client.MergeFrom(initialCluster))
			Expect(err).NotTo(HaveOccurred())

			// we expect the reconciler to list the tablespaces on DB, and to
			// create a new tablespace
			rows := sqlmock.NewRows(
				[]string{"spcname", "rolname"}).
				AddRow("foo", "")
			dbMock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
			// we simulate DB command failure
			stmt := fmt.Sprintf(expectedCreateStmt, "bar", "new_user", "/var/lib/postgresql/tablespaces/bar/data")
			dbMock.ExpectExec(stmt).
				WillReturnError(errors.New("boom"))

			tablespaceReconciler = TablespaceReconciler{
				instance: instance,
				client:   fakeClient,
				storageManager: mockTablespaceStorageManager{
					unavailableStorageLocations: []string{
						"/foo",
					},
				},
			}

			results, err := tablespaceReconciler.Reconcile(ctx, reconcile.Request{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(results).NotTo(BeZero())

			var updatedCluster apiv1.Cluster
			err = fakeClient.Get(ctx, client.ObjectKey{
				Namespace: cluster.Namespace,
				Name:      cluster.Name,
			}, &updatedCluster)
			Expect(err).ToNot(HaveOccurred())
			expectedTablespaceStatus := []apiv1.TablespaceState{
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
			}
			Expect(updatedCluster.Status.TablespacesStatus).To(Equal(expectedTablespaceStatus))
		})

		It("will requeue the tablespace creation if the mount path doesn't exist", func(ctx context.Context) {
			initialCluster := cluster.DeepCopy()
			cluster.Spec.Tablespaces = []apiv1.TablespaceConfiguration{
				{
					Name: "foo",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
			}
			err := fakeClient.Patch(ctx, cluster, client.MergeFrom(initialCluster))
			Expect(err).NotTo(HaveOccurred())

			rows := sqlmock.NewRows(
				[]string{"spcname", "rolname"})
			dbMock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)

			tablespaceReconciler = TablespaceReconciler{
				instance: instance,
				client:   fakeClient,
				storageManager: mockTablespaceStorageManager{
					unavailableStorageLocations: []string{
						"/foo",
					},
				},
			}

			results, err := tablespaceReconciler.Reconcile(ctx, reconcile.Request{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(results).NotTo(BeNil())
			Expect(results.RequeueAfter).NotTo(BeZero())

			var updatedCluster apiv1.Cluster
			err = fakeClient.Get(ctx, client.ObjectKey{
				Namespace: cluster.Namespace,
				Name:      cluster.Name,
			}, &updatedCluster)
			Expect(err).ToNot(HaveOccurred())
			expectedTablespaceStatus := []apiv1.TablespaceState{
				{
					Name:  "foo",
					Owner: "",
					State: "pending",
					Error: "deferred until mount point is created",
				},
			}
			Expect(updatedCluster.Status.TablespacesStatus).To(Equal(expectedTablespaceStatus))
		})
	})
})
