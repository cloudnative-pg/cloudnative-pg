/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Secret creation", func() {
	It("create a secret with the right user and password", func() {
		secret := CreateSecret("name", "namespace", "thisuser", "thispassword")
		Expect(secret.Name).To(Equal("name"))
		Expect(secret.Namespace).To(Equal("namespace"))
		Expect(secret.StringData["username"]).To(Equal("thisuser"))
		Expect(secret.StringData["password"]).To(Equal("thispassword"))
	})
})
