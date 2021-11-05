/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parsing version", func() {
	instance := &Instance{}

	It("properly works when version is malformed", func() {
		_, err := instance.parseVersion("not-a-version")
		Expect(err).To(BeEquivalentTo(ErrMalformedServerVersion))
	})

	It("properly works when version is well-formed", func() {
		_, err := instance.parseVersion("13.4.8 Debian")
		Expect(err).To(BeNil())
	})
})
