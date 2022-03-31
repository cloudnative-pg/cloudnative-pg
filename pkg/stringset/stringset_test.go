/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package stringset

import (
	. "github.com/onsi/ginkgo/v2"
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

	It("removes string keys", func() {
		set := From([]string{"one", "two"})
		set.Delete("one")
		Expect(set.ToList()).To(Equal([]string{"two"}))
	})

	It("constructs a string slice given a set", func() {
		Expect(From([]string{"one", "two"}).ToList()).To(ContainElements("one", "two"))
	})
})
