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
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ShouldResize", func() {
	var (
		usedPercent    float64
		availableBytes int64
		triggers       *apiv1.ResizeTriggers
	)

	BeforeEach(func() {
		triggers = &apiv1.ResizeTriggers{}
		availableBytes = 0
	})

	Context("Usage threshold trigger", func() {
		It("should return true when usage exceeds threshold", func() {
			// 85% used, threshold 80 → true
			usedPercent = 85.0
			triggers.UsageThreshold = ptr.To(80)

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeTrue())
		})

		It("should return false when usage is below threshold", func() {
			// 75% used, threshold 80 → false
			usedPercent = 75.0
			triggers.UsageThreshold = ptr.To(80)

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeFalse())
		})
	})

	Context("MinAvailable trigger", func() {
		It("should return true when available space is below minAvailable", func() {
			// 5Gi available, minAvailable "10Gi" → true
			usedPercent = 50.0
			availableBytes = mustParseQuantityValue("5Gi")
			triggers.UsageThreshold = ptr.To(80)
			triggers.MinAvailable = "10Gi"

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeTrue())
		})

		It("should return false when available space is above minAvailable", func() {
			// 15Gi available, minAvailable "10Gi" → false
			usedPercent = 50.0
			availableBytes = mustParseQuantityValue("15Gi")
			triggers.UsageThreshold = ptr.To(80)
			triggers.MinAvailable = "10Gi"

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeFalse())
		})
	})

	Context("Both usage and minAvailable triggers set", func() {
		It("should return true when usage exceeds threshold", func() {
			// 85%, minAvailable "10Gi", available 15Gi → true (usage triggers)
			usedPercent = 85.0
			availableBytes = mustParseQuantityValue("15Gi")
			triggers.UsageThreshold = ptr.To(80)
			triggers.MinAvailable = "10Gi"

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeTrue())
		})

		It("should return true when available is below minAvailable", func() {
			// 75%, minAvailable "10Gi", available 5Gi → true (minAvailable triggers)
			usedPercent = 75.0
			availableBytes = mustParseQuantityValue("5Gi")
			triggers.UsageThreshold = ptr.To(80)
			triggers.MinAvailable = "10Gi"

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeTrue())
		})

		It("should return false when neither trigger is met", func() {
			// 75%, minAvailable "5Gi", available 10Gi → false
			usedPercent = 75.0
			availableBytes = mustParseQuantityValue("10Gi")
			triggers.UsageThreshold = ptr.To(80)
			triggers.MinAvailable = "5Gi"

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeFalse())
		})
	})

	Context("Default threshold", func() {
		It("should use default threshold of 80 when threshold is nil", func() {
			// nil means default 80, test with 85% → true
			usedPercent = 85.0
			triggers.UsageThreshold = nil

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeTrue())
		})

		It("should use default threshold of 80 when checking against 75%", func() {
			usedPercent = 75.0
			triggers.UsageThreshold = nil

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeFalse())
		})
	})

	Context("Nil triggers", func() {
		It("should return false when triggers is nil", func() {
			usedPercent = 85.0
			triggers = nil

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeFalse())
		})
	})

	Context("Edge cases", func() {
		It("should handle float precision in usage percent", func() {
			// Test boundary condition with floating point
			usedPercent = 80.1
			triggers.UsageThreshold = ptr.To(80)

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeTrue())
		})

		It("should handle exactly equal usage threshold", func() {
			// When usage equals threshold, should not trigger (> not >=)
			usedPercent = 80.0
			triggers.UsageThreshold = ptr.To(80)

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeFalse())
		})

		It("should handle invalid minAvailable and ignore that trigger", func() {
			// Invalid minAvailable should be ignored
			usedPercent = 75.0
			availableBytes = 0
			triggers.UsageThreshold = ptr.To(80)
			triggers.MinAvailable = "invalid"

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeFalse())
		})

		It("should handle empty minAvailable string", func() {
			// Empty minAvailable string should be ignored
			usedPercent = 75.0
			availableBytes = 0
			triggers.UsageThreshold = ptr.To(80)
			triggers.MinAvailable = ""

			result := ShouldResize(usedPercent, availableBytes, triggers)

			Expect(result).To(BeFalse())
		})
	})
})

// mustParseQuantityValue parses a quantity string and returns its int64 value.
func mustParseQuantityValue(s string) int64 {
	q := resource.MustParse(s)
	return q.Value()
}
