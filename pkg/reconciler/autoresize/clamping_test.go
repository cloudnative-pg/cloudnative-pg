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

package autoresize

import (
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CalculateNewSize", func() {
	var (
		currentSize resource.Quantity
		policy      *apiv1.ExpansionPolicy
	)

	BeforeEach(func() {
		policy = &apiv1.ExpansionPolicy{}
	})

	Context("Percentage step expansion", func() {
		It("should expand by percentage of current size", func() {
			// 20% of 100Gi = 20Gi expansion, result = 120Gi
			currentSize = resource.MustParse("100Gi")
			policy.Step = intstr.IntOrString{Type: intstr.String, StrVal: "20%"}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("120Gi"))).To(Equal(0))
		})
	})

	Context("Absolute step expansion", func() {
		It("should expand by absolute quantity", func() {
			// 10Gi on 100Gi = 110Gi
			currentSize = resource.MustParse("100Gi")
			policy.Step = intstr.IntOrString{Type: intstr.String, StrVal: "10Gi"}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("110Gi"))).To(Equal(0))
		})
	})

	Context("MinStep clamping", func() {
		It("should clamp step to minStep when calculation is too small", func() {
			// 20% of 5Gi = 1Gi (too small), clamp to minStep 2Gi → result 7Gi
			currentSize = resource.MustParse("5Gi")
			policy.Step = intstr.IntOrString{Type: intstr.String, StrVal: "20%"}
			policy.MinStep = "2Gi"

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("7Gi"))).To(Equal(0))
		})
	})

	Context("MaxStep clamping", func() {
		It("should clamp step to maxStep when calculation is too large", func() {
			// 20% of 5000Gi = 1000Gi (too large), clamp to maxStep 500Gi → result 5500Gi
			currentSize = resource.MustParse("5000Gi")
			policy.Step = intstr.IntOrString{Type: intstr.String, StrVal: "20%"}
			policy.MaxStep = "500Gi"

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("5500Gi"))).To(Equal(0))
		})
	})

	Context("Limit cap", func() {
		It("should cap new size to limit", func() {
			// 20% of 90Gi = 18Gi, but limit 100Gi → result 100Gi
			currentSize = resource.MustParse("90Gi")
			policy.Step = intstr.IntOrString{Type: intstr.String, StrVal: "20%"}
			policy.Limit = "100Gi"

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("100Gi"))).To(Equal(0))
		})
	})

	Context("Absolute step with limit", func() {
		It("should cap result to limit when absolute step exceeds limit", func() {
			// 50Gi on 80Gi with limit 100Gi → result 100Gi
			currentSize = resource.MustParse("80Gi")
			policy.Step = intstr.IntOrString{Type: intstr.String, StrVal: "50Gi"}
			policy.Limit = "100Gi"

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("100Gi"))).To(Equal(0))
		})
	})

	Context("No limit", func() {
		It("should not cap size when limit is not set", func() {
			// 20% of 100Gi = 20Gi expansion, result = 120Gi (no limit)
			currentSize = resource.MustParse("100Gi")
			policy.Step = intstr.IntOrString{Type: intstr.String, StrVal: "20%"}
			policy.Limit = ""

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("120Gi"))).To(Equal(0))
		})
	})

	Context("Default step", func() {
		It("should use default step when policy.Step is zero value", func() {
			// zero value step uses defaults (20%, 2Gi min, 500Gi max)
			// 20% of 100Gi = 20Gi → result 120Gi
			currentSize = resource.MustParse("100Gi")
			// Step is zero value intstr.IntOrString{}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("120Gi"))).To(Equal(0))
		})

		It("should use default step when policy.Step is empty string", func() {
			// 20% of 100Gi = 20Gi → result 120Gi
			currentSize = resource.MustParse("100Gi")
			policy.Step = intstr.IntOrString{Type: intstr.String, StrVal: ""}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("120Gi"))).To(Equal(0))
		})
	})

	Context("Error handling", func() {
		It("should return error when policy is nil", func() {
			currentSize = resource.MustParse("100Gi")

			_, err := CalculateNewSize(currentSize, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expansion policy is nil"))
		})

		It("should return error with invalid percentage", func() {
			currentSize = resource.MustParse("100Gi")
			policy.Step = intstr.IntOrString{Type: intstr.String, StrVal: "invalid%"}

			_, err := CalculateNewSize(currentSize, policy)

			Expect(err).To(HaveOccurred())
		})

		It("should return error with invalid limit", func() {
			currentSize = resource.MustParse("100Gi")
			policy.Step = intstr.IntOrString{Type: intstr.String, StrVal: "10Gi"}
			policy.Limit = "invalid"

			_, err := CalculateNewSize(currentSize, policy)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse limit"))
		})

		It("should return error for integer step values", func() {
			currentSize = resource.MustParse("100Gi")
			policy.Step = intstr.FromInt(20)

			_, err := CalculateNewSize(currentSize, policy)

			Expect(err).To(HaveOccurred())
		})
	})
})
