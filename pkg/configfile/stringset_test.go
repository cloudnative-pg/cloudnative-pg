/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package configfile

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("String set", func() {
	It("starts as an empty set", func() {
		Expect(NewStringSet().Len()).To(Equal(0))
	})

	It("store string keys", func() {
		set := NewStringSet()
		Expect(set.Has("test")).To(BeFalse())
		Expect(set.Has("test2")).To(BeFalse())

		set.Put("test")
		Expect(set.Has("test")).To(BeTrue())
		Expect(set.Has("test2")).To(BeFalse())
	})
})
