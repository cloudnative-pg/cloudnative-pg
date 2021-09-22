/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package stringset

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("String set", func() {
	It("starts as an empty set", func() {
		Expect(New().Len()).To(Equal(0))
	})

	It("starts with a list of strings", func() {
		Expect(From([]string{"one", "two"}).Len()).To(Equal(2))
		Expect(From([]string{"one", "two", "two"}).Len()).To(Equal(2))
	})

	It("store string keys", func() {
		set := New()
		Expect(set.Has("test")).To(BeFalse())
		Expect(set.Has("test2")).To(BeFalse())

		set.Put("test")
		Expect(set.Has("test")).To(BeTrue())
		Expect(set.Has("test2")).To(BeFalse())
	})
})
