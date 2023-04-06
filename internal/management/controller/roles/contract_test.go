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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

	It("should return true when the objects are equal except inRoles", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
			InRoles: []string{
				"role1", "Userrole2", "Testxxx",
			},
		}
		config := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "role1", "TestroleABC",
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

	It("should return true when the inRole are same but not same order", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
			InRoles: []string{
				"role1", "Userrole2", "TestroleABC",
			},
		}
		config := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "role1", "TestroleABC",
			},
		}
		res := role.isInSameRolesAs(config)
		Expect(res).To(BeTrue())
	})

	It("should return false when the in roles are not equal", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
			InRoles: []string{
				"role1", "Userrole2", "TestroleABC",
			},
		}
		config := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "role1x", "TestroleABC",
			},
		}
		res := role.isInSameRolesAs(config)
		Expect(res).To(BeFalse())
	})

	It("Detects that spec and db role have the same ValidUntil", func() {
		role := DatabaseRole{
			Name:       "abc",
			ValidUntil: &fixedTime,
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
			ValidUntil: &fixedTime,
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
			ValidUntil: &fixedTime,
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

	It("should return Correct Role to grant/revoke", func() {
		rolesInDB := []string{"role1", "DBRole1", "DBRoleABC"}
		rolesInSpec := []string{"role1", "role2", "roleabc"}
		rolesToRevoke := getRolesToRevoke(rolesInDB, rolesInSpec)
		rolesToGrant := getRolesToGrant(rolesInDB, rolesInSpec)
		Expect(rolesToRevoke).To(BeEquivalentTo([]string{"DBRole1", "DBRoleABC"}))
		Expect(rolesToGrant).To(BeEquivalentTo([]string{"role2", "roleabc"}))
	})
})
