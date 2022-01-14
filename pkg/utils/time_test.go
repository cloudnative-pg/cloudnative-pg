/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Time conversion", func() {
	It("properly works given a string in RFC3339 format", func() {
		res := ConvertToPostgresFormat("2021-09-01T10:22:47+03:00")
		Expect(res).To(BeEquivalentTo("2021-09-01 10:22:47.000000+03:00"))
	})
	It("return same input string if not in RFC3339 format", func() {
		res := ConvertToPostgresFormat("2001-09-29 01:02:03")
		Expect(res).To(BeEquivalentTo("2001-09-29 01:02:03"))
	})
})
