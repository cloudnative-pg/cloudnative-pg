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

package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IsPowerOfTwo", func() {
	It("returns true for powers of two", func() {
		powersOfTwo := []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096}
		for _, n := range powersOfTwo {
			Expect(IsPowerOfTwo(n)).To(BeTrue(), "expected %d to be a power of two", n)
		}
	})

	It("returns false for non-powers of two", func() {
		nonPowersOfTwo := []int{3, 5, 6, 7, 9, 10, 12, 15, 100, 1000}
		for _, n := range nonPowersOfTwo {
			Expect(IsPowerOfTwo(n)).To(BeFalse(), "expected %d to not be a power of two", n)
		}
	})

	It("returns false for zero", func() {
		Expect(IsPowerOfTwo(0)).To(BeFalse(), "expected 0 to not be a power of two")
	})

	It("returns false for negative numbers", func() {
		negatives := []int{-1, -2, -4, -8, -1024}
		for _, n := range negatives {
			Expect(IsPowerOfTwo(n)).To(BeFalse(), "expected %d to not be a power of two", n)
		}
	})
})

var _ = Describe("ToBytes", func() {
	It("converts megabytes to bytes correctly", func() {
		Expect(ToBytes(1)).To(BeNumerically("==", 1048576))
		Expect(ToBytes(0)).To(BeNumerically("==", 0))
		Expect(ToBytes(100)).To(BeNumerically("==", 104857600))
		Expect(ToBytes(1024)).To(BeNumerically("==", 1073741824))
	})

	It("works with different numeric types", func() {
		Expect(ToBytes(int32(1))).To(BeNumerically("==", 1048576))
		Expect(ToBytes(int64(1))).To(BeNumerically("==", 1048576))
		Expect(ToBytes(uint(1))).To(BeNumerically("==", 1048576))
		Expect(ToBytes(float64(0.5))).To(BeNumerically("==", 524288))
	})
})
