/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/
package postgres

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LSN handling functions", func() {
	Describe("Parse", func() {
		It("raises errors for invalid LSNs", func() {
			When("the syntax is not valid", func() {
				_, err := LSN("").Parse()
				Expect(err).ToNot(BeNil())

				_, err = LSN("/").Parse()
				Expect(err).ToNot(BeNil())
			})
			When("the number composing the LSNs are not valid", func() {
				_, err := LSN("28734982739847293874823974928738423/987429837498273498723984723").Parse()
				Expect(err).ToNot(BeNil())
			})
		})

		It("works for good LSNs", func() {
			Expect(LSN("1/1").Parse()).Should(Equal(int64(4294967297)))
			Expect(LSN("3/23").Parse()).Should(Equal(int64(12884901923)))
		})
	})
})
