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
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Keypair generation", func() {
	It("should generate a correct root CA", func() {
		pair, err := CreateRootCA("test", "namespace")
		Expect(err).ToNot(HaveOccurred())

		cert, err := pair.ParseCertificate()
		Expect(err).ToNot(HaveOccurred())

		key, err := pair.ParseECPrivateKey()
		Expect(err).ToNot(HaveOccurred())

		Expect(cert.PublicKey).To(BeEquivalentTo(&key.PublicKey))
		Expect(cert.IsCA).To(BeTrue())
		Expect(cert.BasicConstraintsValid).To(BeTrue())
		Expect(cert.KeyUsage & x509.KeyUsageDigitalSignature).To(BeZero())
		Expect(cert.KeyUsage & x509.KeyUsageKeyEncipherment).To(BeZero())
		Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
		Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))

		// The root CA is autosigned
		Expect(cert.CheckSignatureFrom(cert)).ToNot(HaveOccurred())
	})

	It("should create a CA K8s corev1/secret resource structure", func() {
		pair, err := CreateRootCA("test", "namespace")
		Expect(err).ToNot(HaveOccurred())

		secret := pair.GenerateCASecret("namespace", "name")
		Expect(secret.Namespace).To(Equal("namespace"))
		Expect(secret.Name).To(Equal("name"))
		Expect(secret.Data[CACertKey]).To(Equal(pair.Certificate))
		Expect(secret.Data[CAPrivateKeyKey]).To(Equal(pair.Private))
	})

	It("should be able to renew an existing CA certificate", func() {
		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-90 * 24 * time.Hour)
		ca, err := createCAWithValidity(notBefore, notAfter, nil, nil, "root", "namespace")
		Expect(err).ToNot(HaveOccurred())

		privateKey, err := ca.ParseECPrivateKey()
		Expect(err).ToNot(HaveOccurred())

		oldCert, err := ca.ParseCertificate()
		Expect(err).ToNot(HaveOccurred())

		err = ca.RenewCertificate(privateKey, nil, []string{})
		Expect(err).ToNot(HaveOccurred())

		newCert, err := ca.ParseCertificate()
		Expect(err).ToNot(HaveOccurred())

		Expect(newCert.NotBefore).To(BeTemporally("<", time.Now()))
		Expect(newCert.NotAfter).To(BeTemporally(">", time.Now()))

		Expect(newCert.SerialNumber).ToNot(Equal(oldCert.SerialNumber))

		Expect(newCert.Subject).To(Equal(oldCert.Subject))
		Expect(newCert.Issuer).To(Equal(oldCert.Subject))
		Expect(newCert.IsCA).To(Equal(oldCert.IsCA))
		Expect(newCert.KeyUsage).To(Equal(oldCert.KeyUsage))
		Expect(newCert.ExtKeyUsage).To(Equal(oldCert.ExtKeyUsage))
	})

	It("marks expiring certificate as expiring", func() {
		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-90 * 24 * time.Hour)
		ca, err := createCAWithValidity(notBefore, notAfter, nil, nil, "root", "namespace")
		Expect(err).ToNot(HaveOccurred())
		isExpiring, _, err := ca.IsExpiring()
		Expect(isExpiring, err).To(BeTrue())
	})

	It("doesn't marks a valid certificate as expiring", func() {
		ca, err := CreateRootCA("test", "namespace")
		Expect(err).ToNot(HaveOccurred())
		isExpiring, _, err := ca.IsExpiring()
		Expect(isExpiring, err).To(BeFalse())
	})

	It("marks matching alt DNS names as matching", func() {
		ca, err := CreateRootCA("test", "namespace")
		Expect(err).ToNot(HaveOccurred())
		doAltDNSNamesMatch, err := ca.DoAltDNSNamesMatch([]string{})
		Expect(doAltDNSNamesMatch, err).To(BeTrue())
	})

	It("doesn't mark different alt DNS names as matching", func() {
		ca, err := CreateRootCA("test", "namespace")
		Expect(err).ToNot(HaveOccurred())
		doAltDNSNamesMatch, err := ca.DoAltDNSNamesMatch([]string{"foo.bar"})
		Expect(doAltDNSNamesMatch, err).To(BeFalse())
	})

	When("we have a CA generated", func() {
		It("should successfully generate a leaf certificate", func() {
			rootCA, err := CreateRootCA("test", "namespace")
			Expect(err).ToNot(HaveOccurred())

			pair, err := rootCA.CreateAndSignPair("this.host.name.com", CertTypeServer, nil)
			Expect(err).ToNot(HaveOccurred())

			cert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			key, err := pair.ParseECPrivateKey()
			Expect(err).ToNot(HaveOccurred())

			Expect(cert.PublicKey).To(BeEquivalentTo(&key.PublicKey))
			Expect(cert.IsCA).To(BeFalse())
			Expect(cert.BasicConstraintsValid).To(BeTrue())
			Expect(cert.KeyUsage & x509.KeyUsageDigitalSignature).ToNot(BeZero())
			Expect(cert.KeyUsage & x509.KeyUsageKeyEncipherment).ToNot(BeZero())
			Expect(cert.ExtKeyUsage).To(Equal([]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}))
			Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
			Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))
			Expect(cert.VerifyHostname("this.host.name.com")).To(Succeed())
			Expect(cert.DNSNames).To(Equal([]string{"this.host.name.com"}))

			caCert, err := rootCA.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			Expect(cert.CheckSignatureFrom(caCert)).ToNot(HaveOccurred())
		})

		It("should create a CA K8s corev1/secret resource structure", func() {
			rootCA, err := CreateRootCA("test", "namespace")
			Expect(err).ToNot(HaveOccurred())

			pair, err := rootCA.CreateAndSignPair("this.host.name.com", CertTypeServer, nil)
			Expect(err).ToNot(HaveOccurred())

			secret := pair.GenerateCertificateSecret("namespace", "name")
			Expect(secret.Namespace).To(Equal("namespace"))
			Expect(secret.Name).To(Equal("name"))
			Expect(secret.Data["tls.crt"]).To(Equal(pair.Certificate))
			Expect(secret.Data["tls.key"]).To(Equal(pair.Private))
			// GenerateCertificateSecret should NOT include the CA certificate
			Expect(secret.Data).ToNot(HaveKey("ca.crt"))
		})

		It("should create a webhook K8s corev1/secret resource structure with CA", func() {
			rootCA, err := CreateRootCA("test", "namespace")
			Expect(err).ToNot(HaveOccurred())

			pair, err := rootCA.CreateAndSignPair("webhook.service.namespace.svc", CertTypeServer, nil)
			Expect(err).ToNot(HaveOccurred())

			secret := pair.GenerateWebhookCertificateSecret("namespace", "name", rootCA.Certificate)
			Expect(secret.Namespace).To(Equal("namespace"))
			Expect(secret.Name).To(Equal("name"))
			Expect(secret.Data["tls.crt"]).To(Equal(pair.Certificate))
			Expect(secret.Data["tls.key"]).To(Equal(pair.Private))
			// GenerateWebhookCertificateSecret SHOULD include the CA certificate
			Expect(secret.Data).To(HaveKey("ca.crt"))
			Expect(secret.Data["ca.crt"]).To(Equal(rootCA.Certificate))
		})

		It("should be able to renew an existing certificate with no DNS names provided", func() {
			ca, err := CreateRootCA("test", "namespace")
			Expect(err).ToNot(HaveOccurred())

			notAfter := time.Now().Add(-10 * time.Hour)
			notBefore := notAfter.Add(-90 * 24 * time.Hour)

			privateKey, err := ca.ParseECPrivateKey()
			Expect(err).ToNot(HaveOccurred())

			caCert, err := ca.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			pair, err := ca.createAndSignPairWithValidity("this.host.name.com", notBefore, notAfter, CertTypeClient, nil)
			Expect(err).ToNot(HaveOccurred())

			oldCert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			err = pair.RenewCertificate(privateKey, caCert, nil)
			Expect(err).ToNot(HaveOccurred())

			newCert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			Expect(newCert.NotBefore).To(BeTemporally("<", time.Now()))
			Expect(newCert.NotAfter).To(BeTemporally(">", time.Now()))
			Expect(newCert.SerialNumber).ToNot(Equal(oldCert.SerialNumber))

			Expect(newCert.Subject).To(Equal(oldCert.Subject))
			Expect(newCert.Issuer).To(Equal(caCert.Subject))
			Expect(newCert.IPAddresses).To(Equal(oldCert.IPAddresses))
			Expect(newCert.IsCA).To(Equal(oldCert.IsCA))
			Expect(newCert.KeyUsage).To(Equal(oldCert.KeyUsage))
			Expect(newCert.ExtKeyUsage).To(Equal(oldCert.ExtKeyUsage))

			Expect(newCert.DNSNames).To(Equal(oldCert.DNSNames))
		})

		It("should be able to renew an existing certificate with new DNS names provided", func() {
			ca, err := CreateRootCA("test", "namespace")
			Expect(err).ToNot(HaveOccurred())

			notAfter := time.Now().Add(-10 * time.Hour)
			notBefore := notAfter.Add(-90 * 24 * time.Hour)

			privateKey, err := ca.ParseECPrivateKey()
			Expect(err).ToNot(HaveOccurred())

			caCert, err := ca.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			pair, err := ca.createAndSignPairWithValidity("this.host.name.com", notBefore, notAfter, CertTypeClient, nil)
			Expect(err).ToNot(HaveOccurred())

			oldCert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			newDNSNames := []string{"new.host.name.com"}
			err = pair.RenewCertificate(privateKey, caCert, newDNSNames)
			Expect(err).ToNot(HaveOccurred())

			newCert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			Expect(newCert.NotBefore).To(BeTemporally("<", time.Now()))
			Expect(newCert.NotAfter).To(BeTemporally(">", time.Now()))
			Expect(newCert.SerialNumber).ToNot(Equal(oldCert.SerialNumber))

			Expect(newCert.Subject).To(Equal(oldCert.Subject))
			Expect(newCert.Issuer).To(Equal(caCert.Subject))
			Expect(newCert.IPAddresses).To(Equal(oldCert.IPAddresses))
			Expect(newCert.IsCA).To(Equal(oldCert.IsCA))
			Expect(newCert.KeyUsage).To(Equal(oldCert.KeyUsage))
			Expect(newCert.ExtKeyUsage).To(Equal(oldCert.ExtKeyUsage))
			Expect(newCert.DNSNames).NotTo(Equal(oldCert.DNSNames))

			Expect(newCert.DNSNames).To(Equal(newDNSNames))
		})

		It("should be validated against the right server", func() {
			rootCA, err := CreateRootCA("test", "namespace")
			Expect(err).ToNot(HaveOccurred())

			pair, err := rootCA.CreateAndSignPair("this.host.name.com", CertTypeServer, nil)
			Expect(err).ToNot(HaveOccurred())

			err = pair.IsValid(rootCA, nil)
			Expect(err).ToNot(HaveOccurred())

			opts := x509.VerifyOptions{KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}

			err = pair.IsValid(rootCA, &opts)
			Expect(err).ToNot(HaveOccurred())

			otherRootCA, err := CreateRootCA("test", "namespace")
			Expect(err).ToNot(HaveOccurred())

			err = pair.IsValid(otherRootCA, nil)
			Expect(err).To(HaveOccurred())
		})

		It("should be able to handle new lines at the end of server certificates", func() {
			rootCA, err := CreateRootCA("test", "namespace")
			Expect(err).ToNot(HaveOccurred())

			pair, err := rootCA.CreateAndSignPair("this.host.name.com", CertTypeServer, nil)
			Expect(err).ToNot(HaveOccurred())

			blockServer, intermediatesPEM := pem.Decode(pair.Certificate)
			Expect(blockServer).NotTo(BeNil())
			Expect(intermediatesPEM).To(BeEmpty())

			pair.Certificate = append(pair.Certificate, []byte("\n")...)
			blockServer, intermediatesPEM = pem.Decode(pair.Certificate)
			Expect(blockServer).NotTo(BeNil())
			Expect(intermediatesPEM).NotTo(BeEmpty())

			opts := x509.VerifyOptions{KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			err = pair.IsValid(pair, &opts)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should validate using the full certificate chain", func() {
			rootCA, err := CreateRootCA("ROOT", "root certificate")
			Expect(err).ShouldNot(HaveOccurred())

			intermediate1, err := rootCA.CreateDerivedCA("L1", "intermediate 1")
			Expect(err).ShouldNot(HaveOccurred())

			intermediate2, err := intermediate1.CreateDerivedCA("L2", "intermediate 2")
			Expect(err).ShouldNot(HaveOccurred())

			server, err := intermediate2.CreateAndSignPair("this.host.name.com", CertTypeServer, nil)
			Expect(err).ShouldNot(HaveOccurred())

			var caBuffer bytes.Buffer
			caBuffer.Write(intermediate1.Certificate)
			caBuffer.Write(rootCA.Certificate)

			caBundle := &KeyPair{
				Certificate: caBuffer.Bytes(),
			}

			var tlsBuffer bytes.Buffer
			tlsBuffer.Write(server.Certificate)
			tlsBuffer.Write(intermediate2.Certificate)

			tlsCert := &KeyPair{
				Private:     server.Private,
				Certificate: tlsBuffer.Bytes(),
			}

			err = tlsCert.IsValid(caBundle, nil)
			Expect(err).ShouldNot(HaveOccurred())

			opts := x509.VerifyOptions{KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}

			err = tlsCert.IsValid(caBundle, &opts)
			Expect(err).ShouldNot(HaveOccurred())

			caBundleIncomplete := &KeyPair{
				Certificate: rootCA.Certificate,
			}

			err = tlsCert.IsValid(caBundleIncomplete, nil)
			Expect(err).Should(HaveOccurred())
		})
	})
})

var _ = Describe("Certicate duration and expiration threshold", func() {
	defaultCertificateDuration := configuration.CertificateDuration * 24 * time.Hour
	defaultExpiringThreshold := configuration.ExpiringCheckThreshold * 24 * time.Hour
	tenDays := 10 * 24 * time.Hour

	BeforeEach(func() {
		configuration.Current = configuration.NewConfiguration()
	})

	It("returns the default duration", func() {
		duration := getCertificateDuration()
		Expect(duration).To(BeEquivalentTo(defaultCertificateDuration))
	})

	It("returns the default duration if the configuration is a negative value", func() {
		configuration.Current.CertificateDuration = -1
		duration := getCertificateDuration()
		Expect(duration).To(BeEquivalentTo(defaultCertificateDuration))
	})

	It("returns a valid duration of 10 days", func() {
		configuration.Current.CertificateDuration = 10
		duration := getCertificateDuration()
		Expect(duration).To(BeEquivalentTo(tenDays))
	})

	It("returns the default check threshold", func() {
		threshold := getCheckThreshold()
		Expect(threshold).To(BeEquivalentTo(defaultExpiringThreshold))
	})

	It("returns the default check threshold if the configuration is a negative value", func() {
		configuration.Current.ExpiringCheckThreshold = -1
		threshold := getCheckThreshold()
		Expect(threshold).To(BeEquivalentTo(defaultExpiringThreshold))
	})

	It("returns a valid threshold of 10 days", func() {
		configuration.Current.ExpiringCheckThreshold = 10
		threshold := getCheckThreshold()
		Expect(threshold).To(BeEquivalentTo(tenDays))
	})
})
