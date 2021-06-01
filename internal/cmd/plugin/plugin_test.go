/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package plugin

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("create client", func() {
	It("with given configuration", func() {
		err := createClient(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(Client).NotTo(BeNil())
	})
})
