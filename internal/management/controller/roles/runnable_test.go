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
	"database/sql"
	"fmt"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type funcCall struct{ verb, roleName string }

type mockRoleManager struct {
	roles       map[string]DatabaseRole
	callHistory []funcCall
}

func (m *mockRoleManager) List(_ context.Context, _ *sql.DB) ([]DatabaseRole, error) {
	m.callHistory = append(m.callHistory, funcCall{"list", ""})
	re := make([]DatabaseRole, len(m.roles))
	i := 0
	for _, r := range m.roles {
		re[i] = r
		i++
	}
	return re, nil
}

func (m *mockRoleManager) Update(
	_ context.Context, _ *sql.DB, role DatabaseRole,
) error {
	m.callHistory = append(m.callHistory, funcCall{"update", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return fmt.Errorf("tring to update unknown role: %s", role.Name)
	}
	m.roles[role.Name] = role
	return nil
}

func (m *mockRoleManager) UpdateComment(
	_ context.Context, _ *sql.DB, role DatabaseRole,
) error {
	m.callHistory = append(m.callHistory, funcCall{"updateComment", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return fmt.Errorf("tring to update comment of unknown role: %s", role.Name)
	}
	m.roles[role.Name] = role
	return nil
}

func (m *mockRoleManager) Create(
	_ context.Context, _ *sql.DB, role DatabaseRole,
) error {
	m.callHistory = append(m.callHistory, funcCall{"create", role.Name})
	_, found := m.roles[role.Name]
	if found {
		return fmt.Errorf("tring to create existing role: %s", role.Name)
	}
	m.roles[role.Name] = role
	return nil
}

func (m *mockRoleManager) Delete(
	_ context.Context, _ *sql.DB, role DatabaseRole,
) error {
	m.callHistory = append(m.callHistory, funcCall{"delete", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return fmt.Errorf("tring to delete unknown role: %s", role.Name)
	}
	delete(m.roles, role.Name)
	return nil
}

func (m *mockRoleManager) GetLastTransactionID(_ context.Context, _ *sql.DB, _ DatabaseRole) (int64, error) {
	return 0, nil
}

func (m *mockRoleManager) UpdateMembership(
	_ context.Context,
	_ *sql.DB,
	role DatabaseRole,
	_ []string,
	_ []string,
) error {
	m.callHistory = append(m.callHistory, funcCall{"updateMembership", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return fmt.Errorf("trying to update Role Members of unknown role: %s", role.Name)
	}
	m.roles[role.Name] = role
	return nil
}

func (m *mockRoleManager) GetParentRoles(_ context.Context, _ *sql.DB, role DatabaseRole) ([]string, error) {
	m.callHistory = append(m.callHistory, funcCall{"getParentRoles", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return nil, fmt.Errorf("trying to get parent of unknown role: %s", role.Name)
	}
	m.roles[role.Name] = role
	return nil, nil
}

// mock.ExpectExec(unWantedRoleExpectedDelStmt).
// WillReturnError(&pgconn.PgError{Code: "2BP01"})

type fakeInstanceData struct {
	*postgres.Instance
	db *sql.DB
}

func (f *fakeInstanceData) GetSuperUserDB() (*sql.DB, error) {
	return f.db, nil
}

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

		rows := sqlmock.NewRows([]string{
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
			AddRow("ignored_role", true, false, true, true, true, false, -1, []byte("12345"),
				nil, false, []byte("This is a custom role in the DB"), 11, []byte("{}")).
			AddRow("role_to_update", true, true, false, false, false, false, -1, []byte("12345"),
				nil, false, []byte("This is a role to test with"), 11, []byte("{}"))
		expectedSelStmt := `SELECT rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, 
				rolcanlogin, rolreplication, rolconnlimit, rolpassword, rolvaliduntil, rolbypassrls,
			 pg_catalog.shobj_description(auth.oid, 'pg_authid') as comment, auth.xmin,
			 mem.inroles
	 FROM pg_catalog.pg_authid as auth
	 LEFT JOIN (
		 SELECT array_agg(pg_get_userbyid(roleid)) as inroles, member
		 FROM pg_auth_members GROUP BY member
	 ) mem ON member = oid
	 WHERE rolname not like 'pg\_%'`
		mock.ExpectQuery(expectedSelStmt).WillReturnRows(rows)

		roleSynchronizer = RoleSynchronizer{
			instance: &fakeInstanceData{
				Instance: postgres.NewInstance().WithNamespace("myPod"),
				db:       db,
			},
		}
	})

	When("role configurations are realizable", func() {
		It("it will Create ensure:present roles in spec missing from DB", func(ctx context.Context) {
			prm := NewPostgresRoleManager(db)

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
			passwordState, rolesWithErrors, err := roleSynchronizer.synchronizeRoles(ctx, prm, &managedConf,
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
			prm := NewPostgresRoleManager(db)

			_, _, err := roleSynchronizer.synchronizeRoles(ctx, prm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("it will call the necessary grants to update membership", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:      "role_to_update",
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
			mock.ExpectQuery(expectedMembershipStmt).WithArgs("role_to_update").WillReturnError(sql.ErrNoRows)
			mock.ExpectBegin()
			expectedMembershipExecs := []string{
				`GRANT "role1" TO "role_to_update"`,
				`GRANT "role2" TO "role_to_update"`,
			}

			for _, ex := range expectedMembershipExecs {
				mock.ExpectExec(ex).
					WillReturnResult(sqlmock.NewResult(2, 3))
			}

			mock.ExpectCommit()

			prm := NewPostgresRoleManager(db)
			_, rolesWithErrors, err := roleSynchronizer.synchronizeRoles(ctx, prm, &managedConf, map[string]apiv1.PasswordState{
				"role_to_update": {
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
						Name:            "role_to_update",
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
			prm := NewPostgresRoleManager(db)
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, prm, &managedConf, map[string]apiv1.PasswordState{
				"role_to_update": {
					TransactionID: 11, // defined in the mock query to the DB above
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("it will no-op if the roles are reconciled", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:            "role_to_update",
						Superuser:       true,
						Inherit:         ptr.To(true),
						Comment:         "This is a role to test with",
						ConnectionLimit: -1,
					},
				},
			}
			prm := NewPostgresRoleManager(db)
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, prm, &managedConf, map[string]apiv1.PasswordState{
				"role_to_update": {
					TransactionID: 11, // defined in the mock query to the DB above
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("it will Delete ensure:absent roles that are in the DB", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:   "role_to_update",
						Ensure: apiv1.EnsureAbsent,
					},
				},
			}
			roleDeletionStmt := fmt.Sprintf("DROP ROLE \"%s\"", "role_to_update")
			mock.ExpectExec(roleDeletionStmt).WillReturnResult(sqlmock.NewResult(2, 3))
			prm := NewPostgresRoleManager(db)
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, prm, &managedConf, map[string]apiv1.PasswordState{
				"role_to_update": {
					TransactionID: 11, // defined in the mock query to the DB above
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("it will Update ensure:present roles that are in the DB but have different fields", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:            "role_to_update",
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
				"role_to_update")
			mock.ExpectExec(alterStmt).WillReturnResult(sqlmock.NewResult(2, 3))
			rows := mock.NewRows([]string{"xmin"}).AddRow("12")
			lastTransactionQuery := "SELECT xmin FROM pg_catalog.pg_authid WHERE rolname = $1"
			mock.ExpectQuery(lastTransactionQuery).WithArgs("role_to_update").WillReturnRows(rows)
			prm := NewPostgresRoleManager(db)
			passwordState, rolesWithErrors, err := roleSynchronizer.synchronizeRoles(ctx, prm, &managedConf,
				map[string]apiv1.PasswordState{
					"role_to_update": {
						TransactionID: 11, // defined in the mock query to the DB above
					},
				})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rolesWithErrors).To(BeEmpty())
			Expect(passwordState).To(BeEquivalentTo(map[string]apiv1.PasswordState{
				"role_to_update": {
					TransactionID:         12,
					SecretResourceVersion: "",
				},
			}))
		})
	})

	When("role configurations are unrealizable", func() {
		It("it will record that updateMembership could not succeed", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:      "role_to_update",
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

			mock.ExpectQuery(expectedMembershipStmt).WithArgs("role_to_update").WillReturnError(sql.ErrNoRows)
			mock.ExpectBegin()

			mock.ExpectExec(`GRANT "role1" TO "role_to_update"`).
				WillReturnResult(sqlmock.NewResult(2, 3))

			postgresExpectedError := pgconn.PgError{
				Code:    "0LP01", // 0LP01 -> invalid_grant_operation
				Message: "unknown role 'role2'",
			}
			mock.ExpectExec(`GRANT "role2" TO "role_to_update"`).
				WillReturnError(&postgresExpectedError)

			mock.ExpectRollback()

			prm := NewPostgresRoleManager(db)
			_, unrealizable, err := roleSynchronizer.synchronizeRoles(ctx, prm, &managedConf, map[string]apiv1.PasswordState{
				"role_to_update": {
					TransactionID: 11, // defined in the mock query to the DB above
				},
			})

			Expect(err).ShouldNot(HaveOccurred())
			Expect(unrealizable).To(HaveLen(1))
			Expect(unrealizable["role_to_update"]).To(HaveLen(1))
			Expect(unrealizable["role_to_update"][0]).To(BeEquivalentTo(
				"could not perform UPDATE_MEMBERSHIPS on role role_to_update: unknown role 'role2'"))
		})

		It("it will record that Delete could not succeed", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:   "role_to_update",
						Ensure: apiv1.EnsureAbsent,
					},
				},
			}
			postgresExpectedError := pgconn.PgError{
				Code:   "2BP01", // 2BP01 -> dependent_objects_still_exist
				Detail: "owner of database edbDatabase",
			}

			roleDeletionStmt := fmt.Sprintf("DROP ROLE \"%s\"", "role_to_update")
			mock.ExpectExec(roleDeletionStmt).WillReturnError(&postgresExpectedError)
			prm := NewPostgresRoleManager(db)
			_, unrealizable, err := roleSynchronizer.synchronizeRoles(ctx, prm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(unrealizable).To(HaveLen(1))
			Expect(unrealizable["role_to_update"]).To(HaveLen(1))
			Expect(unrealizable["role_to_update"][0]).To(BeEquivalentTo(
				"could not perform DELETE on role role_to_update: owner of database edbDatabase"))
		})
	})
})

var _ = DescribeTable("Role status getter tests",
	func(spec *apiv1.ManagedConfiguration, rm mockRoleManager, expected map[string]apiv1.RoleStatus) {
		ctx := context.TODO()

		db := &sql.DB{}
		roles, err := rm.List(ctx, db)
		Expect(err).ToNot(HaveOccurred())

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
		mockRoleManager{
			roles: map[string]DatabaseRole{
				"postgres": {
					Name:      "postgres",
					Superuser: true,
				},
				"ensurePresent": {
					Name:      "ensurePresent",
					Superuser: true,
					Inherit:   true,
				},
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
		mockRoleManager{
			roles: map[string]DatabaseRole{
				"postgres": {
					Name:      "postgres",
					Superuser: true,
				},
				"unwantedInDB": {
					Name:      "unwantedInDB",
					Superuser: true,
				},
				"drifted": {
					Name:      "drifted",
					Superuser: false,
				},
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
		mockRoleManager{
			roles: map[string]DatabaseRole{
				"postgres": {
					Name:      "postgres",
					Superuser: true,
				},
				"edb_admin": {
					Name:      "edb_admin",
					Superuser: true,
					Inherit:   true,
				},
				"missingFromSpec": {
					Name:      "missingFromSpec",
					Superuser: false,
				},
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
		mockRoleManager{
			roles: map[string]DatabaseRole{
				"postgres": {
					Name:      "postgres",
					Superuser: true,
				},
				"roleWithChangedPassInDB": {
					Name:          "roleWithChangedPassInDB",
					Superuser:     true,
					transactionID: 102,
					Inherit:       true,
				},
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
		mockRoleManager{
			roles: map[string]DatabaseRole{
				"postgres": {
					Name:      "postgres",
					Superuser: true,
				},
				"roleWithChangedPassInSpec": {
					Name:          "roleWithChangedPassInSpec",
					Superuser:     true,
					transactionID: 101,
					Inherit:       true,
				},
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
	userName           = "vinci"
	password           = "vinci1234"
)

var _ = DescribeTable("role secrets test",
	func(
		roleConfig *apiv1.RoleConfiguration,
		expectedResult passwordSecret,
		expectError bool,
	) {
		// define various secrets as test cases to show failure modes
		secret := corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte(userName),
				corev1.BasicAuthPasswordKey: []byte(password),
			},
		}
		secretNoUser := corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      secretNameNoUser,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				corev1.BasicAuthPasswordKey: []byte(password),
			},
		}
		secretNoPass := corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
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
