/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package certs

import (
	"context"
	"io/ioutil"
	"os"
	"path"
	"time"

	"k8s.io/api/admissionregistration/v1beta1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	fakeApiExtension "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	pkiEnvironmentTemplate = PublicKeyInfrastructure{
		CertDir:                            "/tmp",
		CaSecretName:                       "ca-secret",
		SecretName:                         "webhook-secret-name",
		ServiceName:                        "webhook-service",
		OperatorNamespace:                  "operator-namespace",
		MutatingWebhookConfigurationName:   "mutating-webhook",
		ValidatingWebhookConfigurationName: "validating-webhook",
		CustomResourceDefinitionsName: []string{
			"clusters.postgresql.k8s.enterprisedb.io",
			"backups.postgresql.k8s.enterprisedb.io",
		},
	}

	mutatingWebhookTemplate = v1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: pkiEnvironmentTemplate.MutatingWebhookConfigurationName,
		},
		Webhooks: []v1beta1.MutatingWebhook{
			{
				ClientConfig: v1beta1.WebhookClientConfig{},
			},
		},
	}

	validatingWebhookTemplate = v1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: pkiEnvironmentTemplate.ValidatingWebhookConfigurationName,
		},
		Webhooks: []v1beta1.ValidatingWebhook{
			{
				ClientConfig: v1beta1.WebhookClientConfig{},
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

var _ = Describe("Root CA secret generation", func() {
	It("must generate a new CA secret when it doesn't already exist", func() {
		clientSet := fake.NewSimpleClientset()
		secret, err := EnsureRootCACertificate(context.TODO(), clientSet, "operator-namespace", "ca-secret-name")
		Expect(err).To(BeNil())

		Expect(secret.Namespace).To(Equal("operator-namespace"))
		Expect(secret.Name).To(Equal("ca-secret-name"))

		_, err = clientSet.CoreV1().Secrets("operator-namespace").Get(
			context.TODO(), "ca-secret-name", metav1.GetOptions{})
		Expect(err).To(BeNil())
	})

	It("must adopt the current certificate if it is valid", func() {
		ca, err := CreateRootCA("ca-secret-name", "operator-namespace")
		Expect(err).To(BeNil())

		secret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
		clientSet := fake.NewSimpleClientset(secret)

		resultingSecret, err := EnsureRootCACertificate(context.TODO(), clientSet, "operator-namespace", "ca-secret-name")
		Expect(err).To(BeNil())
		Expect(resultingSecret.Namespace).To(Equal("operator-namespace"))
		Expect(resultingSecret.Name).To(Equal("ca-secret-name"))
	})

	It("must renew the CA certificate if it is not valid", func() {
		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-90 * 24 * time.Hour)
		ca, err := createCAWithValidity(notBefore, notAfter, nil, nil, "root", "operator-namespace")
		Expect(err).To(BeNil())

		secret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
		clientSet := fake.NewSimpleClientset(secret)

		// The secret should have been renewed now
		resultingSecret, err := EnsureRootCACertificate(context.TODO(), clientSet, "operator-namespace", "ca-secret-name")
		Expect(err).To(BeNil())
		Expect(resultingSecret.Namespace).To(Equal("operator-namespace"))
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
		ca, _ := CreateRootCA("ca-secret-name", "operator-namespace")
		caSecret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
		clientSet := fake.NewSimpleClientset(caSecret)
		pki := pkiEnvironmentTemplate

		It("should correctly generate a pki certificate", func() {
			webhookSecret, err := pki.EnsureCertificate(context.TODO(), clientSet, caSecret)
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
		ca, _ := CreateRootCA("ca-secret-name", "operator-namespace")
		caSecret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
		clientSet := fake.NewSimpleClientset(caSecret)
		pki := pkiEnvironmentTemplate
		webhookSecret, _ := pki.EnsureCertificate(context.TODO(), clientSet, caSecret)

		It("must reuse them", func() {
			currentWebhookSecret, err := pki.EnsureCertificate(context.TODO(), clientSet, caSecret)
			Expect(err).To(BeNil())
			Expect(webhookSecret.Data).To(BeEquivalentTo(currentWebhookSecret.Data))
		})
	})

	When("we have a valid CA secret and expired webhook secret", func() {
		ca, _ := CreateRootCA("ca-secret-name", "operator-namespace")
		caSecret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")

		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-90 * 24 * time.Hour)
		server, _ := ca.createAndSignPairWithValidity("this.server.com", notBefore, notAfter, CertTypeServer, nil)
		serverSecret := server.GenerateCertificateSecret("operator-namespace", "pki-secret-name")

		clientSet := fake.NewSimpleClientset(caSecret, serverSecret)
		pki := pkiEnvironmentTemplate

		It("must renew the secret", func() {
			currentServerSecret, err := pki.EnsureCertificate(context.TODO(), clientSet, caSecret)
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

	It("can dump the secrets to a directory", func() {
		ca, err := CreateRootCA("ca-secret-name", "operator-namespace")
		Expect(err).To(BeNil())
		caSecret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
		clientSet := fake.NewSimpleClientset(caSecret)
		pki := pkiEnvironmentTemplate
		webhookSecret, err := pki.EnsureCertificate(context.TODO(), clientSet, caSecret)
		Expect(err).To(BeNil())

		tempDirName, err := ioutil.TempDir("/tmp", "cert_*")
		Expect(err).To(BeNil())
		defer func() {
			err = os.RemoveAll(tempDirName)
			Expect(err).To(BeNil())
		}()

		err = DumpSecretToDir(webhookSecret, tempDirName, "apiserver")
		Expect(err).To(BeNil())

		Expect(fileutils.FileExists(path.Join(tempDirName, "apiserver.key"))).To(BeTrue())
		Expect(fileutils.FileExists(path.Join(tempDirName, "apiserver.crt"))).To(BeTrue())
	})
})

var _ = Describe("TLS certificates injection", func() {
	pki := pkiEnvironmentTemplate

	// Create a CA and the pki secret
	ca, _ := CreateRootCA("ca-secret-name", "operator-namespace")
	caSecret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
	webhookPair, _ := ca.CreateAndSignPair("pki-service.operator-namespace.svc", CertTypeServer, nil)
	webhookSecret := webhookPair.GenerateCertificateSecret(pki.OperatorNamespace, pki.SecretName)

	It("inject the pki certificate in the mutating pki", func() {
		// Create the mutating pki
		mutatingWebhook := mutatingWebhookTemplate
		clientSet := fake.NewSimpleClientset(caSecret, webhookSecret, &mutatingWebhook)

		err := pki.InjectPublicKeyIntoMutatingWebhook(context.TODO(), clientSet, webhookSecret)
		Expect(err).To(BeNil())

		updatedWebhook, err := clientSet.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(
			context.TODO(), pki.MutatingWebhookConfigurationName, metav1.GetOptions{})
		Expect(err).To(BeNil())

		Expect(updatedWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))
	})

	It("inject the pki certificate in the validating pki", func() {
		// Create the validating pki
		validatingWebhook := validatingWebhookTemplate
		clientSet := fake.NewSimpleClientset(caSecret, webhookSecret, &validatingWebhook)

		err := pki.InjectPublicKeyIntoValidatingWebhook(context.TODO(), clientSet, webhookSecret)
		Expect(err).To(BeNil())

		updatedWebhook, err := clientSet.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(
			context.TODO(), pki.ValidatingWebhookConfigurationName, metav1.GetOptions{})
		Expect(err).To(BeNil())

		Expect(updatedWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))
	})
})

var _ = Describe("Webhook environment creation", func() {
	It("should setup the certificates and the webhooks", func() {
		tempDirName, err := ioutil.TempDir("/tmp", "cert_*")
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
		apiClientSet := fakeApiExtension.NewSimpleClientset(&firstCrd, &secondCrd)

		err = pki.Setup(ctx, clientSet, apiClientSet)
		Expect(err).To(BeNil())

		webhookSecret, err := clientSet.CoreV1().Secrets(
			pki.OperatorNamespace).Get(ctx, pki.SecretName, metav1.GetOptions{})
		Expect(err).To(BeNil())
		Expect(webhookSecret.Namespace).To(Equal(pki.OperatorNamespace))
		Expect(webhookSecret.Name).To(Equal(pki.SecretName))

		updatedMutatingWebhook, err := clientSet.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(
			ctx, pki.MutatingWebhookConfigurationName, metav1.GetOptions{})
		Expect(err).To(BeNil())
		Expect(updatedMutatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))

		updatedValidatingWebhook, err := clientSet.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(
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
