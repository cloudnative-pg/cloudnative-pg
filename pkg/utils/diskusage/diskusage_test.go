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

package diskusage

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Get", func() {
	It("returns a sane usage for an existing directory", func() {
		u, err := Get(GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(u.TotalBytes).To(BeNumerically(">", 0))
		Expect(u.AvailableBytes).To(BeNumerically(">", 0))
		Expect(u.UsedBytes).To(BeNumerically("<=", u.TotalBytes))
		Expect(u.FilesystemID).NotTo(BeZero())
	})

	It("errors for a non-existent path", func() {
		_, err := Get("/this/path/does/not/exist/at/all")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("PercentUsed", func() {
	It("computes a percentage", func() {
		u := Usage{TotalBytes: 200, UsedBytes: 50}
		Expect(u.PercentUsed()).To(BeNumerically("~", 25.0, 0.001))
	})

	It("returns zero when total is zero", func() {
		Expect(Usage{}.PercentUsed()).To(Equal(0.0))
	})
})
