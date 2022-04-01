/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package plugin

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("create client", func() {
	It("with given configuration", func() {
		err := createClient(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(Client).NotTo(BeNil())
	})
})
