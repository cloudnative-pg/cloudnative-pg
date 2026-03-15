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
	"context"
	"os"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	controllerScheme "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	operatorDeploymentName = "cnpg-controller-manager"
	operatorNamespaceName  = "operator-namespace"
)

var (
	pkiEnvironmentTemplate = PublicKeyInfrastructure{
		CertDir:                            "/tmp",
		CaSecretName:                       "ca-secret",
		SecretName:                         "webhook-secret-name",
		ServiceName:                        "webhook-service",
		OperatorNamespace:                  operatorNamespaceName,
		MutatingWebhookConfigurationName:   "mutating-webhook",
		ValidatingWebhookConfigurationName: "validating-webhook",
	}

	mutatingWebhookTemplate = admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: pkiEnvironmentTemplate.MutatingWebhookConfigurationName,
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				ClientConfig: admissionregistrationv1.WebhookClientConfig{},
			},
		},
	}

	validatingWebhookTemplate = admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: pkiEnvironmentTemplate.ValidatingWebhookConfigurationName,
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				ClientConfig: admissionregistrationv1.WebhookClientConfig{},
			},
		},
	}
)

func createFakeOperatorDeployment(ctx context.Context, kubeClient client.Client) error {
	operatorDep := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorDeploymentName,
			Namespace: operatorNamespaceName,
			Labels: map[string]string{
				"app.kubernetes.io/name": "cloudnative-pg",
			},
		},
		Spec: appsv1.DeploymentSpec{},
	}

	return kubeClient.Create(ctx, &operatorDep)
}

func generateFakeClient() client.Client {
	scheme := controllerScheme.BuildWithAllKnownScheme()
	return fake.NewClientBuilder().
		WithScheme(scheme).
		Build()
}

var _ = Describe("Root CA secret generation", func() {
	pki := PublicKeyInfrastructure{
		OperatorNamespace: operatorNamespaceName,
		CaSecretName:      "ca-secret-name",
	}

	It("must generate a new CA secret when it doesn't already exist", func(ctx SpecContext) {
		kubeClient := generateFakeClient()
		err := createFakeOperatorDeployment(ctx, kubeClient)
		Expect(err).ToNot(HaveOccurred())

		secret, err := pki.ensureRootCACertificate(ctx, kubeClient)
		Expect(err).ToNot(HaveOccurred())

		Expect(secret.Namespace).To(Equal(operatorNamespaceName))
		Expect(secret.Name).To(Equal("ca-secret-name"))

		caSecret := corev1.Secret{}
		err = kubeClient.Get(ctx, client.ObjectKey{Name: "ca-secret-name", Namespace: operatorNamespaceName}, &caSecret)
		Expect(err).ToNot(HaveOccurred())
	})

	It("must adopt the current certificate if it is valid", func(ctx SpecContext) {
		kubeClient := generateFakeClient()
		ca, err := CreateRootCA("ca-secret-name", operatorNamespaceName)
		Expect(err).ToNot(HaveOccurred())

		secret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
		err = kubeClient.Create(ctx, secret)
		Expect(err).ToNot(HaveOccurred())

		resultingSecret, err := pki.ensureRootCACertificate(ctx, kubeClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(resultingSecret.Namespace).To(Equal(operatorNamespaceName))
		Expect(resultingSecret.Name).To(Equal("ca-secret-name"))
	})

	It("must renew the CA certificate if it is not valid", func(ctx SpecContext) {
		kubeClient := generateFakeClient()
		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-90 * 24 * time.Hour)
		ca, err := createCAWithValidity(notBefore, notAfter,
			nil, nil, "root", operatorNamespaceName)
		Expect(err).ToNot(HaveOccurred())

		secret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
		err = kubeClient.Create(ctx, secret)
		Expect(err).ToNot(HaveOccurred())

		// The secret should have been renewed now
		resultingSecret, err := pki.ensureRootCACertificate(ctx, kubeClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(resultingSecret.Namespace).To(Equal(operatorNamespaceName))
		Expect(resultingSecret.Name).To(Equal("ca-secret-name"))

		caPair, err := ParseCASecret(resultingSecret)
		Expect(err).ToNot(HaveOccurred())

		cert, err := caPair.ParseCertificate()
		Expect(err).ToNot(HaveOccurred())

		Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
		Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))
	})
})

var _ = Describe("Webhook certificate validation", func() {
	When("we have a valid CA secret", Ordered, func() {
		kubeClient := generateFakeClient()
		pki := pkiEnvironmentTemplate

		var caSecret *corev1.Secret

		It("sets us the root CA environment", func(ctx SpecContext) {
			err := createFakeOperatorDeployment(ctx, kubeClient)
			Expect(err).ToNot(HaveOccurred())

			ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)
			caSecret = ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
			err = kubeClient.Create(ctx, caSecret)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should correctly generate a pki certificate", func(ctx SpecContext) {
			webhookSecret, err := pki.ensureCertificate(ctx, kubeClient, caSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(webhookSecret.Name).To(Equal(pki.SecretName))
			Expect(webhookSecret.Namespace).To(Equal(pki.OperatorNamespace))

			// Verify that the webhook secret contains the CA certificate
			Expect(webhookSecret.Data).To(HaveKey(CACertKey))
			Expect(webhookSecret.Data[CACertKey]).To(Equal(caSecret.Data[CACertKey]))

			pair, err := ParseServerSecret(webhookSecret)
			Expect(err).ToNot(HaveOccurred())

			cert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
			Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))
		})
	})

	When("we have a valid CA and webhook secret", Ordered, func() {
		kubeClient := generateFakeClient()
		pki := pkiEnvironmentTemplate
		var caSecret, webhookSecret *corev1.Secret

		It("should create the secret", func(ctx SpecContext) {
			err := createFakeOperatorDeployment(ctx, kubeClient)
			Expect(err).ToNot(HaveOccurred())

			ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)

			caSecret = ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
			err = kubeClient.Create(ctx, caSecret)
			Expect(err).ToNot(HaveOccurred())
			webhookSecret, _ = pki.ensureCertificate(ctx, kubeClient, caSecret)
		})

		It("must reuse them", func(ctx SpecContext) {
			currentWebhookSecret, err := pki.ensureCertificate(ctx, kubeClient, caSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(webhookSecret.Data).To(BeEquivalentTo(currentWebhookSecret.Data))
		})
	})

	When("we have a valid CA secret and expired webhook secret", Ordered, func() {
		kubeClient := generateFakeClient()
		pki := pkiEnvironmentTemplate

		caSecret := &corev1.Secret{}
		serverSecret := &corev1.Secret{}

		It("sets up the environment", func(ctx SpecContext) {
			err := createFakeOperatorDeployment(ctx, kubeClient)
			Expect(err).ToNot(HaveOccurred())

			ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)
			caSecret = ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")

			notAfter := time.Now().Add(-10 * time.Hour)
			notBefore := notAfter.Add(-90 * 24 * time.Hour)
			server, _ := ca.createAndSignPairWithValidity("this.server.com", notBefore, notAfter, CertTypeServer, nil)
			serverSecret = server.GenerateCertificateSecret(operatorNamespaceName, "pki-secret-name")

			err = kubeClient.Create(ctx, caSecret)
			Expect(err).ToNot(HaveOccurred())

			err = kubeClient.Create(ctx, serverSecret)
			Expect(err).ToNot(HaveOccurred())
		})

		It("must renew the secret", func(ctx SpecContext) {
			currentServerSecret, err := pki.ensureCertificate(ctx, kubeClient, caSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(serverSecret.Data).To(Not(BeEquivalentTo(currentServerSecret.Data)))

			// Verify that the renewed secret now includes the CA certificate
			Expect(currentServerSecret.Data).To(HaveKey(CACertKey))
			Expect(currentServerSecret.Data[CACertKey]).To(Equal(caSecret.Data[CACertKey]))

			pair, err := ParseServerSecret(currentServerSecret)
			Expect(err).ToNot(HaveOccurred())

			cert, err := pair.ParseCertificate()
			Expect(err).ToNot(HaveOccurred())

			Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
			Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))
		})
	})
})

var _ = Describe("TLS certificates injection", func() {
	pki := pkiEnvironmentTemplate

	// Create a CA and the pki secret
	ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)
	webhookPair, _ := ca.CreateAndSignPair("pki-service.operator-namespace.svc", CertTypeServer, nil)
	// Use GenerateWebhookCertificateSecret to include the CA certificate
	webhookSecret := webhookPair.GenerateWebhookCertificateSecret(pki.OperatorNamespace, pki.SecretName, ca.Certificate)

	kubeClient := generateFakeClient()

	It("set up the environment", func(ctx SpecContext) {
		err := kubeClient.Create(ctx, webhookSecret)
		Expect(err).ToNot(HaveOccurred())
	})

	It("inject the pki certificate in the mutating pki", func(ctx SpecContext) {
		// Create the mutating pki
		mutatingWebhook := mutatingWebhookTemplate

		err := kubeClient.Create(ctx, &mutatingWebhook)
		Expect(err).ToNot(HaveOccurred())

		err = pki.injectPublicKeyIntoMutatingWebhook(ctx, kubeClient, webhookSecret)
		Expect(err).ToNot(HaveOccurred())

		updatedWebhook := admissionregistrationv1.MutatingWebhookConfiguration{}

		err = kubeClient.Get(ctx, client.ObjectKey{Name: pki.MutatingWebhookConfigurationName}, &updatedWebhook)
		Expect(err).ToNot(HaveOccurred())

		// Now it should use the CA certificate from ca.crt instead of tls.crt
		Expect(updatedWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data[CACertKey]))
	})

	It("inject the pki certificate in the validating pki", func(ctx SpecContext) {
		// Create the validating pki
		validatingWebhook := validatingWebhookTemplate

		err := kubeClient.Create(ctx, &validatingWebhook)
		Expect(err).ToNot(HaveOccurred())

		err = pki.injectPublicKeyIntoValidatingWebhook(ctx, kubeClient, webhookSecret)
		Expect(err).ToNot(HaveOccurred())

		updatedWebhook := admissionregistrationv1.ValidatingWebhookConfiguration{}
		err = kubeClient.Get(ctx, client.ObjectKey{Name: pki.ValidatingWebhookConfigurationName}, &updatedWebhook)
		Expect(err).ToNot(HaveOccurred())

		// Now it should use the CA certificate from ca.crt instead of tls.crt
		Expect(updatedWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data[CACertKey]))
	})
})

var _ = Describe("Webhook environment creation", func() {
	It("should setup the certificates and the webhooks", func(ctx SpecContext) {
		tempDirName, err := os.MkdirTemp("/tmp", "cert_*")
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			err = os.RemoveAll(tempDirName)
			Expect(err).ToNot(HaveOccurred())
		}()

		pki := pkiEnvironmentTemplate
		mutatingWebhook := mutatingWebhookTemplate
		validatingWebhook := validatingWebhookTemplate

		kubeClient := generateFakeClient()

		err = createFakeOperatorDeployment(ctx, kubeClient)
		Expect(err).ToNot(HaveOccurred())

		err = kubeClient.Create(ctx, &mutatingWebhook)
		Expect(err).ToNot(HaveOccurred())

		err = kubeClient.Create(ctx, &validatingWebhook)
		Expect(err).ToNot(HaveOccurred())

		ca, err := pki.ensureRootCACertificate(ctx, kubeClient)
		Expect(err).ToNot(HaveOccurred())

		_, err = pki.setupWebhooksCertificate(ctx, kubeClient, ca)
		Expect(err).ToNot(HaveOccurred())

		webhookSecret := corev1.Secret{}
		err = kubeClient.Get(
			ctx,
			client.ObjectKey{Namespace: pki.OperatorNamespace, Name: pki.SecretName},
			&webhookSecret)
		Expect(err).ToNot(HaveOccurred())
		Expect(webhookSecret.Namespace).To(Equal(pki.OperatorNamespace))
		Expect(webhookSecret.Name).To(Equal(pki.SecretName))

		updatedMutatingWebhook := admissionregistrationv1.MutatingWebhookConfiguration{}
		err = kubeClient.Get(
			ctx,
			client.ObjectKey{Name: pki.MutatingWebhookConfigurationName},
			&updatedMutatingWebhook)
		Expect(err).ToNot(HaveOccurred())
		// The CABundle should now contain the CA certificate, not the server certificate
		Expect(updatedMutatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data[CACertKey]))

		updatedValidatingWebhook := admissionregistrationv1.ValidatingWebhookConfiguration{}
		err = kubeClient.Get(
			ctx,
			client.ObjectKey{Name: pki.ValidatingWebhookConfigurationName},
			&updatedValidatingWebhook)
		Expect(err).ToNot(HaveOccurred())
		// The CABundle should now contain the CA certificate, not the server certificate
		Expect(updatedValidatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data[CACertKey]))
	})
})
