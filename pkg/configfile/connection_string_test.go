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

package configfile

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Connection string parameter escaper", func() {
	It("works correctly when we don't need to escape values", func() {
		Expect(escapeConnectionStringParameter("one", "1")).To(Equal("one='1'"))
	})

	It("escapes values when they contain spaces", func() {
		Expect(escapeConnectionStringParameter("one", "1 ")).To(Equal("one='1 '"))
		Expect(escapeConnectionStringParameter("two", " 2")).To(Equal("two=' 2'"))
	})

	It("escapes values when they are empty", func() {
		Expect(escapeConnectionStringParameter("empty", "")).To(Equal("empty=''"))
	})

	It("works correctly when the apostrophe character is detected", func() {
		Expect(escapeConnectionStringParameter("one", "'hey'")).To(Equal("one='''hey'''"))
	})
})

var _ = Describe("Connection string generator", func() {
	It("works with zero items", func() {
		Expect(CreateConnectionString(map[string]string{})).To(Equal(""))
	})

	It("works with one item", func() {
		Expect(CreateConnectionString(
			map[string]string{
				"one": "1",
			})).To(Equal("one='1'"))
	})

	It("works with two items", func() {
		Expect(CreateConnectionString(
			map[string]string{
				"one": "1",
				"two": "2",
			})).To(
			Or(
				Equal("one='1' two='2'"),
				Equal("two='2' one='1'"),
			))
	})
})
