/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package configfile

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("configuration file parser", func() {
	It("return an empty list of lines when the content is empty", func() {
		Expect(splitLines("")).To(Equal([]string{}))
		Expect(splitLines("\n")).To(Equal([]string{}))
	})

	It("correctly split in lines", func() {
		Expect(splitLines("one\ntwo\nthree\n")).To(Equal([]string{"one", "two", "three"}))
	})
})
