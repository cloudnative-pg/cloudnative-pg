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

package hba

import (
	"github.com/cloudnative-pg/machinery/pkg/stringset"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ensureCIDR", func() {
	It("adds /32 suffix to IPv4 address", func() {
		Expect(ensureCIDR("10.0.0.1")).To(Equal("10.0.0.1/32"))
		Expect(ensureCIDR("192.168.1.1")).To(Equal("192.168.1.1/32"))
		Expect(ensureCIDR("0.0.0.0")).To(Equal("0.0.0.0/32"))
	})

	It("adds /128 suffix to IPv6 address", func() {
		Expect(ensureCIDR("2001:db8::1")).To(Equal("2001:db8::1/128"))
		Expect(ensureCIDR("::1")).To(Equal("::1/128"))
		Expect(ensureCIDR("fe80::1")).To(Equal("fe80::1/128"))
	})

	It("preserves existing IPv4 CIDR mask", func() {
		Expect(ensureCIDR("10.0.0.0/24")).To(Equal("10.0.0.0/24"))
		Expect(ensureCIDR("192.168.1.0/16")).To(Equal("192.168.1.0/16"))
		Expect(ensureCIDR("0.0.0.0/0")).To(Equal("0.0.0.0/0"))
	})

	It("preserves existing IPv6 CIDR mask", func() {
		Expect(ensureCIDR("2001:db8::/64")).To(Equal("2001:db8::/64"))
		Expect(ensureCIDR("::/0")).To(Equal("::/0"))
	})

	It("returns invalid input unchanged", func() {
		Expect(ensureCIDR("invalid")).To(Equal("invalid"))
		Expect(ensureCIDR("")).To(Equal(""))
	})
})

var _ = Describe("ValidateLine", func() {
	It("accepts a plain line without references", func() {
		err := ValidateLine("host all all 10.0.0.1/32 md5", stringset.New())
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("accepts a valid podselector reference", func() {
		err := ValidateLine("host all all ${podselector:myapp} md5", stringset.From([]string{"myapp"}))
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("rejects an unknown podselector reference", func() {
		err := ValidateLine("host all all ${podselector:unknown} md5", stringset.From([]string{"myapp"}))
		Expect(err).Should(HaveOccurred())
		Expect(err).To(BeAssignableToTypeOf(&ErrPodSelectorNotFound{}))
		Expect(err.(*ErrPodSelectorNotFound).SelectorName).To(Equal("unknown"))
	})

	It("rejects an unknown descriptor type", func() {
		err := ValidateLine("host all all ${unknown:value} md5", stringset.New())
		Expect(err).Should(HaveOccurred())
		Expect(err).To(BeAssignableToTypeOf(&ErrUnknownDescriptorType{}))
		Expect(err.(*ErrUnknownDescriptorType).DescriptorType).To(Equal("unknown"))
	})

	It("rejects multiple podselector references", func() {
		err := ValidateLine(
			"host all all ${podselector:app1} ${podselector:app2} md5",
			stringset.From([]string{"app1", "app2"}),
		)
		Expect(err).Should(HaveOccurred())
		Expect(err).To(BeAssignableToTypeOf(&ErrMultiplePodSelectorReferences{}))
		Expect(err.(*ErrMultiplePodSelectorReferences).Count).To(Equal(2))
	})
})

var _ = Describe("ExpandLine", func() {
	It("returns the original line when there are no references", func() {
		result := ExpandLine("host all all 10.0.0.1/32 md5", nil)
		Expect(result).To(Equal([]string{"host all all 10.0.0.1/32 md5"}))
	})

	It("expands a single IPv4 with /32 suffix", func() {
		result := ExpandLine("host all all ${podselector:myapp} md5", map[string][]string{
			"myapp": {"10.0.0.1"},
		})
		Expect(result).To(Equal([]string{"host all all 10.0.0.1/32 md5"}))
	})

	It("expands multiple IPv4 with /32 suffix", func() {
		result := ExpandLine("host all all ${podselector:myapp} md5", map[string][]string{
			"myapp": {"10.0.0.1", "10.0.0.2", "10.0.0.3"},
		})
		Expect(result).To(Equal([]string{
			"host all all 10.0.0.1/32 md5",
			"host all all 10.0.0.2/32 md5",
			"host all all 10.0.0.3/32 md5",
		}))
	})

	It("expands IPv6 with /128 suffix", func() {
		result := ExpandLine("host all all ${podselector:myapp} md5", map[string][]string{
			"myapp": {"2001:db8::1", "::1"},
		})
		Expect(result).To(Equal([]string{
			"host all all 2001:db8::1/128 md5",
			"host all all ::1/128 md5",
		}))
	})

	It("expands mixed IPv4 and IPv6 from dual-stack pods", func() {
		result := ExpandLine("host all all ${podselector:myapp} md5", map[string][]string{
			"myapp": {"10.0.0.1", "2001:db8::1"},
		})
		Expect(result).To(Equal([]string{
			"host all all 10.0.0.1/32 md5",
			"host all all 2001:db8::1/128 md5",
		}))
	})

	It("preserves existing CIDR mask", func() {
		result := ExpandLine("host all all ${podselector:myapp} md5", map[string][]string{
			"myapp": {"10.0.0.0/24", "2001:db8::/64"},
		})
		Expect(result).To(Equal([]string{
			"host all all 10.0.0.0/24 md5",
			"host all all 2001:db8::/64 md5",
		}))
	})

	It("returns empty slice when selector has no IPs", func() {
		result := ExpandLine("host all all ${podselector:myapp} md5", map[string][]string{
			"myapp": {},
		})
		Expect(result).To(BeEmpty())
	})

	It("comments out line with unknown selector", func() {
		result := ExpandLine("host all all ${podselector:unknown} md5", map[string][]string{})
		Expect(result).To(HaveLen(1))
		Expect(result[0]).To(HavePrefix("# "))
		Expect(result[0]).To(ContainSubstring("pod selector not found: unknown"))
	})

	It("comments out line with unknown descriptor type", func() {
		result := ExpandLine("host all all ${unknown:value} md5", nil)
		Expect(result).To(HaveLen(1))
		Expect(result[0]).To(HavePrefix("# "))
		Expect(result[0]).To(ContainSubstring("unknown descriptor type: unknown"))
	})
})
