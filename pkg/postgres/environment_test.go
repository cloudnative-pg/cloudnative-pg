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

package postgres

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IsReservedEnvironmentVariable", func() {
	DescribeTable("detects reserved variables",
		func(name string) {
			Expect(IsReservedEnvironmentVariable(name)).To(BeTrue())
		},
		Entry("PGDATA", "PGDATA"),
		Entry("PGHOST", "PGHOST"),
		Entry("CNPG_SECRET", "CNPG_SECRET"),
		Entry("POD_NAME", "POD_NAME"),
		Entry("NAMESPACE", "NAMESPACE"),
		Entry("CLUSTER_NAME", "CLUSTER_NAME"),
	)

	DescribeTable("allows non-reserved variables",
		func(name string) {
			Expect(IsReservedEnvironmentVariable(name)).To(BeFalse())
		},
		Entry("LC_ALL", "LC_ALL"),
		Entry("TZ", "TZ"),
		Entry("FOO", "FOO"),
	)
})

var _ = Describe("FindUnknownPlaceholders", func() {
	It("returns nil for values without placeholders", func() {
		Expect(FindUnknownPlaceholders("/some/path")).To(BeNil())
	})

	It("returns nil for known placeholders", func() {
		Expect(FindUnknownPlaceholders("${image_root}/lib")).To(BeNil())
	})

	It("detects unknown placeholders", func() {
		Expect(FindUnknownPlaceholders("${image_rot}/lib")).To(Equal([]string{"${image_rot}"}))
	})

	It("detects multiple unknown placeholders", func() {
		Expect(FindUnknownPlaceholders("${foo}/${bar}")).To(Equal([]string{"${foo}", "${bar}"}))
	})

	It("reports only unknown placeholders when mixed with known ones", func() {
		Expect(FindUnknownPlaceholders("${image_root}/${typo}")).To(Equal([]string{"${typo}"}))
	})

	It("ignores escaped placeholders", func() {
		Expect(FindUnknownPlaceholders("$${not_a_placeholder}")).To(BeNil())
	})

	It("ignores escaped known placeholders", func() {
		Expect(FindUnknownPlaceholders("$${image_root}")).To(BeNil())
	})

	It("detects unknown after escaped", func() {
		Expect(FindUnknownPlaceholders("$${escaped}/${unknown}")).To(Equal([]string{"${unknown}"}))
	})

	It("detects unknown in $$${unknown} (literal $ plus placeholder)", func() {
		Expect(FindUnknownPlaceholders("$$${unknown}")).To(Equal([]string{"${unknown}"}))
	})

	It("ignores $$$${unknown} (fully escaped)", func() {
		Expect(FindUnknownPlaceholders("$$$${unknown}")).To(BeNil())
	})
})

var _ = Describe("ExpandEnvPlaceholders", func() {
	It("expands image_root to the extension mount path", func() {
		Expect(ExpandEnvPlaceholders("${image_root}/lib", "my-ext")).To(Equal("/extensions/my-ext/lib"))
	})

	It("unescapes $${...} to literal ${...}", func() {
		Expect(ExpandEnvPlaceholders("$${not_expanded}", "my-ext")).To(Equal("${not_expanded}"))
	})

	It("handles mixed escaped and expanded placeholders", func() {
		Expect(ExpandEnvPlaceholders("${image_root}/$${literal}", "my-ext")).To(Equal("/extensions/my-ext/${literal}"))
	})

	It("preserves $${image_root} as literal", func() {
		Expect(ExpandEnvPlaceholders("$${image_root}", "my-ext")).To(Equal("${image_root}"))
	})

	It("expands $$${image_root} to literal $ plus expanded path", func() {
		Expect(ExpandEnvPlaceholders("$$${image_root}", "my-ext")).To(Equal("$/extensions/my-ext"))
	})

	It("preserves $$$${image_root} as $${image_root}", func() {
		Expect(ExpandEnvPlaceholders("$$$${image_root}", "my-ext")).To(Equal("$${image_root}"))
	})

	It("does not alter bare $$ without braces", func() {
		Expect(ExpandEnvPlaceholders("plain $$text", "my-ext")).To(Equal("plain $$text"))
	})

	It("leaves unknown placeholders as-is", func() {
		Expect(ExpandEnvPlaceholders("${unknown}", "my-ext")).To(Equal("${unknown}"))
	})
})
