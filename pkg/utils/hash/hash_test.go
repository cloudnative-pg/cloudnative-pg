/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package hash

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Hashing", func() {
	Context("ComputeHash", func() {
		It("should compute a hash for a given object", func() {
			object := "test"
			hash, err := ComputeHash(object)
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).NotTo(BeEmpty())
		})
	})

	Context("ComputeVersionedHash", func() {
		It("should compute a versioned hash for a given object and epoc", func() {
			object := "versioned-test"
			epoc := 1
			hash, err := ComputeVersionedHash(object, epoc)
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).NotTo(BeEmpty())
		})

		It("should return different hashes for different epocs", func() {
			object := "consistent"
			hash1, err1 := ComputeVersionedHash(object, 1)
			hash2, err2 := ComputeVersionedHash(object, 2)
			Expect(err1).NotTo(HaveOccurred())
			Expect(err2).NotTo(HaveOccurred())
			Expect(hash1).NotTo(Equal(hash2))
		})
	})
})
