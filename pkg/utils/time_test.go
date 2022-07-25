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

var _ = Describe("Time conversion", func() {
	It("properly works given a string in RFC3339 format", func() {
		res := ConvertToPostgresFormat("2021-09-01T10:22:47+03:00")
		Expect(res).To(BeEquivalentTo("2021-09-01 10:22:47.000000+03:00"))
	})

	It("return same input string if not in RFC3339 format", func() {
		res := ConvertToPostgresFormat("2001-09-29 01:02:03")
		Expect(res).To(BeEquivalentTo("2001-09-29 01:02:03"))
	})

	It("properly works given a string in RFC3339Micro format", func() {
		res := ConvertToPostgresFormat("2022-07-25T13:30:21.166753Z")
		Expect(res).To(Equal("2022-07-25 13:30:21.166753Z"))
	})
})

var _ = Describe("Parsing targetTime", func() {
	It("parsing works given targetTime in `YYYY-MM-DD HH24:MI:SS` format", func() {
		res, err := ParseTargetTime(nil, "2021-09-01 10:22:47")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.MarshalText()).To(BeEquivalentTo("2021-09-01T10:22:47Z"))
	})
	It("parsing works given targetTime in `YYYY-MM-DD HH24:MI:SS.FF6TZH` format", func() {
		res, err := ParseTargetTime(nil, "2021-09-01 10:22:47.000000+06")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.MarshalText()).To(BeEquivalentTo("2021-09-01T10:22:47+06:00"))
	})
	It("parsing works given targetTime in `YYYY-MM-DD HH24:MI:SS.FF6TZH:TZM` format", func() {
		res, err := ParseTargetTime(nil, "2021-09-01 10:22:47.000000+06:00")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.MarshalText()).To(BeEquivalentTo("2021-09-01T10:22:47+06:00"))
	})
	It("parsing works given targetTime in `YYYY-MM-DDTHH24:MI:SSZ` format", func() {
		res, err := ParseTargetTime(nil, "2021-09-01T10:22:47Z")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.MarshalText()).To(BeEquivalentTo("2021-09-01T10:22:47Z"))
	})
	It("parsing works given targetTime in `YYYY-MM-DDTHH24:MI:SS±TZH:TZM` format", func() {
		res, err := ParseTargetTime(nil, "2021-09-01T10:22:47+00:00")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.MarshalText()).To(BeEquivalentTo("2021-09-01T10:22:47Z"))
	})
	It("parsing works given targetTime in `YYYY-MM-DDTHH24:MI:SS` format", func() {
		res, err := ParseTargetTime(nil, "2021-09-01T10:22:47")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.MarshalText()).To(BeEquivalentTo("2021-09-01T10:22:47Z"))
	})
	It("should calculate correctly the difference between two timestamps", func() {
		By("having the first time bigger than the second", func() {
			time1 := "2022-07-06T13:11:09Z"
			time2 := "2022-07-06T13:11:07Z"
			expectedSecondDifference := float64(2)
			difference, err := DifferenceBetweenTimestamps(time1, time2)
			Expect(err).To(BeNil())
			Expect(difference.Seconds()).To(Equal(expectedSecondDifference))
		})
		By("having the first time smaller than the second", func() {
			time1 := "2022-07-06T13:11:07Z"
			time2 := "2022-07-06T13:11:09Z"
			expectedSecondDifference := float64(-2)
			difference, err := DifferenceBetweenTimestamps(time1, time2)
			Expect(err).To(BeNil())
			Expect(difference.Seconds()).To(Equal(expectedSecondDifference))
		})
	})
})
