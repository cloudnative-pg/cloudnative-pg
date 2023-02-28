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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type funcCall struct{ verb, roleName string }

type mockRoleManager struct {
	roles       map[string]apiv1.RoleConfiguration
	callHistory []funcCall
}

func (m *mockRoleManager) List(
	ctx context.Context, config *apiv1.ManagedConfiguration,
) ([]apiv1.RoleConfiguration, error) {
	m.callHistory = append(m.callHistory, funcCall{"list", ""})
	re := make([]apiv1.RoleConfiguration, len(m.roles))
	i := 0
	for _, r := range m.roles {
		re[i] = r
		i++
	}
	return re, nil
}

func (m *mockRoleManager) Update(
	ctx context.Context, role apiv1.RoleConfiguration,
) error {
	m.callHistory = append(m.callHistory, funcCall{"update", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return fmt.Errorf("tring to update unknown role: %s", role.Name)
	}
	m.roles[role.Name] = role
	return nil
}

func (m *mockRoleManager) Create(
	ctx context.Context, role apiv1.RoleConfiguration,
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
	ctx context.Context, role apiv1.RoleConfiguration,
) error {
	m.callHistory = append(m.callHistory, funcCall{"delete", role.Name})
	_, found := m.roles[role.Name]
	if !found {
		return fmt.Errorf("tring to delete unknown role: %s", role.Name)
	}
	delete(m.roles, role.Name)
	return nil
}

var _ = Describe("Role synchronizer tests", func() {
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
			roles: map[string]apiv1.RoleConfiguration{
				"postgres": {
					Name:      "postgres",
					Superuser: true,
				},
			},
		}
		err := synchronizeRoles(ctx, &rm, "myPod", &managedConf)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(rm.callHistory).To(ConsistOf(
			([]funcCall{
				{"list", ""},
				{"create", "edb_test"},
				{"create", "foo_bar"},
			}),
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
			roles: map[string]apiv1.RoleConfiguration{
				"postgres": {
					Name:      "postgres",
					Superuser: true,
				},
			},
		}
		err := synchronizeRoles(ctx, &rm, "myPod", &managedConf)
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
			roles: map[string]apiv1.RoleConfiguration{
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
		err := synchronizeRoles(ctx, &rm, "myPod", &managedConf)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(rm.callHistory).To(ConsistOf(funcCall{"list", ""}))
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
			roles: map[string]apiv1.RoleConfiguration{
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
		err := synchronizeRoles(ctx, &rm, "myPod", &managedConf)
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
			roles: map[string]apiv1.RoleConfiguration{
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
		err := synchronizeRoles(ctx, &rm, "myPod", &managedConf)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(rm.callHistory).To(ConsistOf(
			funcCall{"list", ""},
			funcCall{"update", "edb_test"},
		))
	})
})
