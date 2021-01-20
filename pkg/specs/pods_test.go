/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("The PostgreSQL security context", func() {
	securityContext := CreatePostgresSecurityContext(26, 26)

	It("allows the container to create its own PGDATA", func() {
		Expect(securityContext.RunAsUser).To(Equal(securityContext.FSGroup))
	})
})
