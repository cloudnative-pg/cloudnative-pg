package roles

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DatabaseRole implementation test", func() {
	It("should return true when the objects are equal", func() {
		role := DatabaseRole{Name: "abc", Inherit: true}
		config := apiv1.RoleConfiguration{Name: "abc"}
		res := role.isEquivalent(config)
		Expect(res).To(BeTrue())
	})

	It("should return false when the objects aren't equal", func() {
		role := DatabaseRole{Name: "abc", Inherit: true}
		config := apiv1.RoleConfiguration{Name: "def"}
		res := role.isEquivalent(config)
		Expect(res).To(BeFalse())
	})
})
