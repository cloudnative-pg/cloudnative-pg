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

var _ = Describe("NewCmd", func() {
	// An in-place instance manager upgrade re-execs this binary with the argv of
	// the Pod, so a flag dropped in a newer release must still parse.
	It("accepts the status-port-tls flag passed by Pods created before 1.30", func() {
		Expect(NewCmd().ParseFlags([]string{"--status-port-tls"})).To(Succeed())
	})
})
