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

package utils

import (
	"github.com/cloudnative-pg/machinery/pkg/postgres/password"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EnsureEncryptedPassword", func() {
	It("encrypts a plaintext password using SCRAM-SHA-256", func() {
		out, err := EnsureEncryptedPassword("hunter2")
		Expect(err).ToNot(HaveOccurred())
		Expect(password.GetType(out)).To(Equal(password.SCRAMSHA256))
	})

	It("returns an already SCRAM-SHA-256 hash unchanged", func() {
		const scramHash = "SCRAM-SHA-256$4096:Y2F2YWxjYW50aQ==$" +
			"eCIyo2QEZvwlcMThm1zwQDPnw0jOHlCapCE+QFpHsGs=:" +
			"YKhSEcd4QiX3SBzmtTOHHA/9yaTBGJWAMMw7+92OyHM="

		out, err := EnsureEncryptedPassword(scramHash)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(Equal(scramHash))
	})

	It("returns an already MD5 hash unchanged", func() {
		const md5Hash = "md5e2bf8852d3801fa55a86d7c8d6dcb39d"

		out, err := EnsureEncryptedPassword(md5Hash)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(Equal(md5Hash))
	})

	It("does not leak the cleartext password in the error", func() {
		// A NUL byte trips SASLprep's prohibition on ASCII control
		// characters (RFC 4013 §2.3, table C.2.1).
		const secret = "leaky-secret\x00value"

		out, err := EnsureEncryptedPassword(secret)
		Expect(err).To(HaveOccurred())
		Expect(out).To(BeEmpty())
		Expect(err.Error()).ToNot(ContainSubstring("leaky-secret"))
		Expect(err.Error()).ToNot(ContainSubstring("value"))
	})
})
