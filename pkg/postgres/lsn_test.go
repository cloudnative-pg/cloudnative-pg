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

package postgres

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LSN handling functions", func() {
	Describe("Parse", func() {
		It("raises errors for invalid LSNs", func() {
			_, err := LSN("").Parse()
			Expect(err).To(HaveOccurred())
			_, err = LSN("/").Parse()
			Expect(err).To(HaveOccurred())
			_, err = LSN("28734982739847293874823974928738423/987429837498273498723984723").Parse()
			Expect(err).To(HaveOccurred())
		})

		It("works for good LSNs", func() {
			Expect(LSN("1/1").Parse()).Should(Equal(int64(4294967297)))
			Expect(LSN("3/23").Parse()).Should(Equal(int64(12884901923)))
			Expect(LSN("3BB/A9FFFBE8").Parse()).Should(Equal(int64(4104545893352)))
		})
	})

	Describe("Less", func() {
		It("handles errors in the same way as the zero LSN value", func() {
			Expect(LSN("").Less("3/23")).To(BeTrue())
			Expect(LSN("3/23").Less("")).To(BeFalse())
		})

		It("works correctly for good LSNs", func() {
			Expect(LSN("1/23").Less(LSN("1/24"))).To(BeTrue())
			Expect(LSN("1/24").Less(LSN("1/23"))).To(BeFalse())
			Expect(LSN("1/23").Less(LSN("2/23"))).To(BeTrue())
			Expect(LSN("2/23").Less(LSN("1/23"))).To(BeFalse())
		})
	})
})
