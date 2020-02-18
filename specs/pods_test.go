/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("The PostgreSQL security context", func() {
	securityContext := CreatePostgresSecurityContext()

	It("allows the container to create its own PGDATA", func() {
		Expect(securityContext.RunAsUser).To(Equal(securityContext.FSGroup))
	})
})
