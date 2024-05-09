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

var _ = DescribeTable("PostgreSQL booleans parsing",
	func(input string, expectedPositive, expectedNegative bool) {
		Expect(IsTrue(input)).To(Equal(expectedPositive))
		Expect(IsFalse(input)).To(Equal(expectedNegative))
	},
	Entry("on", "on", true, false),
	Entry("oN", "oN", true, false),
	Entry("off", "off", false, true),
	Entry("true", "true", true, false),
	Entry("false", "false", false, true),
	Entry("0", "0", false, true),
	Entry("1", "1", true, false),
	Entry("foo", "foo", false, false),
)
