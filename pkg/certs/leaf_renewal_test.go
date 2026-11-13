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

	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Leaf certificate renewal", func() {
	const namespace = "test-namespace"

	const host = "this.server.com"

	// Server certificates carry the host as DNS name, client certificates
	// carry none: pass the matching list to RenewLeafCertificate so that only
	// the CA check decides whether a renewal is needed.
	altDNSNamesFor := func(usage CertType) []string {
		if usage == CertTypeServer {
			return []string{host}
		}
		return nil
	}

	anyUsageOptions := func() *x509.VerifyOptions {
		return &x509.VerifyOptions{KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}
	}

	newLeafSecret := func(ca *KeyPair, usage CertType) *corev1.Secret {
		leaf, err := ca.CreateAndSignPair(host, usage, altDNSNamesFor(usage))
		Expect(err).ToNot(HaveOccurred())
		return leaf.GenerateCertificateSecret(namespace, "leaf-secret")
	}

	It("does not renew a valid leaf certificate signed by the current CA", func() {
		ca, err := CreateRootCA("ca", namespace)
		Expect(err).ToNot(HaveOccurred())
		caSecret := ca.GenerateCASecret(namespace, "ca-secret")
		leafSecret := newLeafSecret(ca, CertTypeServer)
		originalCert := leafSecret.Data["tls.crt"]

		renewed, err := RenewLeafCertificate(caSecret, leafSecret, altDNSNamesFor(CertTypeServer))
		Expect(err).ToNot(HaveOccurred())
		Expect(renewed).To(BeFalse())
		Expect(leafSecret.Data["tls.crt"]).To(Equal(originalCert))
	})

	It("does not renew a valid client leaf certificate signed by the current CA", func() {
		ca, err := CreateRootCA("ca", namespace)
		Expect(err).ToNot(HaveOccurred())
		caSecret := ca.GenerateCASecret(namespace, "ca-secret")
		leafSecret := newLeafSecret(ca, CertTypeClient)

		renewed, err := RenewLeafCertificate(caSecret, leafSecret, altDNSNamesFor(CertTypeClient))
		Expect(err).ToNot(HaveOccurred())
		Expect(renewed).To(BeFalse())
	})

	It("renews a valid leaf certificate when the CA has changed", func() {
		oldCA, err := CreateRootCA("ca", namespace)
		Expect(err).ToNot(HaveOccurred())
		newCA, err := CreateRootCA("ca", namespace)
		Expect(err).ToNot(HaveOccurred())
		newCASecret := newCA.GenerateCASecret(namespace, "ca-secret")

		for _, usage := range []CertType{CertTypeServer, CertTypeClient} {
			leafSecret := newLeafSecret(oldCA, usage)
			originalCert := leafSecret.Data["tls.crt"]

			renewed, err := RenewLeafCertificate(newCASecret, leafSecret, altDNSNamesFor(usage))
			Expect(err).ToNot(HaveOccurred())
			Expect(renewed).To(BeTrue())
			Expect(leafSecret.Data["tls.crt"]).ToNot(Equal(originalCert))

			renewedPair, err := ParseServerSecret(leafSecret)
			Expect(err).ToNot(HaveOccurred())
			// IsValid fills opts.Roots, so every check needs its own options
			Expect(renewedPair.IsValid(newCA, anyUsageOptions())).To(Succeed())
			Expect(renewedPair.IsValid(oldCA, anyUsageOptions())).ToNot(Succeed())
		}
	})
})
