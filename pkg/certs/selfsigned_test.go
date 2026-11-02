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

package certs

import (
	"crypto/x509"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Self-signed client certificate", func() {
	Describe("GenerateSelfSignedClientCertificate", func() {
		It("should generate a valid self-signed certificate", func() {
			pair, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())
			Expect(pair).ToNot(BeNil())
		})

		It("should set the requested common name", func() {
			pair, err := GenerateSelfSignedClientCertificate("my-operator")
			Expect(err).ToNot(HaveOccurred())

			cert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())
			Expect(cert.Subject.CommonName).To(Equal("my-operator"))
		})

		It("should produce a certificate with client auth extended key usage", func() {
			pair, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())

			cert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())
			Expect(cert.ExtKeyUsage).To(ContainElement(x509.ExtKeyUsageClientAuth))
			Expect(cert.ExtKeyUsage).ToNot(ContainElement(x509.ExtKeyUsageServerAuth))
		})

		It("should produce a certificate that is not a CA", func() {
			pair, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())

			cert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())
			Expect(cert.IsCA).To(BeFalse())
		})

		It("should produce a currently valid certificate", func() {
			pair, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())

			cert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())
			Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
			Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))
		})

		It("should produce two distinct certificates on successive calls", func() {
			pair1, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())
			pair2, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())

			Expect(pair1.Certificate).ToNot(Equal(pair2.Certificate))
			Expect(pair1.Private).ToNot(Equal(pair2.Private))
		})
	})

	Describe("TLSCertificate", func() {
		It("should convert a key pair to a tls.Certificate without error", func() {
			pair, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())

			tlsCert, err := pair.TLSCertificate()
			Expect(err).ToNot(HaveOccurred())
			Expect(tlsCert.Certificate).ToNot(BeEmpty())
		})
	})

	Describe("PublicKeyFingerprint", func() {
		It("should return a non-empty hex string", func() {
			pair, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())

			cert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			fp := PublicKeyFingerprint(cert)
			Expect(fp).ToNot(BeEmpty())
			Expect(fp).To(HaveLen(64)) // SHA-256 hex is always 64 chars
		})

		It("should return the same fingerprint for the same key", func() {
			pair, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())

			cert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			Expect(PublicKeyFingerprint(cert)).To(Equal(PublicKeyFingerprint(cert)))
		})

		It("should return different fingerprints for different keys", func() {
			pair1, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())
			pair2, err := GenerateSelfSignedClientCertificate("test-cn")
			Expect(err).ToNot(HaveOccurred())

			cert1, err := pair1.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())
			cert2, err := pair2.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			Expect(PublicKeyFingerprint(cert1)).ToNot(Equal(PublicKeyFingerprint(cert2)))
		})
	})
})
