package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing uniqueStringSlice", func() {
	const testNamespace = "test-namespace"

	var slice uniqueStringSlice
	BeforeEach(func() {
		slice = uniqueStringSlice{}
	})

	It("should add a new string slice without returning errors", func() {
		err := slice.add(testNamespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should return an error if the name already exists", func() {
		slice.values = append(slice.values, testNamespace)

		err := slice.add(testNamespace)
		Expect(err).To(HaveOccurred())
	})

	It("should work when invoked multiple times", func() {
		err := slice.add(testNamespace)
		Expect(err).ToNot(HaveOccurred())

		err = slice.add("new-namespace")
		Expect(err).ToNot(HaveOccurred())
	})
})
