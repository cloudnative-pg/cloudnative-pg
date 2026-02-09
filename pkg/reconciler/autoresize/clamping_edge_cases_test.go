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

// These tests cover cross-field interactions in the clamping logic:
// minStep vs limit, maxStep vs limit, current size at limit, etc.
// They verify that the reconciler handles degenerate configurations
// safely even if the webhook doesn't reject them.

var _ = Describe("CalculateNewSize edge cases", func() {
	Context("minStep overshoots limit", func() {
		It("should cap to limit when minStep would exceed it", func() {
			// currentSize=9Gi, 10% step=0.9Gi → clamped to minStep=2Gi → 11Gi
			// But limit=10Gi, so result should be capped to 10Gi.
			currentSize := resource.MustParse("9Gi")
			policy := &apiv1.ExpansionPolicy{
				Step:    intstr.IntOrString{Type: intstr.String, StrVal: "10%"},
				MinStep: "2Gi",
				Limit:   "10Gi",
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("10Gi"))).To(Equal(0),
				"should cap to limit even when minStep would overshoot")
		})
	})

	Context("maxStep with small limit", func() {
		It("should cap to limit when maxStep-clamped step would exceed it", func() {
			// currentSize=50Gi, 50% step=25Gi → clamped to maxStep=10Gi → 60Gi
			// But limit=55Gi, so result should be 55Gi.
			currentSize := resource.MustParse("50Gi")
			policy := &apiv1.ExpansionPolicy{
				Step:    intstr.IntOrString{Type: intstr.String, StrVal: "50%"},
				MaxStep: "10Gi",
				Limit:   "55Gi",
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("55Gi"))).To(Equal(0),
				"limit should override maxStep-clamped expansion")
		})
	})

	Context("current size equals limit", func() {
		It("should return current size when already at limit", func() {
			// currentSize=100Gi, step=20%, limit=100Gi
			// Expansion would yield 120Gi but limit caps to 100Gi.
			// The reconciler should detect newSize <= currentSize and skip resize.
			currentSize := resource.MustParse("100Gi")
			policy := &apiv1.ExpansionPolicy{
				Step:  intstr.IntOrString{Type: intstr.String, StrVal: "20%"},
				Limit: "100Gi",
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			// CalculateNewSize caps to limit, so newSize == limit == currentSize
			Expect(newSize.Cmp(currentSize)).To(Equal(0),
				"should not grow beyond limit")
		})
	})

	Context("current size exceeds limit (degenerate config)", func() {
		It("should return current size when limit is smaller than current size", func() {
			// currentSize=20Gi, step=20%=4Gi → 24Gi, but limit=10Gi
			// The limit cap sets newSize=10Gi, but 10Gi < currentSize
			// so the reconciler would see newSize < currentSize.
			// CalculateNewSize itself just returns the limit.
			currentSize := resource.MustParse("20Gi")
			policy := &apiv1.ExpansionPolicy{
				Step:  intstr.IntOrString{Type: intstr.String, StrVal: "20%"},
				Limit: "10Gi",
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			// CalculateNewSize returns the limit value; the reconciler
			// should compare and skip the resize because newSize < currentSize.
			Expect(newSize.Cmp(resource.MustParse("10Gi"))).To(Equal(0),
				"should cap to limit even when limit < currentSize")
			Expect(newSize.Cmp(currentSize)).To(BeNumerically("<", 0),
				"limit < currentSize means reconciler should not issue a PVC patch")
		})
	})

	Context("100% step doubles the volume", func() {
		It("should double the volume with 100% step", func() {
			currentSize := resource.MustParse("10Gi")
			policy := &apiv1.ExpansionPolicy{
				Step: intstr.IntOrString{Type: intstr.String, StrVal: "100%"},
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("20Gi"))).To(Equal(0),
				"100% step should double the volume")
		})
	})

	Context("1% step on small volume (minStep kicks in)", func() {
		It("should use default minStep when 1% step is tiny", func() {
			// 1% of 10Gi = 100Mi, but default minStep is 2Gi
			currentSize := resource.MustParse("10Gi")
			policy := &apiv1.ExpansionPolicy{
				Step: intstr.IntOrString{Type: intstr.String, StrVal: "1%"},
				// MinStep defaults to 2Gi
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			// 1% of 10Gi = ~107Mi, default minStep 2Gi kicks in
			Expect(newSize.Cmp(resource.MustParse("12Gi"))).To(Equal(0),
				"default minStep (2Gi) should override tiny percentage step")
		})
	})

	Context("absolute step ignores minStep and maxStep", func() {
		It("should use exact absolute step even when smaller than minStep", func() {
			// Absolute step 512Mi, minStep=2Gi — minStep is ignored.
			currentSize := resource.MustParse("10Gi")
			policy := &apiv1.ExpansionPolicy{
				Step:    intstr.IntOrString{Type: intstr.String, StrVal: "512Mi"},
				MinStep: "2Gi",
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			// 10Gi + 512Mi = 10.5Gi
			expected := resource.MustParse("10752Mi") // 10Gi = 10240Mi + 512Mi = 10752Mi
			Expect(newSize.Cmp(expected)).To(Equal(0),
				"absolute step should not be clamped by minStep")
		})

		It("should use exact absolute step even when larger than maxStep", func() {
			// Absolute step 100Gi, maxStep=10Gi — maxStep is ignored.
			currentSize := resource.MustParse("100Gi")
			policy := &apiv1.ExpansionPolicy{
				Step:    intstr.IntOrString{Type: intstr.String, StrVal: "100Gi"},
				MaxStep: "10Gi",
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("200Gi"))).To(Equal(0),
				"absolute step should not be clamped by maxStep")
		})
	})

	Context("minStep equals maxStep (fixed step size)", func() {
		It("should use the fixed step regardless of percentage calculation", func() {
			// 20% of 1000Gi = 200Gi, but min=max=5Gi forces exactly 5Gi step
			currentSize := resource.MustParse("1000Gi")
			policy := &apiv1.ExpansionPolicy{
				Step:    intstr.IntOrString{Type: intstr.String, StrVal: "20%"},
				MinStep: "5Gi",
				MaxStep: "5Gi",
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			Expect(newSize.Cmp(resource.MustParse("1005Gi"))).To(Equal(0),
				"when minStep == maxStep, the step is fixed regardless of percentage")
		})
	})

	Context("very large volumes", func() {
		It("should handle terabyte-scale volumes", func() {
			currentSize := resource.MustParse("10Ti")
			policy := &apiv1.ExpansionPolicy{
				Step:    intstr.IntOrString{Type: intstr.String, StrVal: "10%"},
				MaxStep: "2Ti", // Allow 1Ti step (10% of 10Ti) without clamping
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			expected := resource.MustParse("11Ti")
			Expect(newSize.Cmp(expected)).To(Equal(0))
		})
	})

	Context("very small volumes", func() {
		It("should handle megabyte-scale volumes with default minStep", func() {
			// 20% of 100Mi = 20Mi, but default minStep is 2Gi
			currentSize := resource.MustParse("100Mi")
			policy := &apiv1.ExpansionPolicy{
				Step: intstr.IntOrString{Type: intstr.String, StrVal: "20%"},
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			// Default minStep (2Gi) is much larger than 20% of 100Mi (20Mi).
			// Result should be 100Mi + 2Gi ≈ 2148Mi
			Expect(newSize.Value()).To(BeNumerically(">", currentSize.Value()+2*1024*1024*1024-1024*1024),
				"default minStep should dominate for tiny volumes")
		})
	})

	Context("invalid minStep/maxStep fallback", func() {
		It("should fall back to defaults when minStep is invalid", func() {
			// Invalid minStep should fall back to default "2Gi"
			currentSize := resource.MustParse("5Gi")
			policy := &apiv1.ExpansionPolicy{
				Step:    intstr.IntOrString{Type: intstr.String, StrVal: "20%"},
				MinStep: "not-a-quantity",
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			// 20% of 5Gi = 1Gi, but default minStep fallback (2Gi) should apply
			Expect(newSize.Cmp(resource.MustParse("7Gi"))).To(Equal(0),
				"invalid minStep should fall back to default 2Gi")
		})

		It("should fall back to defaults when maxStep is invalid", func() {
			// Invalid maxStep should fall back to default "500Gi"
			currentSize := resource.MustParse("5000Gi")
			policy := &apiv1.ExpansionPolicy{
				Step:    intstr.IntOrString{Type: intstr.String, StrVal: "20%"},
				MaxStep: "not-a-quantity",
			}

			newSize, err := CalculateNewSize(currentSize, policy)

			Expect(err).NotTo(HaveOccurred())
			// 20% of 5000Gi = 1000Gi, but default maxStep fallback (500Gi) should cap
			Expect(newSize.Cmp(resource.MustParse("5500Gi"))).To(Equal(0),
				"invalid maxStep should fall back to default 500Gi")
		})
	})
})
