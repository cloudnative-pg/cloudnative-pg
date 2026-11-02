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

package run

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("boolFromEnv", func() {
	const envVar = "CNPG_TEST_BOOL_FROM_ENV"

	AfterEach(func() {
		GinkgoT().Setenv(envVar, "")
	})

	It("returns false when the variable is unset", func() {
		GinkgoT().Setenv(envVar, "")
		Expect(boolFromEnv(envVar)).To(BeFalse())
	})

	DescribeTable("accepts valid boolean encodings",
		func(value string, expected bool) {
			GinkgoT().Setenv(envVar, value)
			Expect(boolFromEnv(envVar)).To(Equal(expected))
		},
		Entry("true", "true", true),
		Entry("TRUE", "TRUE", true),
		Entry("1", "1", true),
		Entry("t", "t", true),
		Entry("T", "T", true),
		Entry("false", "false", false),
		Entry("FALSE", "FALSE", false),
		Entry("0", "0", false),
		Entry("f", "f", false),
		Entry("F", "F", false),
	)

	// Unparseable values cause boolFromEnv to call os.Exit(1). We deliberately
	// do not exercise that path here — it would terminate the ginkgo process.
	// The behavior is covered by code review and by the boolFromEnv godoc.
})
