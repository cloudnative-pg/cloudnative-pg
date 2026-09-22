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

package execute

import (
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("tryAddDataChecksums", func() {
	checksumsEnabled := utils.ParsePgControldataOutput("Data page checksum version:1\n")
	checksumsDisabled := utils.ParsePgControldataOutput("Data page checksum version:0\n")

	It("sets --data-checksums when checksums are enabled and target version is before 18", func() {
		options, err := tryAddDataChecksums(checksumsEnabled, 17, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(options).To(ContainElement("--data-checksums"))
	})

	It("does not set --data-checksums when checksums are enabled and target version is 18+", func() {
		options, err := tryAddDataChecksums(checksumsEnabled, 18, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(options).ToNot(ContainElement("--data-checksums"))
		Expect(options).ToNot(ContainElement("--no-data-checksums"))
	})

	It("does not set --no-data-checksums when checksums are disabled and target version is before 18", func() {
		options, err := tryAddDataChecksums(checksumsDisabled, 17, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(options).ToNot(ContainElement("--no-data-checksums"))
		Expect(options).ToNot(ContainElement("--data-checksums"))
	})

	It("sets --no-data-checksums when checksums are disabled and target version is 18+", func() {
		options, err := tryAddDataChecksums(checksumsDisabled, 18, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(options).To(ContainElement("--no-data-checksums"))
	})

	It("treats an unrecognized checksum version as enabled rather than disabled", func() {
		checksumsUnknown := utils.ParsePgControldataOutput("Data page checksum version:2\n")

		options, err := tryAddDataChecksums(checksumsUnknown, 18, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(options).ToNot(ContainElement("--no-data-checksums"))
	})
})
