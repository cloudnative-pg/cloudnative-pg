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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
})

var _ = Describe("Parsing targetTime", func() {
	It("should calculate correctly the difference between two timestamps", func() {
		By("having the first time bigger than the second", func() {
			time1 := "2022-07-06T13:11:09.000000Z"
			time2 := "2022-07-06T13:11:07.000000Z"
			expectedSecondDifference := float64(2)
			difference, err := DifferenceBetweenTimestamps(time1, time2)
			Expect(err).ToNot(HaveOccurred())
			Expect(difference.Seconds()).To(Equal(expectedSecondDifference))
		})
		By("having the first time smaller than the second", func() {
			time1 := "2022-07-06T13:11:07.000000Z"
			time2 := "2022-07-06T13:11:09.000000Z"
			expectedSecondDifference := float64(-2)
			difference, err := DifferenceBetweenTimestamps(time1, time2)
			Expect(err).ToNot(HaveOccurred())
			Expect(difference.Seconds()).To(Equal(expectedSecondDifference))
		})
		By("having first or second time wrong", func() {
			time1 := "2022-07-06T13:12:09.000000Z"

			_, err := DifferenceBetweenTimestamps(time1, "")
			Expect(err).To(HaveOccurred())

			_, err = DifferenceBetweenTimestamps("", time1)
			Expect(err).To(HaveOccurred())
		})
	})

	It("should be RFC3339Micro format", func() {
		time1 := GetCurrentTimestamp()

		_, err := time.Parse(metav1.RFC3339Micro, time1)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("ToCompactISO8601", func() {
	It("should return a string in the expected format for a given time", func() {
		testTime := time.Date(2022, 0o1, 0o2, 15, 0o4, 0o5, 0, time.UTC)
		compactISO8601 := ToCompactISO8601(testTime)
		Expect(compactISO8601).To(Equal("20220102150405"))
	})

	It("should return a string of length 14", func() {
		testTime := time.Now()
		compactISO8601 := ToCompactISO8601(testTime)
		Expect(compactISO8601).To(HaveLen(14))
	})
})
