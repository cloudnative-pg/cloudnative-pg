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

package certificate

import (
	"crypto/tls"
	"path"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeServerCertificateHandler struct {
	certificate *tls.Certificate
}

func (f *fakeServerCertificateHandler) SetServerCertificate(certificate *tls.Certificate) {
	f.certificate = certificate
}

func (f *fakeServerCertificateHandler) GetServerCertificate() *tls.Certificate {
	return f.certificate
}

var _ = Describe("refresh certificate files from a secret", func() {
	publicKeyContent := []byte("public_key")
	privateKeyContent := []byte("private_key")
	fakeReconciler := Reconciler{}
	fakeSecret := corev1.Secret{
		Data: map[string][]byte{
			corev1.TLSCertKey:       publicKeyContent,
			corev1.TLSPrivateKeyKey: privateKeyContent,
		},
	}

	It("writing the required files into a directory", func(ctx SpecContext) {
		tempDir := GinkgoT().TempDir()
		certificateLocation := path.Join(tempDir, "tls.crt")
		privateKeyLocation := path.Join(tempDir, "tls.key")

		By("having code create new files", func() {
			status, err := fakeReconciler.refreshCertificateFilesFromSecret(
				ctx, &fakeSecret, certificateLocation, privateKeyLocation)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeTrue())

			writtenPublicKey, err := fileutils.ReadFile(certificateLocation)
			Expect(err).ToNot(HaveOccurred())
			Expect(writtenPublicKey).To(Equal(publicKeyContent))

			writtenPrivateKey, err := fileutils.ReadFile(privateKeyLocation)
			Expect(err).ToNot(HaveOccurred())
			Expect(writtenPrivateKey).To(Equal(privateKeyContent))
		})

		By("writing again the same data, and verifying that the certificate refresh is not triggered", func() {
			status, err := fakeReconciler.refreshCertificateFilesFromSecret(
				ctx, &fakeSecret, certificateLocation, privateKeyLocation)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeFalse())
		})

		By("changing the file contents, and verifying that the certificate refresh is triggered", func() {
			newPublicKeyContent := []byte("changed public key")
			newPrivateKeyContent := []byte("changed private key")

			changedSecret := fakeSecret.DeepCopy()
			changedSecret.Data[corev1.TLSCertKey] = newPublicKeyContent
			changedSecret.Data[corev1.TLSPrivateKeyKey] = newPrivateKeyContent

			status, err := fakeReconciler.refreshCertificateFilesFromSecret(
				ctx, changedSecret, certificateLocation, privateKeyLocation)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeTrue())
		})
	})
})

var _ = Describe("refresh CA from a secret", func() {
	publicKeyContent := []byte("public_key")
	fakeReconciler := Reconciler{}
	fakeSecret := corev1.Secret{
		Data: map[string][]byte{
			certs.CACertKey: publicKeyContent,
		},
	}

	It("writing the required files into a directory", func(ctx SpecContext) {
		tempDir := GinkgoT().TempDir()
		certificateLocation := path.Join(tempDir, "ca.crt")

		By("having code create new files", func() {
			status, err := fakeReconciler.refreshCAFromSecret(
				ctx, &fakeSecret, certificateLocation)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeTrue())

			writtenPublicKey, err := fileutils.ReadFile(certificateLocation)
			Expect(err).ToNot(HaveOccurred())
			Expect(writtenPublicKey).To(Equal(publicKeyContent))
		})

		By("writing again the same data, and verifying that the certificate refresh is not triggered", func() {
			status, err := fakeReconciler.refreshCAFromSecret(
				ctx, &fakeSecret, certificateLocation)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeFalse())
		})

		By("changing the file contents, and verifying that the certificate refresh is triggered", func() {
			newPublicKeyContent := []byte("changed public key")

			changedSecret := fakeSecret.DeepCopy()
			changedSecret.Data[certs.CACertKey] = newPublicKeyContent

			status, err := fakeReconciler.refreshCAFromSecret(
				ctx, changedSecret, certificateLocation)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeTrue())
		})
	})
})

var _ = Describe("server certificate refresh handler", func() {
	It("refresh the server certificate", func() {
		var secret *corev1.Secret

		By("creating a new root CA", func() {
			root, err := certs.CreateRootCA("common-name", "organization-unit")
			Expect(err).ToNot(HaveOccurred())

			pair, err := root.CreateAndSignPair("host", certs.CertTypeServer, nil)
			Expect(err).ToNot(HaveOccurred())

			secret = pair.GenerateCertificateSecret("default", "pair")
		})

		By("triggering the certificate refresh when no handler is set", func() {
			fakeReconciler := Reconciler{}
			err := fakeReconciler.refreshInstanceCertificateFromSecret(secret)
			Expect(err).Error().Should(Equal(ErrNoServerCertificateHandler))
		})

		By("triggering the certificate refresh when a handler is set", func() {
			fakeReconciler := Reconciler{
				serverCertificateHandler: &fakeServerCertificateHandler{},
			}

			err := fakeReconciler.refreshInstanceCertificateFromSecret(secret)
			Expect(err).ShouldNot(HaveOccurred())

			cert := fakeReconciler.serverCertificateHandler.GetServerCertificate()
			Expect(cert).ToNot(BeNil())
		})
	})
})
