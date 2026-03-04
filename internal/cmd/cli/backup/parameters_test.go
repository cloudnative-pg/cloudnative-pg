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

package backup

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("plugin parameters parsing", func() {
	DescribeTable(
		"plugin parameters and values table",
		func(value string, expectedParams pluginParameters) {
			var params pluginParameters
			Expect(params.Set(value)).ToNot(HaveOccurred())
			Expect(params).To(HaveLen(len(expectedParams)))
			for k, v := range expectedParams {
				Expect(params).To(HaveKeyWithValue(k, v))
			}
		},
		Entry("empty value", "", nil),
		Entry("singleton", "a=b", map[string]string{
			"a": "b",
		}),
		Entry("singleton without value", "a", map[string]string{
			"a": "",
		}),
		Entry("set", "a=b,c=d", map[string]string{
			"a": "b",
			"c": "d",
		}),
		Entry("set with elements without value", "a=b,c,d=,e=f", map[string]string{
			"a": "b",
			"c": "",
			"d": "",
			"e": "f",
		}),
	)
})
