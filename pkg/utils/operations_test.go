/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Difference of values of maps", func() {
	p1 := make(map[string]string, 2)
	const testString = "test"
	p1["t"] = testString
	p1["r"] = "rest"
	It("is nil when maps contains same key/value pairs", func() {
		p2 := make(map[string]string, 2)
		p2["t"] = testString
		p2["r"] = "rest"
		Expect(CollectDifferencesFromMaps(p1, p2)).To(BeNil())
	})

	It("is a list of string with difference when maps contains different key/value pairs", func() {
		p2 := make(map[string]string, 2)
		p2["t"] = testString
		p2["a"] = "apple"
		res := CollectDifferencesFromMaps(p1, p2)
		Expect(len(res)).To(BeEquivalentTo(2))
	})
})
