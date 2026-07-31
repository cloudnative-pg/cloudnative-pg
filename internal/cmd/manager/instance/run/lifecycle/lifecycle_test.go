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

package lifecycle

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("postmasterMayComeBack", func() {
	It("lets the postmaster come back when the instance is expected to be down", func() {
		Expect(postmasterMayComeBack(false, true)).To(BeTrue())
	})

	It("exits when nothing expected the instance to be down", func() {
		Expect(postmasterMayComeBack(false, false)).To(BeFalse())
	})

	It("exits when the instance was stopped because it is isolated", func() {
		Expect(postmasterMayComeBack(true, false)).To(BeFalse())
	})

	It("exits when the instance was stopped because it is isolated even if something "+
		"else concurrently expected it to be down", func() {
		// This is the case the flag exists for: a restart or a fencing cycle racing the
		// isolation shutdown must not keep the instance manager up to start PostgreSQL
		// again while the partition is still there.
		Expect(postmasterMayComeBack(true, true)).To(BeFalse())
	})
})
