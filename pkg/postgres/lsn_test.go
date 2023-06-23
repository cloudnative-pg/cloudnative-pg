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

	Describe("Diff", func() {
		Context("when the LSNs can be parsed to int64", func() {
			It("should return the difference of the LSNs", func() {
				lsn1 := LSN("1/10")
				lsn2 := LSN("1/B")
				res := lsn1.Diff(lsn2)
				Expect(res).NotTo(BeNil())
				Expect(*res).To(Equal(int64(5)))
			})
		})

		Context("when the LSNs cannot be parsed to int64", func() {
			It("should return nil", func() {
				lsn1 := LSN("1/10")
				lsn2 := LSN("wrong_format")
				res := lsn1.Diff(lsn2)
				Expect(res).To(BeNil())
			})

			It("should return nil when LSN is empty", func() {
				lsn1 := LSN("1/10")
				lsn2 := LSN("")
				res := lsn1.Diff(lsn2)
				Expect(res).To(BeNil())
			})
		})
	})
})
