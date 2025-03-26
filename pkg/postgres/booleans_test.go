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

var _ = DescribeTable("Test parsing of PostgreSQL configuration booleans",
	func(input string, expectedValue, expectError bool) {
		value, err := ParsePostgresConfigBoolean(input)
		if expectError {
			Expect(err).Should(HaveOccurred())
		} else {
			Expect(err).ShouldNot(HaveOccurred())
		}
		Expect(value).To(Equal(expectedValue))
	},
	Entry("foo", "foo", false, true),
	Entry("on", "on", true, false),
	Entry("ON", "ON", true, false),
	Entry("off", "off", false, false),
	Entry("true", "true", true, false),
	Entry("false", "false", false, false),
	Entry("0", "0", false, false),
	Entry("1", "1", true, false),
	Entry("n", "n", false, false),
	Entry("y", "y", true, false),
	Entry("t", "t", true, false),
	Entry("f", "f", false, false),
	Entry("o", "o", false, true),
	Entry("ye", "ye", true, false),
	Entry("tr", "tr", true, false),
	Entry("fa", "fa", false, false),
)
