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

package roles

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeInstanceData struct {
	*postgres.Instance
	db *sql.DB
}

func (f *fakeInstanceData) GetSuperUserDB() (*sql.DB, error) {
	return f.db, nil
}

type mockInstanceForStart struct {
	isPrimaryVal     atomic.Bool
	syncChan         chan *apiv1.ManagedConfiguration
	isPrimaryChecked chan struct{}
	isReadyCalls     atomic.Int32
}

func (m *mockInstanceForStart) GetSuperUserDB() (*sql.DB, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockInstanceForStart) IsPrimary() (bool, error) {
	result := m.isPrimaryVal.Load()
	if m.isPrimaryChecked != nil {
		select {
		case m.isPrimaryChecked <- struct{}{}:
		default:
		}
	}
	return result, nil
}

func (m *mockInstanceForStart) RoleSynchronizerChan() <-chan *apiv1.ManagedConfiguration {
	return m.syncChan
}

func (m *mockInstanceForStart) IsReady() error {
	m.isReadyCalls.Add(1)
	return fmt.Errorf("not ready")
}

func (m *mockInstanceForStart) GetClusterName() string {
	return "test-cluster"
}

func (m *mockInstanceForStart) GetNamespaceName() string {
	return "default"
}

var _ = Describe("RoleSynchronizer Start", func() {
	It("should return nil when context is cancelled", func() {
		syncChan := make(chan *apiv1.ManagedConfiguration)
		instance := &mockInstanceForStart{syncChan: syncChan}
		sr := &RoleSynchronizer{instance: instance}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- sr.Start(ctx)
		}()

		// Ensure Start is blocking (not returning immediately)
		Consistently(errCh, 200*time.Millisecond).ShouldNot(Receive())

		cancel()

		var startErr error
		Eventually(errCh).Should(Receive(&startErr))
		Expect(startErr).ToNot(HaveOccurred())
	})

	It("should skip reconciliation on non-primary instances", func() {
		syncChan := make(chan *apiv1.ManagedConfiguration, 1)
		isPrimaryChecked := make(chan struct{}, 1)
		instance := &mockInstanceForStart{
			syncChan:         syncChan,
			isPrimaryChecked: isPrimaryChecked,
		}
		sr := &RoleSynchronizer{instance: instance}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go sr.Start(ctx) //nolint:errcheck

		syncChan <- &apiv1.ManagedConfiguration{
			Roles: []apiv1.RoleConfiguration{{Name: "test-role"}},
		}
		Eventually(isPrimaryChecked).Should(Receive())
		Expect(instance.isReadyCalls.Load()).To(BeEquivalentTo(0))
	})

	It("should attempt reconciliation on primary instances", func() {
		syncChan := make(chan *apiv1.ManagedConfiguration, 1)
		isPrimaryChecked := make(chan struct{}, 1)
		instance := &mockInstanceForStart{
			syncChan:         syncChan,
			isPrimaryChecked: isPrimaryChecked,
		}
		instance.isPrimaryVal.Store(true)
		sr := &RoleSynchronizer{instance: instance}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go sr.Start(ctx) //nolint:errcheck

		syncChan <- &apiv1.ManagedConfiguration{
			Roles: []apiv1.RoleConfiguration{{Name: "test-role"}},
		}
		Eventually(isPrimaryChecked).Should(Receive())
		Eventually(func() int32 {
			return instance.isReadyCalls.Load()
		}).Should(BeEquivalentTo(1))
	})

	It("should start reconciling after promotion", func() {
		syncChan := make(chan *apiv1.ManagedConfiguration, 1)
		isPrimaryChecked := make(chan struct{}, 1)
		instance := &mockInstanceForStart{
			syncChan:         syncChan,
			isPrimaryChecked: isPrimaryChecked,
		}
		sr := &RoleSynchronizer{instance: instance}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go sr.Start(ctx) //nolint:errcheck

		config := &apiv1.ManagedConfiguration{
			Roles: []apiv1.RoleConfiguration{{Name: "test-role"}},
		}

		// Trigger while still a replica: reconcile must be skipped
		syncChan <- config
		Eventually(isPrimaryChecked).Should(Receive())
		Expect(instance.isReadyCalls.Load()).To(BeEquivalentTo(0))

		// Simulate promotion
		instance.isPrimaryVal.Store(true)

		syncChan <- config
		Eventually(isPrimaryChecked).Should(Receive())
		Eventually(func() int32 {
			return instance.isReadyCalls.Load()
		}).Should(BeEquivalentTo(1))
	})
})

var _ = Describe("Role synchronizer tests", func() {
	var (
		db               *sql.DB
		mock             sqlmock.Sqlmock
		err              error
		roleSynchronizer RoleSynchronizer
	)

	BeforeEach(func() {
		db, mock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		testDate := time.Date(2023, 4, 4, 0, 0, 0, 0, time.UTC)

		rowsInMockDatabase := sqlmock.NewRows([]string{
			"rolname", "rolsuper", "rolinherit", "rolcreaterole", "rolcreatedb",
			"rolcanlogin", "rolreplication", "rolconnlimit", "rolpassword", "rolvaliduntil", "rolbypassrls", "comment",
			"xmin", "inroles",
		}).
			AddRow("postgres", true, false, true, true, true, false, -1, []byte("12345"),
				nil, false, []byte("This is postgres user"), 11, []byte("{}")).
			AddRow("streaming_replica", false, false, true, true, false, true, 10, []byte("54321"),
				pgtype.Timestamp{
					Valid:            true,
					Time:             testDate,
					InfinityModifier: pgtype.Finite,
				}, false, []byte("This is streaming_replica user"), 22, []byte(`{"role1","role2"}`)).
			AddRow("role_to_ignore", true, false, true, true, true, false, -1, []byte("12345"),
				nil, false, []byte("This is a custom role in the DB"), 11, []byte("{}")).
			AddRow("role_to_test1", true, true, false, false, false, false, -1, []byte("12345"),
				nil, false, []byte("This is a role to test with"), 11, []byte("{}")).
			AddRow("role_to_test2", true, true, false, false, false, false, -1, []byte("12345"),
				nil, false, []byte("This is a role to test with"), 11, []byte("{inrole}"))
		mock.ExpectQuery(expectedSelStmt).WillReturnRows(rowsInMockDatabase)

		roleSynchronizer = RoleSynchronizer{
			instance: &fakeInstanceData{
				Instance: postgres.NewInstance().WithNamespace("default"),
				db:       db,
			},
		}
	})

	When("role configurations are realizable", func() {
		It("it will Create ensure:present roles in spec missing from DB", func(ctx context.Context) {
			mock.ExpectExec("CREATE ROLE \"foo_bar\" NOBYPASSRLS NOCREATEDB NOCREATEROLE INHERIT " +
				"NOLOGIN NOREPLICATION NOSUPERUSER CONNECTION LIMIT 0").
				WillReturnResult(sqlmock.NewResult(11, 1))
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:   "foo_bar",
						Ensure: apiv1.EnsurePresent,
					},
				},
			}
			rows := mock.NewRows([]string{"xmin"}).AddRow("12")
			lastTransactionQuery := "SELECT xmin FROM pg_catalog.pg_authid WHERE rolname = $1"
			mock.ExpectQuery(lastTransactionQuery).WithArgs("foo_bar").WillReturnRows(rows)
			passwordState, rolesWithErrors, err := roleSynchronizer.synchronizeRoles(ctx, db, &managedConf,
				map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rolesWithErrors).To(BeEmpty())
			Expect(passwordState).To(BeEquivalentTo(map[string]apiv1.PasswordState{
				"foo_bar": {
					TransactionID:         12,
					SecretResourceVersion: "",
				},
			}))
		})

		It("it will ignore ensure:absent roles in spec missing from DB", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:   "edb_test",
						Ensure: apiv1.EnsureAbsent,
					},
				},
			}

			_, _, err := roleSynchronizer.synchronizeRoles(ctx, db, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("it will call the necessary grants to update membership", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:      "role_to_test1",
						Superuser: true,
						Inherit:   ptr.To(true),
						InRoles: []string{
							"role1",
							"role2",
						},
						Comment:         "This is a role to test with",
						ConnectionLimit: -1,
					},
				},
			}
			noParents := sqlmock.NewRows([]string{"inroles"}).AddRow([]byte(`{}`))
			mock.ExpectQuery(expectedMembershipStmt).WithArgs("role_to_test1").WillReturnRows(noParents)
			mock.ExpectBegin()
			expectedMembershipExecs := []string{
				`GRANT "role1" TO "role_to_test1"`,
				`GRANT "role2" TO "role_to_test1"`,
			}

			for _, ex := range expectedMembershipExecs {
				mock.ExpectExec(ex).
					WillReturnResult(sqlmock.NewResult(2, 3))
			}

			mock.ExpectCommit()

			_, rolesWithErrors, err := roleSynchronizer.synchronizeRoles(ctx, db, &managedConf, map[string]apiv1.PasswordState{
				"role_to_test1": {
					TransactionID: 11, // defined in the mock query to the DB above
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rolesWithErrors).To(BeEmpty())
		})

		It("it will call the necessary revokes to update membership", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:            "role_to_test2",
						Superuser:       true,
						Inherit:         ptr.To(true),
						InRoles:         []string{},
						Comment:         "This is a role to test with",
						ConnectionLimit: -1,
					},
				},
			}
			rows := sqlmock.NewRows([]string{
				"inroles",
			}).
				AddRow([]byte(`{"foo"}`))
			mock.ExpectQuery(expectedMembershipStmt).WithArgs("role_to_test2").WillReturnRows(rows)
			mock.ExpectBegin()

			mock.ExpectExec(`REVOKE "foo" FROM "role_to_test2"`).
				WillReturnResult(sqlmock.NewResult(2, 3))

			mock.ExpectCommit()

			_, rolesWithErrors, err := roleSynchronizer.synchronizeRoles(ctx, db, &managedConf, map[string]apiv1.PasswordState{
				"role_to_test2": {
					TransactionID: 11, // defined in the mock query to the DB above
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rolesWithErrors).To(BeEmpty())
		})

		It("it will call the updateComment method", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:            "role_to_test1",
						Superuser:       true,
						Inherit:         ptr.To(true),
						Comment:         "my comment",
						ConnectionLimit: -1,
					},
				},
			}
			wantedRoleCommentStmt := fmt.Sprintf(
				wantedRoleCommentTpl,
				managedConf.Roles[0].Name, pq.QuoteLiteral(managedConf.Roles[0].Comment))
			mock.ExpectExec(wantedRoleCommentStmt).WillReturnResult(sqlmock.NewResult(2, 3))
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, db, &managedConf, map[string]apiv1.PasswordState{
				"role_to_test1": {
					TransactionID: 11, // defined in the mock query to the DB above
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("it will no-op if the roles are reconciled", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:            "role_to_test1",
						Superuser:       true,
						Inherit:         ptr.To(true),
						Comment:         "This is a role to test with",
						ConnectionLimit: -1,
					},
				},
			}
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, db, &managedConf, map[string]apiv1.PasswordState{
				"role_to_test1": {
					TransactionID: 11, // defined in the mock query to the DB above
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("it will Delete ensure:absent roles that are in the DB", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:   "role_to_test1",
						Ensure: apiv1.EnsureAbsent,
					},
				},
			}
			roleDeletionStmt := fmt.Sprintf("DROP ROLE \"%s\"", "role_to_test1")
			mock.ExpectExec(roleDeletionStmt).WillReturnResult(sqlmock.NewResult(2, 3))
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, db, &managedConf, map[string]apiv1.PasswordState{
				"role_to_test1": {
					TransactionID: 11, // defined in the mock query to the DB above
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("it will Update ensure:present roles that are in the DB but have different fields", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:            "role_to_test1",
						Superuser:       false,
						Inherit:         ptr.To(false),
						Comment:         "This is a role to test with",
						BypassRLS:       true,
						CreateRole:      true,
						Login:           true,
						ConnectionLimit: 2,
					},
				},
			}
			alterStmt := fmt.Sprintf(
				"ALTER ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION NOSUPERUSER CONNECTION LIMIT 2 ",
				"role_to_test1")
			mock.ExpectExec(alterStmt).WillReturnResult(sqlmock.NewResult(2, 3))
			rows := mock.NewRows([]string{"xmin"}).AddRow("12")
			lastTransactionQuery := "SELECT xmin FROM pg_catalog.pg_authid WHERE rolname = $1"
			mock.ExpectQuery(lastTransactionQuery).WithArgs("role_to_test1").WillReturnRows(rows)
			passwordState, rolesWithErrors, err := roleSynchronizer.synchronizeRoles(ctx, db, &managedConf,
				map[string]apiv1.PasswordState{
					"role_to_test1": {
						TransactionID: 11, // defined in the mock query to the DB above
					},
				})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rolesWithErrors).To(BeEmpty())
			Expect(passwordState).To(BeEquivalentTo(map[string]apiv1.PasswordState{
				"role_to_test1": {
					TransactionID:         12,
					SecretResourceVersion: "",
				},
			}))
		})
	})

	When("role configurations are unrealizable", func() {
		It("it will carry on and capture postgres errors per role", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:      "role_to_test1",
						Superuser: true,
						Inherit:   ptr.To(true),
						InRoles: []string{
							"role1",
							"role2",
						},
						Comment:         "This is a role to test with",
						ConnectionLimit: -1,
					},
					{
						Name:   "role_to_test2",
						Ensure: apiv1.EnsureAbsent,
					},
				},
			}

			noParents := sqlmock.NewRows([]string{"inroles"}).AddRow([]byte(`{}`))
			mock.ExpectQuery(expectedMembershipStmt).WithArgs("role_to_test1").WillReturnRows(noParents)
			mock.ExpectBegin()

			mock.ExpectExec(`GRANT "role1" TO "role_to_test1"`).
				WillReturnResult(sqlmock.NewResult(2, 3))

			impossibleGrantError := pgconn.PgError{
				Code:    "0LP01", // 0LP01 -> invalid_grant_operation
				Message: "unknown role 'role2'",
			}
			mock.ExpectExec(`GRANT "role2" TO "role_to_test1"`).
				WillReturnError(&impossibleGrantError)

			mock.ExpectRollback()

			impossibleDeleteError := pgconn.PgError{
				Code:   "2BP01", // 2BP01 -> dependent_objects_still_exist
				Detail: "owner of database edbDatabase",
			}

			roleDeletionStmt := fmt.Sprintf("DROP ROLE \"%s\"", "role_to_test2")
			mock.ExpectExec(roleDeletionStmt).WillReturnError(&impossibleDeleteError)

			_, unrealizable, err := roleSynchronizer.synchronizeRoles(ctx, db, &managedConf, map[string]apiv1.PasswordState{
				"role_to_test1": {
					TransactionID: 11, // defined in the mock query to the DB above
				},
			})

			Expect(err).ShouldNot(HaveOccurred())
			Expect(unrealizable).To(HaveLen(2))
			Expect(unrealizable["role_to_test1"]).To(HaveLen(1))
			Expect(unrealizable["role_to_test1"][0]).To(BeEquivalentTo(
				"could not perform UPDATE_MEMBERSHIPS on role role_to_test1: unknown role 'role2'"))
			Expect(unrealizable["role_to_test2"]).To(HaveLen(1))
			Expect(unrealizable["role_to_test2"][0]).To(BeEquivalentTo(
				"could not perform DELETE on role role_to_test2: owner of database edbDatabase"))
		})
	})
})

var _ = DescribeTable("Role status tests",
	func(spec *apiv1.ManagedConfiguration, roles []DatabaseRole, expected map[string]apiv1.RoleStatus) {
		ctx := context.TODO()

		statusMap := evaluateNextRoleActions(ctx, spec, roles, map[string]apiv1.PasswordState{
			"roleWithChangedPassInSpec": {
				TransactionID:         101,
				SecretResourceVersion: "101B",
			},
			"roleWithChangedPassInDB": {
				TransactionID:         101,
				SecretResourceVersion: "101B",
			},
		},
			map[string]string{
				"roleWithChangedPassInSpec": "102B",
				"roleWithChangedPassInDB":   "101B",
			}).
			convertToRolesByStatus()

		// pivot the result to have a map: roleName -> Status, which is easier to compare for Ginkgo
		statusByRole := make(map[string]apiv1.RoleStatus)
		for action, roles := range statusMap {
			for _, role := range roles {
				statusByRole[role.Name] = action
			}
		}
		Expect(statusByRole).To(BeEquivalentTo(expected))
	},
	Entry("detects roles that are fully reconciled",
		&apiv1.ManagedConfiguration{
			Roles: []apiv1.RoleConfiguration{
				{
					Name:      "ensurePresent",
					Superuser: true,
					Ensure:    apiv1.EnsurePresent,
				},
				{
					Name:      "ensureAbsent",
					Superuser: true,
					Ensure:    apiv1.EnsureAbsent,
				},
			},
		},
		[]DatabaseRole{
			{
				Name:      "postgres",
				Superuser: true,
			},
			{
				Name:      "ensurePresent",
				Superuser: true,
				Inherit:   true,
			},
		},
		map[string]apiv1.RoleStatus{
			"ensurePresent": apiv1.RoleStatusReconciled,
			"ensureAbsent":  apiv1.RoleStatusReconciled,
			"postgres":      apiv1.RoleStatusReserved,
		},
	),
	Entry("detects roles that are not reconciled",
		&apiv1.ManagedConfiguration{
			Roles: []apiv1.RoleConfiguration{
				{
					Name:      "unwantedInDB",
					Superuser: true,
					Ensure:    apiv1.EnsureAbsent,
				},
				{
					Name:      "missingFromDB",
					Superuser: true,
					Ensure:    apiv1.EnsurePresent,
				},
				{
					Name:      "drifted",
					Superuser: true,
					Ensure:    apiv1.EnsurePresent,
				},
			},
		},
		[]DatabaseRole{
			{
				Name:      "postgres",
				Superuser: true,
			},
			{
				Name:      "unwantedInDB",
				Superuser: true,
			},
			{
				Name:      "drifted",
				Superuser: false,
			},
		},
		map[string]apiv1.RoleStatus{
			"postgres":      apiv1.RoleStatusReserved,
			"unwantedInDB":  apiv1.RoleStatusPendingReconciliation,
			"missingFromDB": apiv1.RoleStatusPendingReconciliation,
			"drifted":       apiv1.RoleStatusPendingReconciliation,
		},
	),
	Entry("detects roles that are not in the spec and ignores them",
		&apiv1.ManagedConfiguration{
			Roles: []apiv1.RoleConfiguration{
				{
					Name:      "edb_admin",
					Superuser: true,
					Ensure:    apiv1.EnsurePresent,
				},
			},
		},
		[]DatabaseRole{
			{
				Name:      "postgres",
				Superuser: true,
			},
			{
				Name:      "edb_admin",
				Superuser: true,
				Inherit:   true,
			},
			{
				Name:      "missingFromSpec",
				Superuser: false,
			},
		},
		map[string]apiv1.RoleStatus{
			"postgres":        apiv1.RoleStatusReserved,
			"edb_admin":       apiv1.RoleStatusReconciled,
			"missingFromSpec": apiv1.RoleStatusNotManaged,
		},
	),

	Entry("detects roles with changed passwords in the Database",
		&apiv1.ManagedConfiguration{
			Roles: []apiv1.RoleConfiguration{
				{
					Name:      "roleWithChangedPassInDB",
					Superuser: true,
					Ensure:    apiv1.EnsurePresent,
				},
			},
		},
		[]DatabaseRole{
			{
				Name:      "postgres",
				Superuser: true,
			},
			{
				Name:          "roleWithChangedPassInDB",
				Superuser:     true,
				transactionID: 102,
				Inherit:       true,
			},
		},
		map[string]apiv1.RoleStatus{
			"postgres":                apiv1.RoleStatusReserved,
			"roleWithChangedPassInDB": apiv1.RoleStatusPendingReconciliation,
		},
	),
	Entry("detects roles with changed passwords in the Spec",
		&apiv1.ManagedConfiguration{
			Roles: []apiv1.RoleConfiguration{
				{
					Name:      "roleWithChangedPassInSpec",
					Superuser: true,
					Ensure:    apiv1.EnsurePresent,
				},
			},
		},
		[]DatabaseRole{
			{
				Name:      "postgres",
				Superuser: true,
			},
			{
				Name:          "roleWithChangedPassInSpec",
				Superuser:     true,
				transactionID: 101,
				Inherit:       true,
			},
		},
		map[string]apiv1.RoleStatus{
			"postgres":                  apiv1.RoleStatusReserved,
			"roleWithChangedPassInSpec": apiv1.RoleStatusPendingReconciliation,
		},
	),
)

const (
	namespace          = "vinci-namespace"
	secretName         = "vinci-secret-name"
	secretNameNoUser   = "vinci-secret-no-user"
	secretNameNoPass   = "vinci-secret-no-pass"
	secretNameNotExist = "vinci-secret-name-not-exist"
	userNameNotExist   = "vinci-not-exist"
)

var (
	userName = rand.String(12)
	password = rand.String(12)
)

var _ = DescribeTable("role secrets test",
	func(
		roleConfig *apiv1.RoleConfiguration,
		expectedResult passwordSecret,
		expectError bool,
	) {
		// define various secrets as test cases to show failure modes
		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte(userName),
				corev1.BasicAuthPasswordKey: []byte(password),
			},
		}
		secretNoUser := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretNameNoUser,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				corev1.BasicAuthPasswordKey: []byte(password),
			},
		}
		secretNoPass := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretNameNoPass,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte(userName),
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&secret, &secretNoUser, &secretNoPass).
			Build()
		ctx := context.Background()
		decoded, err := getPassword(ctx, cl, roleConfigurationAdapter{RoleConfiguration: *roleConfig}, namespace)
		if expectError {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}

		Expect(decoded.username).To(Equal(expectedResult.username))
		Expect(decoded.password).To(Equal(expectedResult.password))
		if (expectedResult == passwordSecret{}) {
			Expect(decoded).To(BeZero())
		}
	},
	Entry("Can extract credentials on correct role secretName and secret content",
		&apiv1.RoleConfiguration{
			Name: userName,
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: secretName,
			},
		},
		passwordSecret{
			username: userName,
			password: password,
		},
		false,
	),
	Entry("Cannot extract credentials if role secretName is empty",
		&apiv1.RoleConfiguration{
			Name: userName,
		},
		passwordSecret{},
		false,
	),
	Entry("Cannot extract credentials if role's secretName does not match a secret",
		&apiv1.RoleConfiguration{
			Name: userName,
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: secretNameNotExist,
			},
		},
		passwordSecret{},
		false,
	),
	Entry("Throws error if secret username does not match role name",
		&apiv1.RoleConfiguration{
			Name: userNameNotExist,
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: secretName,
			},
		},
		passwordSecret{},
		true,
	),
	Entry("Throws error if configured secret does not contain a username",
		&apiv1.RoleConfiguration{
			Name: userName,
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: secretNameNoUser,
			},
		},
		passwordSecret{},
		true,
	),
	Entry("Throws error if configured secret does not contain a password",
		&apiv1.RoleConfiguration{
			Name: userName,
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: secretNameNoPass,
			},
		},
		passwordSecret{},
		true,
	),
)
