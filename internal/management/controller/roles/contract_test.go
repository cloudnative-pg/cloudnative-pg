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
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DatabaseRole implementation test", func() {
	fixedTime := time.Date(2023, 4, 4, 0, 0, 0, 0, time.UTC)
	fixedTime2 := time.Date(2023, 4, 4, 1, 0, 0, 0, time.UTC)
	It("should return true when the objects are equal", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
		}
		config := apiv1.RoleConfiguration{Name: "abc"}
		res := role.isEquivalentTo(config)
		Expect(res).To(BeTrue())
	})

	It("should return true when the objects are equal except inRoles and roleGrants", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
			InRoles: []string{
				"Userrole2", "Testxxx",
			},
			RoleGrants: []DatabaseRoleGrant{
				{
					Name:    "role1",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
			},
		}
		config := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "TestroleABC",
			},
			RoleGrants: []apiv1.RoleGrant{
				{
					Name:    "role1",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
				{Name: "role2"},
			},
		}
		res := role.isEquivalentTo(config)
		Expect(res).To(BeTrue())
	})

	It("should return false when the objects aren't equal", func() {
		role := DatabaseRole{Name: "abc", Inherit: true}
		config := apiv1.RoleConfiguration{Name: "def"}
		res := role.isEquivalentTo(config)
		Expect(res).To(BeFalse())
	})

	It("should return true when the inRoles and roleGrants are same but not same order", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
			InRoles: []string{
				"TestroleABC",
				"Userrole2",
			},
			RoleGrants: []DatabaseRoleGrant{
				{
					Name:    "role1",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
				{
					Name:    "role3",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
			},
		}
		config := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "TestroleABC",
			},
			RoleGrants: []apiv1.RoleGrant{
				{
					Name:    "role3",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
				{
					Name:    "role1",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
			},
		}
		res := role.hasMatchingRoleGrants(config)
		Expect(res).To(BeTrue())
	})

	It("should return true when all inRoles are present in roleGrants, ignoring specific options", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
			InRoles: []string{},
			RoleGrants: []DatabaseRoleGrant{
				{
					Name:    "role1",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
				{
					Name:    "role3",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
				{
					Name:    "TestroleABC",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
				{
					Name:    "Userrole2",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
			},
		}
		config := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "TestroleABC",
			},
			RoleGrants: []apiv1.RoleGrant{
				{
					Name:    "role3",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
				{
					Name:    "role1",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
			},
		}
		res := role.hasMatchingRoleGrants(config)
		Expect(res).To(BeTrue())
	})

	It("should return false when the inRoles and roleGrants are not equal", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
			InRoles: []string{
				"Userrole2", "TestroleABC",
			},
			RoleGrants: []DatabaseRoleGrant{
				{
					Name:    "role1",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
			},
		}
		configInRolesWrong := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "TestroleABCx",
			},
			RoleGrants: []apiv1.RoleGrant{
				{
					Name:    "role1",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
			},
		}
		configRoleGrantsWrong := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "TestroleABC",
			},
			RoleGrants: []apiv1.RoleGrant{
				{
					Name:    "role1x",
					Admin:   ptr.To(true),
					Inherit: ptr.To(true),
					Set:     ptr.To(false),
				},
			},
		}
		resInRoles := role.hasMatchingRoleGrants(configInRolesWrong)
		Expect(resInRoles).To(BeFalse())

		resRoleGrants := role.hasMatchingRoleGrants(configRoleGrantsWrong)
		Expect(resRoleGrants).To(BeFalse())
	})

	It("Detects that spec and db role have the same ValidUntil", func() {
		role := DatabaseRole{
			Name:       "abc",
			ValidUntil: pgtype.Timestamp{Valid: true, Time: fixedTime},
		}
		inSpec := apiv1.RoleConfiguration{
			Name:       "abc",
			ValidUntil: &metav1.Time{Time: fixedTime},
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeTrue())
	})

	It("Detects both database and spec don't have a VALID UNTIL", func() {
		role := DatabaseRole{
			Name: "abc",
		}
		inSpec := apiv1.RoleConfiguration{
			Name: "abc",
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeTrue())
	})

	It("Detects the VALID UNTIL has drifted", func() {
		role := DatabaseRole{
			Name:       "abc",
			ValidUntil: pgtype.Timestamp{Valid: true, Time: fixedTime},
		}
		inSpec := apiv1.RoleConfiguration{
			Name:       "abc",
			ValidUntil: &metav1.Time{Time: fixedTime2},
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeFalse())
	})

	It("Detects difference in VALID UNTIL if db has it but spec does not", func() {
		role := DatabaseRole{
			Name:       "abc",
			ValidUntil: pgtype.Timestamp{Valid: true, Time: fixedTime},
		}
		inSpec := apiv1.RoleConfiguration{
			Name: "abc",
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeFalse())
	})

	It("Detects difference in VALID UNTIL if spec has it but db does not", func() {
		role := DatabaseRole{
			Name: "abc",
		}
		inSpec := apiv1.RoleConfiguration{
			Name:       "abc",
			ValidUntil: &metav1.Time{Time: fixedTime2},
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeFalse())
	})

	It("Detects that spec and db role have never-expiring passwords", func() {
		role := DatabaseRole{
			Name:       "abc",
			ValidUntil: pgtype.Timestamp{Valid: true, Time: time.Time{}, InfinityModifier: pgtype.Infinity},
		}
		inSpec := apiv1.RoleConfiguration{
			Name:       "abc",
			ValidUntil: nil,
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeTrue())
	})

	It("translates a removed validUntil to infinity when the role already had one", func() {
		role := roleConfigurationAdapter{
			RoleConfiguration:        apiv1.RoleConfiguration{Name: "foo"},
			validUntilNullIsInfinity: true,
		}.toDatabaseRole()
		Expect(role.ValidUntil.Valid).To(BeTrue())
		Expect(role.ValidUntil.InfinityModifier).To(Equal(pgtype.Infinity))
	})

	It("leaves a removed validUntil unset when the role had none", func() {
		role := roleConfigurationAdapter{
			RoleConfiguration: apiv1.RoleConfiguration{Name: "foo"},
		}.toDatabaseRole()
		Expect(role.ValidUntil.Valid).To(BeFalse())
	})

	It("should return Correct Role to grant/revoke", func() {
		rolesInDB := []DatabaseRoleGrant{
			{
				Name:    "role1",
				Admin:   ptr.To(true),
				Inherit: ptr.To(true),
				Set:     ptr.To(false),
			}, {
				Name:    "DBRole1",
				Admin:   ptr.To(true),
				Inherit: ptr.To(true),
				Set:     ptr.To(false),
			}, {
				Name:    "DBRoleABC",
				Admin:   ptr.To(true),
				Inherit: ptr.To(true),
				Set:     ptr.To(false),
			}, {
				Name:    "role_with_explicit_set_option",
				Admin:   ptr.To(true),
				Inherit: ptr.To(true),
				Set:     ptr.To(false),
			}}
		rolesInSpec := []DatabaseRoleGrant{
			{
				Name: "role1",
			}, {
				Name: "role2",
			}, {
				Name: "roleabc",
			}, {
				Name: "role_with_explicit_set_option",
				Set:  ptr.To(true),
			}}
		rolesToRevoke := getRolesToRevoke(rolesInDB, rolesInSpec)
		rolesToGrant := getRolesToGrant(rolesInDB, rolesInSpec)
		Expect(rolesToRevoke).To(BeEquivalentTo([]string{"DBRole1", "DBRoleABC"}))
		Expect(rolesToGrant).To(BeEquivalentTo([]DatabaseRoleGrant{{Name: "role2"}, {Name: "roleabc"}, {
			Name: "role_with_explicit_set_option",
			Set:  ptr.To(true),
		}}))
	})
})
