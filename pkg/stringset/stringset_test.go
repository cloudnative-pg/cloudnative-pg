/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
