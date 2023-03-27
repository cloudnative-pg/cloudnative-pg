package roles

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DatabaseRole implementation test", func() {
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

	It("should return Correct Role to grant/revoke", func() {
		rolesInDB := []string{"role1", "DBRole1", "DBRoleABC"}
		rolesInSpec := []string{"role1", "role2", "roleabc"}
		rolesToRevoke := getRolesToRevoke(rolesInDB, rolesInSpec)
		rolesToGrant := getRolesToGrant(rolesInDB, rolesInSpec)
		Expect(rolesToRevoke).To(BeEquivalentTo([]string{"DBRole1", "DBRoleABC"}))
		Expect(rolesToGrant).To(BeEquivalentTo([]string{"role2", "roleabc"}))
	})
})
