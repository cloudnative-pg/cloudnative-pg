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
