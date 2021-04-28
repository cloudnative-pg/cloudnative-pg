/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package configfile

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("configuration file parser", func() {
	It("return an empty list of lines when the content is empty", func() {
		Expect(SplitLines("")).To(Equal([]string{}))
		Expect(SplitLines("\n")).To(Equal([]string{}))
	})

	It("correctly split in lines", func() {
		Expect(SplitLines("one\ntwo\nthree\n")).To(Equal([]string{"one", "two", "three"}))
	})
})
