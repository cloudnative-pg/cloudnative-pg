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

package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
)

// TEMPORARY: plants a worker crash to test the crash diagnostics. Not for merge.
var _ = Describe("Planted panic", Label("diag-plant"), func() {
	It("crashes the worker", func() {
		go func() {
			var m map[string]int
			m["planted"] = 1
		}()
		time.Sleep(30 * time.Second)
	})
})
