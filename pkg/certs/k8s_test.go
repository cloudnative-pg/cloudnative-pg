/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package certs

import (
	"context"
	"os"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
		CustomResourceDefinitionsName: []string{
			"clusters.postgresqlx.cnpg.io",
			"backups.postgresqlx.cnpg.io",
		},
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

	firstCrdTemplate = apiextensionv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: pkiEnvironmentTemplate.CustomResourceDefinitionsName[0],
		},
		Spec: apiextensionv1.CustomResourceDefinitionSpec{
			Conversion: &apiextensionv1.CustomResourceConversion{
				Webhook: &apiextensionv1.WebhookConversion{
					ConversionReviewVersions: []string{"v1", "v1alpha1"},
					ClientConfig:             &apiextensionv1.WebhookClientConfig{},
				},
			},
		},
	}

	secondCrdTemplate = apiextensionv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: pkiEnvironmentTemplate.CustomResourceDefinitionsName[1],
		},
		Spec: apiextensionv1.CustomResourceDefinitionSpec{
			Conversion: &apiextensionv1.CustomResourceConversion{
				Webhook: &apiextensionv1.WebhookConversion{
					ConversionReviewVersions: []string{"v1", "v1alpha1"},
					ClientConfig:             &apiextensionv1.WebhookClientConfig{},
				},
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
		Expect(err).To(BeNil())

		secret, err := pki.ensureRootCACertificate(ctx, kubeClient)
		Expect(err).To(BeNil())

		Expect(secret.Namespace).To(Equal(operatorNamespaceName))
		Expect(secret.Name).To(Equal("ca-secret-name"))

		caSecret := corev1.Secret{}
		err = kubeClient.Get(ctx, client.ObjectKey{Name: "ca-secret-name", Namespace: operatorNamespaceName}, &caSecret)
		Expect(err).To(BeNil())
	})

	It("must adopt the current certificate if it is valid", func(ctx SpecContext) {
		kubeClient := generateFakeClient()
		ca, err := CreateRootCA("ca-secret-name", operatorNamespaceName)
		Expect(err).To(BeNil())

		secret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
		err = kubeClient.Create(ctx, secret)
		Expect(err).To(BeNil())

		resultingSecret, err := pki.ensureRootCACertificate(ctx, kubeClient)
		Expect(err).To(BeNil())
		Expect(resultingSecret.Namespace).To(Equal(operatorNamespaceName))
		Expect(resultingSecret.Name).To(Equal("ca-secret-name"))
	})

	It("must renew the CA certificate if it is not valid", func(ctx SpecContext) {
		kubeClient := generateFakeClient()
		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-90 * 24 * time.Hour)
		ca, err := createCAWithValidity(notBefore, notAfter,
			nil, nil, "root", operatorNamespaceName)
		Expect(err).To(BeNil())

		secret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
		err = kubeClient.Create(ctx, secret)
		Expect(err).To(BeNil())

		// The secret should have been renewed now
		resultingSecret, err := pki.ensureRootCACertificate(ctx, kubeClient)
		Expect(err).To(BeNil())
		Expect(resultingSecret.Namespace).To(Equal(operatorNamespaceName))
		Expect(resultingSecret.Name).To(Equal("ca-secret-name"))

		caPair, err := ParseCASecret(resultingSecret)
		Expect(err).To(BeNil())

		cert, err := caPair.ParseCertificate()
		Expect(err).To(BeNil())

		Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
		Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))
	})
})

var _ = Describe("Webhook certificate validation", func() {
	When("we have a valid CA secret", func() {
		kubeClient := generateFakeClient()
		pki := pkiEnvironmentTemplate

		var caSecret *corev1.Secret

		It("sets us the root CA environment", func(ctx SpecContext) {
			err := createFakeOperatorDeployment(ctx, kubeClient)
			Expect(err).To(BeNil())

			ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)
			caSecret = ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
			err = kubeClient.Create(ctx, caSecret)
			Expect(err).To(BeNil())
		})

		It("should correctly generate a pki certificate", func(ctx SpecContext) {
			webhookSecret, err := pki.ensureCertificate(ctx, kubeClient, caSecret)
			Expect(err).To(BeNil())
			Expect(webhookSecret.Name).To(Equal(pki.SecretName))
			Expect(webhookSecret.Namespace).To(Equal(pki.OperatorNamespace))

			pair, err := ParseServerSecret(webhookSecret)
			Expect(err).To(BeNil())

			cert, err := pair.ParseCertificate()
			Expect(err).To(BeNil())

			Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
			Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))
		})
	})

	When("we have a valid CA and webhook secret", func() {
		kubeClient := generateFakeClient()
		pki := pkiEnvironmentTemplate
		var caSecret, webhookSecret *corev1.Secret

		It("should create the secret", func(ctx SpecContext) {
			err := createFakeOperatorDeployment(ctx, kubeClient)
			Expect(err).To(BeNil())

			ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)

			caSecret = ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
			err = kubeClient.Create(ctx, caSecret)
			Expect(err).To(BeNil())
			webhookSecret, _ = pki.ensureCertificate(ctx, kubeClient, caSecret)
		})

		It("must reuse them", func(ctx SpecContext) {
			currentWebhookSecret, err := pki.ensureCertificate(ctx, kubeClient, caSecret)
			Expect(err).To(BeNil())
			Expect(webhookSecret.Data).To(BeEquivalentTo(currentWebhookSecret.Data))
		})
	})

	When("we have a valid CA secret and expired webhook secret", func() {
		kubeClient := generateFakeClient()
		pki := pkiEnvironmentTemplate

		caSecret := &corev1.Secret{}
		serverSecret := &corev1.Secret{}

		It("sets up the environment", func(ctx SpecContext) {
			err := createFakeOperatorDeployment(ctx, kubeClient)
			Expect(err).To(BeNil())

			ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)
			caSecret = ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")

			notAfter := time.Now().Add(-10 * time.Hour)
			notBefore := notAfter.Add(-90 * 24 * time.Hour)
			server, _ := ca.createAndSignPairWithValidity("this.server.com", notBefore, notAfter, CertTypeServer, nil)
			serverSecret = server.GenerateCertificateSecret(operatorNamespaceName, "pki-secret-name")

			err = kubeClient.Create(ctx, caSecret)
			Expect(err).To(BeNil())

			err = kubeClient.Create(ctx, serverSecret)
			Expect(err).To(BeNil())
		})

		It("must renew the secret", func(ctx SpecContext) {
			currentServerSecret, err := pki.ensureCertificate(ctx, kubeClient, caSecret)
			Expect(err).To(BeNil())
			Expect(serverSecret.Data).To(Not(BeEquivalentTo(currentServerSecret.Data)))

			pair, err := ParseServerSecret(currentServerSecret)
			Expect(err).To(BeNil())

			cert, err := pair.ParseCertificate()
			Expect(err).To(BeNil())

			Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
			Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))
		})
	})
})

var _ = Describe("TLS certificates injection", func() {
	pki := pkiEnvironmentTemplate

	// Create a CA and the pki secret
	ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)
	// TODO: caSecret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
	webhookPair, _ := ca.CreateAndSignPair("pki-service.operator-namespace.svc", CertTypeServer, nil)
	webhookSecret := webhookPair.GenerateCertificateSecret(pki.OperatorNamespace, pki.SecretName)

	kubeClient := generateFakeClient()

	It("set up the environment", func(ctx SpecContext) {
		err := kubeClient.Create(ctx, webhookSecret)
		Expect(err).To(BeNil())
	})

	It("inject the pki certificate in the mutating pki", func(ctx SpecContext) {
		// Create the mutating pki
		mutatingWebhook := mutatingWebhookTemplate

		err := kubeClient.Create(ctx, &mutatingWebhook)
		Expect(err).To(BeNil())

		err = pki.injectPublicKeyIntoMutatingWebhook(ctx, kubeClient, webhookSecret)
		Expect(err).To(BeNil())

		updatedWebhook := admissionregistrationv1.MutatingWebhookConfiguration{}

		err = kubeClient.Get(ctx, client.ObjectKey{Name: pki.MutatingWebhookConfigurationName}, &updatedWebhook)
		Expect(err).To(BeNil())

		Expect(updatedWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))
	})

	It("inject the pki certificate in the validating pki", func(ctx SpecContext) {
		// Create the validating pki
		validatingWebhook := validatingWebhookTemplate

		err := kubeClient.Create(ctx, &validatingWebhook)
		Expect(err).To(BeNil())

		err = pki.injectPublicKeyIntoValidatingWebhook(ctx, kubeClient, webhookSecret)
		Expect(err).To(BeNil())

		updatedWebhook := admissionregistrationv1.ValidatingWebhookConfiguration{}
		err = kubeClient.Get(ctx, client.ObjectKey{Name: pki.ValidatingWebhookConfigurationName}, &updatedWebhook)
		Expect(err).To(BeNil())

		Expect(updatedWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))
	})
})

var _ = Describe("Webhook environment creation", func() {
	It("should setup the certificates and the webhooks", func(ctx SpecContext) {
		tempDirName, err := os.MkdirTemp("/tmp", "cert_*")
		Expect(err).To(BeNil())
		defer func() {
			err = os.RemoveAll(tempDirName)
			Expect(err).To(BeNil())
		}()

		pki := pkiEnvironmentTemplate
		mutatingWebhook := mutatingWebhookTemplate
		validatingWebhook := validatingWebhookTemplate
		firstCrd := firstCrdTemplate
		secondCrd := secondCrdTemplate

		kubeClient := generateFakeClient()

		err = createFakeOperatorDeployment(ctx, kubeClient)
		Expect(err).To(BeNil())

		err = kubeClient.Create(ctx, &firstCrd)
		Expect(err).To(BeNil())

		err = kubeClient.Create(ctx, &secondCrd)
		Expect(err).To(BeNil())

		err = kubeClient.Create(ctx, &mutatingWebhook)
		Expect(err).To(BeNil())

		err = kubeClient.Create(ctx, &validatingWebhook)
		Expect(err).To(BeNil())

		ca, err := pki.ensureRootCACertificate(ctx, kubeClient)
		Expect(err).To(BeNil())

		_, err = pki.setupWebhooksCertificate(ctx, kubeClient, ca)
		Expect(err).To(BeNil())

		webhookSecret := corev1.Secret{}
		err = kubeClient.Get(
			ctx,
			client.ObjectKey{Namespace: pki.OperatorNamespace, Name: pki.SecretName},
			&webhookSecret)
		Expect(err).To(BeNil())
		Expect(webhookSecret.Namespace).To(Equal(pki.OperatorNamespace))
		Expect(webhookSecret.Name).To(Equal(pki.SecretName))

		updatedMutatingWebhook := admissionregistrationv1.MutatingWebhookConfiguration{}
		err = kubeClient.Get(
			ctx,
			client.ObjectKey{Name: pki.MutatingWebhookConfigurationName},
			&updatedMutatingWebhook)
		Expect(err).To(BeNil())
		Expect(updatedMutatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))

		updatedValidatingWebhook := admissionregistrationv1.ValidatingWebhookConfiguration{}
		err = kubeClient.Get(
			ctx,
			client.ObjectKey{Name: pki.ValidatingWebhookConfigurationName},
			&updatedValidatingWebhook)
		Expect(err).To(BeNil())
		Expect(updatedValidatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))

		updatedFirstCrd := apiextensionv1.CustomResourceDefinition{}
		err = kubeClient.Get(
			ctx,
			client.ObjectKey{Name: pki.CustomResourceDefinitionsName[0]},
			&updatedFirstCrd)
		Expect(err).To(BeNil())
		Expect(updatedFirstCrd.Spec.Conversion.Webhook.ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))

		updatedSecondCrd := apiextensionv1.CustomResourceDefinition{}
		err = kubeClient.Get(
			ctx,
			client.ObjectKey{Name: pki.CustomResourceDefinitionsName[1]},
			&updatedSecondCrd)
		Expect(err).To(BeNil())
		Expect(updatedSecondCrd.Spec.Conversion.Webhook.ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))
	})
})
