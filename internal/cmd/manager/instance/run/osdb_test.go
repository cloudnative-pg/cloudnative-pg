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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OS database", func() {
	today := time.Date(2025, time.September, 18, 0, 0, 0, 0, time.UTC)

	DescribeTable(
		"OS checks",
		func(distro string, deprecated, supported bool) {
			entry, ok := defaultOSDB.Get(distro)
			Expect(ok).To(
				BeTrue(),
				"entry for distro %q not found",
				distro)

			actualSupported := entry.IsSupported(today)
			actualDeprecated := entry.IsDeprecated(today)
			Expect(actualDeprecated).To(
				Equal(deprecated),
				"expected %s deprecation status to be %t instead of %t", distro, deprecated, actualDeprecated)
			Expect(actualSupported).To(
				Equal(supported),
				"expected %s support status to be %t instead of %t", distro, supported, actualSupported)
		},
		Entry("trixie", "13 (trixie)", false, true),
		Entry("bookworm", "12 (bookworm)", false, true),
		Entry("bullseye", "11 (bullseye)", true, true),
		Entry("buster", "10 (buster)", true, false),
	)
})
