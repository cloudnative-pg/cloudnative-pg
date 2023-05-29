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

	"github.com/jackc/pgx/v5/pgconn"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (m *mockRoleManager) List(_ context.Context) ([]DatabaseRole, error) {
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
	_ context.Context, role DatabaseRole,
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
	_ context.Context, role DatabaseRole,
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
	_ context.Context, role DatabaseRole,
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
	_ context.Context, role DatabaseRole,
) error {
	m.callHistory = append(m.callHistory, funcCall{"delete", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return fmt.Errorf("tring to delete unknown role: %s", role.Name)
	}
	delete(m.roles, role.Name)
	return nil
}

func (m *mockRoleManager) GetLastTransactionID(_ context.Context, _ DatabaseRole) (int64, error) {
	return 0, nil
}

func (m *mockRoleManager) UpdateMembership(
	_ context.Context,
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

func (m *mockRoleManager) GetParentRoles(_ context.Context, role DatabaseRole) ([]string, error) {
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

type mockRoleManagerWithError struct {
	roles       map[string]DatabaseRole
	callHistory []funcCall
}

func (m *mockRoleManagerWithError) List(_ context.Context) ([]DatabaseRole, error) {
	m.callHistory = append(m.callHistory, funcCall{"list", ""})
	re := make([]DatabaseRole, len(m.roles))
	i := 0
	for _, r := range m.roles {
		re[i] = r
		i++
	}
	return re, nil
}

func (m *mockRoleManagerWithError) Update(
	_ context.Context, role DatabaseRole,
) error {
	m.callHistory = append(m.callHistory, funcCall{"update", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return fmt.Errorf("tring to update unknown role: %s", role.Name)
	}
	m.roles[role.Name] = role
	return nil
}

func (m *mockRoleManagerWithError) UpdateComment(
	_ context.Context, role DatabaseRole,
) error {
	m.callHistory = append(m.callHistory, funcCall{"updateComment", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return fmt.Errorf("tring to update comment of unknown role: %s", role.Name)
	}
	m.roles[role.Name] = role
	return nil
}

func (m *mockRoleManagerWithError) Create(
	_ context.Context, role DatabaseRole,
) error {
	m.callHistory = append(m.callHistory, funcCall{"create", role.Name})
	_, found := m.roles[role.Name]
	if found {
		return fmt.Errorf("tring to create existing role: %s", role.Name)
	}
	m.roles[role.Name] = role
	return nil
}

func (m *mockRoleManagerWithError) Delete(
	_ context.Context, role DatabaseRole,
) error {
	m.callHistory = append(m.callHistory, funcCall{"delete", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return fmt.Errorf("tring to delete unknown role: %s", role.Name)
	}
	return fmt.Errorf("could not delete role 'foo': %w",
		&pgconn.PgError{
			Code: "2BP01", Detail: "owner of database edbDatabase",
			Message: `role "dante" cannot be dropped because some objects depend on it`,
		})
}

func (m *mockRoleManagerWithError) GetLastTransactionID(_ context.Context, _ DatabaseRole) (int64, error) {
	return 0, nil
}

func (m *mockRoleManagerWithError) UpdateMembership(
	_ context.Context,
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
	return &pgconn.PgError{Code: "42704", Message: "unknown role 'blah'"}
}

func (m *mockRoleManagerWithError) GetParentRoles(_ context.Context, role DatabaseRole) ([]string, error) {
	m.callHistory = append(m.callHistory, funcCall{"getParentRoles", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return nil, fmt.Errorf("trying to get parent of unknown role: %s", role.Name)
	}
	m.roles[role.Name] = role
	return nil, nil
}

var _ = Describe("Role synchronizer tests", func() {
	roleSynchronizer := RoleSynchronizer{
		instance: &postgres.Instance{
			Namespace: "myPod",
		},
	}

	When("role configurations are realizable", func() {
		It("it will Create ensure:present roles in spec missing from DB", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:   "edb_test",
						Ensure: apiv1.EnsurePresent,
					},
					{
						Name:   "foo_bar",
						Ensure: apiv1.EnsurePresent,
					},
				},
			}
			rm := mockRoleManager{
				roles: map[string]DatabaseRole{
					"postgres": {
						Name:      "postgres",
						Superuser: true,
					},
				},
			}
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(
				[]funcCall{
					{"list", ""},
					{"create", "edb_test"},
					{"create", "foo_bar"},
				},
			))
			Expect(rm.callHistory).To(ConsistOf(
				funcCall{"list", ""},
				funcCall{"create", "edb_test"},
				funcCall{"create", "foo_bar"},
			))
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
			rm := mockRoleManager{
				roles: map[string]DatabaseRole{
					"postgres": {
						Name:      "postgres",
						Superuser: true,
					},
				},
			}

			_, _, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(funcCall{"list", ""}))
		})

		It("it will ignore DB roles that are not in spec", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:   "edb_test",
						Ensure: apiv1.EnsureAbsent,
					},
				},
			}
			rm := mockRoleManager{
				roles: map[string]DatabaseRole{
					"postgres": {
						Name:      "postgres",
						Superuser: true,
					},
					"ignorezMoi": {
						Name:      "ignorezMoi",
						Superuser: true,
					},
				},
			}
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(funcCall{"list", ""}))
		})

		It("it will call the updateMembership method", func(ctx context.Context) {
			trueValue := true
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:      "edb_test",
						Superuser: true,
						Inherit:   &trueValue,
						InRoles: []string{
							"role1",
							"role2",
						},
					},
				},
			}
			rm := mockRoleManager{
				roles: map[string]DatabaseRole{
					"edb_test": {
						Name:      "edb_test",
						Superuser: true,
						Inherit:   true,
					},
				},
			}
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(funcCall{"list", ""},
				funcCall{"getParentRoles", "edb_test"},
				funcCall{"updateMembership", "edb_test"}))
		})

		It("it will call the updateComment method", func(ctx context.Context) {
			trueValue := true
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:      "edb_test",
						Superuser: true,
						Inherit:   &trueValue,
						Comment:   "my comment",
					},
				},
			}
			rm := mockRoleManager{
				roles: map[string]DatabaseRole{
					"edb_test": {
						Name:      "edb_test",
						Superuser: true,
						Inherit:   true,
						Comment:   "my tailor is rich",
					},
				},
			}
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(funcCall{"list", ""},
				funcCall{"updateComment", "edb_test"}))
		})

		It("it will no-op if the roles are reconciled", func(ctx context.Context) {
			trueValue := true
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:      "edb_test",
						Superuser: true,
						Inherit:   &trueValue,
					},
				},
			}
			rm := mockRoleManager{
				roles: map[string]DatabaseRole{
					"edb_test": {
						Name:      "edb_test",
						Superuser: true,
						Inherit:   true,
					},
				},
			}
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(
				funcCall{"list", ""}))
		})

		It("it will Delete ensure:absent roles that are in the DB", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:   "edb_test",
						Ensure: apiv1.EnsureAbsent,
					},
				},
			}
			rm := mockRoleManager{
				roles: map[string]DatabaseRole{
					"postgres": {
						Name:      "postgres",
						Superuser: true,
					},
					"edb_test": {
						Name:      "edb_test",
						Superuser: true,
					},
				},
			}
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(
				funcCall{"list", ""},
				funcCall{"delete", "edb_test"},
			))
		})

		It("it will Update ensure:present roles that are in the DB but have different fields", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:      "edb_test",
						Ensure:    apiv1.EnsurePresent,
						CreateDB:  true,
						BypassRLS: true,
					},
				},
			}
			rm := mockRoleManager{
				roles: map[string]DatabaseRole{
					"postgres": {
						Name:      "postgres",
						Superuser: true,
					},
					"edb_test": {
						Name:      "edb_test",
						Superuser: true,
					},
				},
			}
			_, _, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(
				funcCall{"list", ""},
				funcCall{"update", "edb_test"},
			))
		})
	})

	When("role configurations are unrealizable", func() {
		It("it will record that updateMembership could not succeed", func(ctx context.Context) {
			trueValue := true
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:      "edb_test",
						Superuser: true,
						Inherit:   &trueValue,
						InRoles: []string{
							"role1",
							"role2",
						},
					},
				},
			}
			rm := mockRoleManagerWithError{
				roles: map[string]DatabaseRole{
					"edb_test": {
						Name:      "edb_test",
						Superuser: true,
						Inherit:   true,
					},
				},
			}
			_, unrealizable, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(funcCall{"list", ""},
				funcCall{"getParentRoles", "edb_test"},
				funcCall{"updateMembership", "edb_test"}))
			Expect(unrealizable).To(HaveLen(1))
			Expect(unrealizable["edb_test"]).To(HaveLen(1))
			Expect(unrealizable["edb_test"][0]).To(BeEquivalentTo(
				"could not perform UPDATE_MEMBERSHIPS on role edb_test: unknown role 'blah'"))
		})

		It("it will record that Delete could not succeed", func(ctx context.Context) {
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:   "edb_test",
						Ensure: apiv1.EnsureAbsent,
					},
				},
			}
			rm := mockRoleManagerWithError{
				roles: map[string]DatabaseRole{
					"postgres": {
						Name:      "postgres",
						Superuser: true,
					},
					"edb_test": {
						Name:      "edb_test",
						Superuser: true,
					},
				},
			}
			_, unrealizable, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(
				funcCall{"list", ""},
				funcCall{"delete", "edb_test"},
			))
			Expect(unrealizable).To(HaveLen(1))
			Expect(unrealizable["edb_test"]).To(HaveLen(1))
			Expect(unrealizable["edb_test"][0]).To(BeEquivalentTo(
				"could not perform DELETE on role edb_test: owner of database edbDatabase"))
		})

		It("it will continue the synchronization even if it finds errors", func(ctx context.Context) {
			trueValue := true
			managedConf := apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name:   "edb_test",
						Ensure: apiv1.EnsureAbsent,
					},
					{
						Name:      "another_test",
						Ensure:    apiv1.EnsurePresent,
						Superuser: true,
						Inherit:   &trueValue,
						InRoles: []string{
							"role1",
							"role2",
						},
					},
				},
			}
			rm := mockRoleManagerWithError{
				roles: map[string]DatabaseRole{
					"postgres": {
						Name:      "postgres",
						Superuser: true,
					},
					"edb_test": {
						Name:      "edb_test",
						Superuser: true,
					},
					"another_test": {
						Name:      "another_test",
						Superuser: true,
						Inherit:   true,
					},
				},
			}
			_, unrealizable, err := roleSynchronizer.synchronizeRoles(ctx, &rm, &managedConf, map[string]apiv1.PasswordState{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rm.callHistory).To(ConsistOf(
				funcCall{"list", ""},
				funcCall{"delete", "edb_test"},
				funcCall{"getParentRoles", "another_test"},
				funcCall{"updateMembership", "another_test"},
			))
			Expect(unrealizable).To(HaveLen(2))
			Expect(unrealizable["edb_test"]).To(HaveLen(1))
			Expect(unrealizable["edb_test"][0]).To(BeEquivalentTo(
				"could not perform DELETE on role edb_test: owner of database edbDatabase"))
			Expect(unrealizable["another_test"]).To(HaveLen(1))
			Expect(unrealizable["another_test"][0]).To(BeEquivalentTo(
				"could not perform UPDATE_MEMBERSHIPS on role another_test: unknown role 'blah'"))
		})
	})
})

var _ = DescribeTable("Role status getter tests",
	func(spec *apiv1.ManagedConfiguration, db mockRoleManager, expected map[string]apiv1.RoleStatus) {
		ctx := context.TODO()

		roles, err := db.List(ctx)
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
		decoded, err := getPassword(ctx, cl, *roleConfig, namespace)
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
