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
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	fakeApiExtension "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

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
			"clusters.postgresql.cnpg.io",
			"backups.postgresql.cnpg.io",
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

func generateFakeOperatorDeployment(clientSet *fake.Clientset) {
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
	_, err := clientSet.AppsV1().Deployments(operatorNamespaceName).
		Create(context.TODO(), &operatorDep, metav1.CreateOptions{})
	Expect(err).To(BeNil())
}

var _ = Describe("Root CA secret generation", func() {
	pki := PublicKeyInfrastructure{
		OperatorNamespace: operatorNamespaceName,
		CaSecretName:      "ca-secret-name",
	}

	It("must generate a new CA secret when it doesn't already exist", func() {
		clientSet := fake.NewSimpleClientset()
		generateFakeOperatorDeployment(clientSet)
		secret, err := pki.ensureRootCACertificate(context.TODO(), clientSet)
		Expect(err).To(BeNil())

		Expect(secret.Namespace).To(Equal(operatorNamespaceName))
		Expect(secret.Name).To(Equal("ca-secret-name"))

		_, err = clientSet.CoreV1().Secrets(operatorNamespaceName).Get(
			context.TODO(), "ca-secret-name", metav1.GetOptions{})
		Expect(err).To(BeNil())
	})

	It("must adopt the current certificate if it is valid", func() {
		ca, err := CreateRootCA("ca-secret-name", operatorNamespaceName)
		Expect(err).To(BeNil())

		secret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
		clientSet := fake.NewSimpleClientset(secret)

		resultingSecret, err := pki.ensureRootCACertificate(context.TODO(), clientSet)
		Expect(err).To(BeNil())
		Expect(resultingSecret.Namespace).To(Equal(operatorNamespaceName))
		Expect(resultingSecret.Name).To(Equal("ca-secret-name"))
	})

	It("must renew the CA certificate if it is not valid", func() {
		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-90 * 24 * time.Hour)
		ca, err := createCAWithValidity(notBefore, notAfter,
			nil, nil, "root", operatorNamespaceName)
		Expect(err).To(BeNil())

		secret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
		clientSet := fake.NewSimpleClientset(secret)

		// The secret should have been renewed now
		resultingSecret, err := pki.ensureRootCACertificate(context.TODO(), clientSet)
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
		clientSet := fake.NewSimpleClientset()
		generateFakeOperatorDeployment(clientSet)

		ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)
		caSecret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
		err := clientSet.Tracker().Add(caSecret)
		Expect(err).To(BeNil())
		pki := pkiEnvironmentTemplate

		It("should correctly generate a pki certificate", func() {
			webhookSecret, err := pki.ensureCertificate(context.TODO(), clientSet, caSecret)
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
		clientSet := fake.NewSimpleClientset()
		generateFakeOperatorDeployment(clientSet)

		ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)
		caSecret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
		err := clientSet.Tracker().Add(caSecret)
		Expect(err).To(BeNil())
		pki := pkiEnvironmentTemplate
		webhookSecret, _ := pki.ensureCertificate(context.TODO(), clientSet, caSecret)

		It("must reuse them", func() {
			currentWebhookSecret, err := pki.ensureCertificate(context.TODO(), clientSet, caSecret)
			Expect(err).To(BeNil())
			Expect(webhookSecret.Data).To(BeEquivalentTo(currentWebhookSecret.Data))
		})
	})

	When("we have a valid CA secret and expired webhook secret", func() {
		clientSet := fake.NewSimpleClientset()
		generateFakeOperatorDeployment(clientSet)

		ca, _ := CreateRootCA("ca-secret-name", operatorNamespaceName)
		caSecret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")

		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-90 * 24 * time.Hour)
		server, _ := ca.createAndSignPairWithValidity("this.server.com", notBefore, notAfter, CertTypeServer, nil)
		serverSecret := server.GenerateCertificateSecret(operatorNamespaceName, "pki-secret-name")

		err := clientSet.Tracker().Add(caSecret)
		Expect(err).To(BeNil())

		err = clientSet.Tracker().Add(serverSecret)
		Expect(err).To(BeNil())

		pki := pkiEnvironmentTemplate

		It("must renew the secret", func() {
			currentServerSecret, err := pki.ensureCertificate(context.TODO(), clientSet, caSecret)
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
	caSecret := ca.GenerateCASecret(operatorNamespaceName, "ca-secret-name")
	webhookPair, _ := ca.CreateAndSignPair("pki-service.operator-namespace.svc", CertTypeServer, nil)
	webhookSecret := webhookPair.GenerateCertificateSecret(pki.OperatorNamespace, pki.SecretName)

	It("inject the pki certificate in the mutating pki", func() {
		// Create the mutating pki
		mutatingWebhook := mutatingWebhookTemplate
		clientSet := fake.NewSimpleClientset(caSecret, webhookSecret, &mutatingWebhook)

		err := pki.injectPublicKeyIntoMutatingWebhook(context.TODO(), clientSet, webhookSecret)
		Expect(err).To(BeNil())

		updatedWebhook, err := clientSet.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(
			context.TODO(), pki.MutatingWebhookConfigurationName, metav1.GetOptions{})
		Expect(err).To(BeNil())

		Expect(updatedWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))
	})

	It("inject the pki certificate in the validating pki", func() {
		// Create the validating pki
		validatingWebhook := validatingWebhookTemplate
		clientSet := fake.NewSimpleClientset(caSecret, webhookSecret, &validatingWebhook)

		err := pki.injectPublicKeyIntoValidatingWebhook(context.TODO(), clientSet, webhookSecret)
		Expect(err).To(BeNil())

		updatedWebhook, err := clientSet.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
			context.TODO(), pki.ValidatingWebhookConfigurationName, metav1.GetOptions{})
		Expect(err).To(BeNil())

		Expect(updatedWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))
	})
})

var _ = Describe("Webhook environment creation", func() {
	It("should setup the certificates and the webhooks", func() {
		tempDirName, err := os.MkdirTemp("/tmp", "cert_*")
		Expect(err).To(BeNil())
		defer func() {
			err = os.RemoveAll(tempDirName)
			Expect(err).To(BeNil())
		}()

		ctx := context.Background()

		pki := pkiEnvironmentTemplate
		mutatingWebhook := mutatingWebhookTemplate
		validatingWebhook := validatingWebhookTemplate
		firstCrd := firstCrdTemplate
		secondCrd := secondCrdTemplate

		clientSet := fake.NewSimpleClientset(&mutatingWebhook, &validatingWebhook)
		generateFakeOperatorDeployment(clientSet)

		apiClientSet := fakeApiExtension.NewSimpleClientset(&firstCrd, &secondCrd)

		ca, err := pki.ensureRootCACertificate(ctx, clientSet)
		Expect(err).To(BeNil())

		_, err = pki.setupWebhooksCertificate(ctx, clientSet, apiClientSet, ca)
		Expect(err).To(BeNil())

		webhookSecret, err := clientSet.CoreV1().Secrets(
			pki.OperatorNamespace).Get(ctx, pki.SecretName, metav1.GetOptions{})
		Expect(err).To(BeNil())
		Expect(webhookSecret.Namespace).To(Equal(pki.OperatorNamespace))
		Expect(webhookSecret.Name).To(Equal(pki.SecretName))

		updatedMutatingWebhook, err := clientSet.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(
			ctx, pki.MutatingWebhookConfigurationName, metav1.GetOptions{})
		Expect(err).To(BeNil())
		Expect(updatedMutatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))

		updatedValidatingWebhook, err := clientSet.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
			ctx, pki.ValidatingWebhookConfigurationName, metav1.GetOptions{})
		Expect(err).To(BeNil())
		Expect(updatedValidatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))

		updatedFirstCrd, err := apiClientSet.ApiextensionsV1().CustomResourceDefinitions().Get(
			ctx, pki.CustomResourceDefinitionsName[0], metav1.GetOptions{})
		Expect(err).To(BeNil())
		Expect(updatedFirstCrd.Spec.Conversion.Webhook.ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))

		updatedSecondCrd, err := apiClientSet.ApiextensionsV1().CustomResourceDefinitions().Get(
			ctx, pki.CustomResourceDefinitionsName[1], metav1.GetOptions{})
		Expect(err).To(BeNil())
		Expect(updatedSecondCrd.Spec.Conversion.Webhook.ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))
	})
})
