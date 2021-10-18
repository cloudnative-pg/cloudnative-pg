/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("parsing policy", func() {
	It("must properly parse a correct policy", func() {
		res, err := ParsePolicy("30w")
		Expect(res).To(BeEquivalentTo("RECOVERY WINDOW OF 30 WEEKS"))
		Expect(err).To(BeNil())
	})

	It("must complain with a wrong policy", func() {
		_, err := ParsePolicy("30")
		Expect(err).ToNot(BeNil())

		_, err = ParsePolicy("www")
		Expect(err).ToNot(BeNil())

		_, err = ParsePolicy("00d")
		Expect(err).ToNot(BeNil())
	})
})
