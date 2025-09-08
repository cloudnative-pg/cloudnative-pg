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

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type testStruct struct{ Val int }

var _ = Describe("toSliceWithPointers", func() {
	It("should return pointers to the original slice elements", func() {
		items := []testStruct{{1}, {2}, {3}}
		pointers := toSliceWithPointers(items)
		Expect(pointers).To(HaveLen(len(items)))
		for i := range items {
			Expect(pointers[i]).To(BeIdenticalTo(&items[i]))
		}
	})
})
