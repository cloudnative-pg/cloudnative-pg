/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package certs

import (
	"crypto/x509"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestCerts(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Certs Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = Describe("Keypair generation", func() {
	It("should generate a correct root CA", func() {
		pair, err := CreateCA()
		Expect(err).To(BeNil())

		cert, err := pair.ParseCertificate()
		Expect(err).To(BeNil())

		key, err := pair.ParseECPrivateKey()
		Expect(err).To(BeNil())

		Expect(cert.PublicKey).To(BeEquivalentTo(&key.PublicKey))
		Expect(cert.IsCA).To(BeTrue())
		Expect(cert.BasicConstraintsValid).To(BeTrue())
		Expect(cert.KeyUsage & x509.KeyUsageDigitalSignature).To(Not(Equal(0)))
		Expect(cert.KeyUsage & x509.KeyUsageKeyEncipherment).To(Not(Equal(0)))
		Expect(cert.ExtKeyUsage).To(Equal([]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}))
		Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
		Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))

		// The root CA is autosigned
		Expect(cert.CheckSignatureFrom(cert)).To(BeNil())
	})

	It("should create a CA K8s corev1/secret resource structure", func() {
		pair, err := CreateCA()
		Expect(err).To(BeNil())

		secret := pair.GenerateCASecret("namespace", "name")
		Expect(secret.Namespace).To(Equal("namespace"))
		Expect(secret.Name).To(Equal("name"))
		Expect(secret.Data["ca.crt"]).To(Equal(pair.Certificate))
		Expect(secret.Data["ca.key"]).To(Equal(pair.Private))
	})

	It("should be able to renew an existing CA certificate", func() {
		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-365 * 24 * time.Hour)
		ca, err := createCAWithValidity(notBefore, notAfter)
		Expect(err).To(BeNil())

		privateKey, err := ca.ParseECPrivateKey()
		Expect(err).To(BeNil())

		err = ca.RenewCertificate(privateKey)
		Expect(err).To(BeNil())

		newCert, err := ca.ParseCertificate()
		Expect(err).To(BeNil())

		Expect(newCert.NotBefore).To(BeTemporally("<", time.Now()))
		Expect(newCert.NotAfter).To(BeTemporally(">", time.Now()))
	})

	It("should be able to verify if a certificate will expire", func() {
		When("it is expiring", func() {
			notAfter := time.Now().Add(-10 * time.Hour)
			notBefore := notAfter.Add(-365 * 24 * time.Hour)
			ca, err := createCAWithValidity(notBefore, notAfter)
			Expect(err).To(BeNil())
			Expect(ca.IsExpiring()).To(BeTrue())
		})

		When("it's not expiring", func() {
			ca, err := CreateCA()
			Expect(err).To(BeNil())
			Expect(ca.IsExpiring()).To(BeFalse())
		})
	})

	When("we have a CA generated", func() {
		It("should successfully generate a leaf certificate", func() {
			rootCA, err := CreateCA()
			Expect(err).To(BeNil())

			pair, err := rootCA.CreateAndSignPair("this.host.name.com")
			Expect(err).To(BeNil())

			cert, err := pair.ParseCertificate()
			Expect(err).To(BeNil())

			key, err := pair.ParseECPrivateKey()
			Expect(err).To(BeNil())

			Expect(cert.PublicKey).To(BeEquivalentTo(&key.PublicKey))
			Expect(cert.IsCA).To(BeFalse())
			Expect(cert.BasicConstraintsValid).To(BeTrue())
			Expect(cert.KeyUsage & x509.KeyUsageDigitalSignature).To(Not(Equal(0)))
			Expect(cert.KeyUsage & x509.KeyUsageKeyEncipherment).To(Not(Equal(0)))
			Expect(cert.ExtKeyUsage).To(Equal([]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}))
			Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
			Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))
			Expect(cert.VerifyHostname("this.host.name.com")).To(BeNil())

			caCert, err := rootCA.ParseCertificate()
			Expect(err).To(BeNil())

			Expect(cert.CheckSignatureFrom(caCert)).To(BeNil())
		})

		It("should create a CA K8s corev1/secret resource structure", func() {
			rootCA, err := CreateCA()
			Expect(err).To(BeNil())

			pair, err := rootCA.CreateAndSignPair("this.host.name.com")
			Expect(err).To(BeNil())

			secret := pair.GenerateServerSecret("namespace", "name")
			Expect(secret.Namespace).To(Equal("namespace"))
			Expect(secret.Name).To(Equal("name"))
			Expect(secret.Data["tls.crt"]).To(Equal(pair.Certificate))
			Expect(secret.Data["tls.key"]).To(Equal(pair.Private))
		})
	})
})
