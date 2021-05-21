/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package configfile

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Connection string parameter escaper", func() {
	It("works correctly when we don't need to escape values", func() {
		Expect(escapeConnectionStringParameter("one", "1")).To(Equal("one='1'"))
	})

	It("escapes values when they contain spaces", func() {
		Expect(escapeConnectionStringParameter("one", "1 ")).To(Equal("one='1 '"))
		Expect(escapeConnectionStringParameter("two", " 2")).To(Equal("two=' 2'"))
	})

	It("escapes values when they are empty", func() {
		Expect(escapeConnectionStringParameter("empty", "")).To(Equal("empty=''"))
	})

	It("works correctly when the apostrophe character is detected", func() {
		Expect(escapeConnectionStringParameter("one", "'hey'")).To(Equal("one='''hey'''"))
	})
})

var _ = Describe("Connection string generator", func() {
	It("works with zero items", func() {
		Expect(CreateConnectionString(map[string]string{})).To(Equal(""))
	})

	It("works with one item", func() {
		Expect(CreateConnectionString(
			map[string]string{
				"one": "1",
			})).To(Equal("one='1'"))
	})

	It("works with two items", func() {
		Expect(CreateConnectionString(
			map[string]string{
				"one": "1",
				"two": "2",
			})).To(Equal("one='1' two='2'"))
	})
})
