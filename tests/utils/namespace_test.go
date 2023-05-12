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

	It("should generate a new string slice without returning errors", func() {
		name := slice.generateUniqueName(testNamespace)
		Expect(name).ToNot(Equal(testNamespace))
		Expect(name).To(HavePrefix(testNamespace))
	})

	It("should work when invoked multiple times", func() {
		name := slice.generateUniqueName(testNamespace)
		Expect(name).ToNot(Equal(testNamespace))
		Expect(name).To(HavePrefix(testNamespace))

		nameTwo := slice.generateUniqueName(testNamespace)
		Expect(nameTwo).ToNot(Equal(testNamespace))
		Expect(nameTwo).To(HavePrefix(testNamespace))
		Expect(nameTwo).ToNot(Equal(name))
	})
})
