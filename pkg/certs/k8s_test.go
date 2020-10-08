/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package certs

import (
	"io/ioutil"
	"os"
	"path"
	"time"

	"k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/fileutils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	webhookEnvironmentTemplate = WebhookEnvironment{
		CertDir:                            "/tmp",
		CaSecretName:                       "ca-secret",
		SecretName:                         "webhook-secret-name",
		ServiceName:                        "webhook-service",
		OperatorNamespace:                  "operator-namespace",
		MutatingWebhookConfigurationName:   "mutating-webhook",
		ValidatingWebhookConfigurationName: "validating-webhook",
	}

	mutatingWebhookTemplate = v1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookEnvironmentTemplate.MutatingWebhookConfigurationName,
		},
		Webhooks: []v1beta1.MutatingWebhook{
			{
				ClientConfig: v1beta1.WebhookClientConfig{},
			},
		},
	}

	validatingWebhookTemplate = v1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookEnvironmentTemplate.ValidatingWebhookConfigurationName,
		},
		Webhooks: []v1beta1.ValidatingWebhook{
			{
				ClientConfig: v1beta1.WebhookClientConfig{},
			},
		},
	}
)

var _ = Describe("Root CA secret generation", func() {
	It("must generate a new CA secret when it doesn't already exist", func() {
		clientSet := fake.NewSimpleClientset()
		secret, err := EnsureRootCACertificate(clientSet, "operator-namespace", "ca-secret-name")
		Expect(err).To(BeNil())

		Expect(secret.Namespace).To(Equal("operator-namespace"))
		Expect(secret.Name).To(Equal("ca-secret-name"))

		_, err = clientSet.CoreV1().Secrets("operator-namespace").Get(
			"ca-secret-name", metav1.GetOptions{})
		Expect(err).To(BeNil())
	})

	It("must adopt the current certificate if it is valid", func() {
		ca, err := CreateCA()
		Expect(err).To(BeNil())

		secret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
		clientSet := fake.NewSimpleClientset(secret)

		resultingSecret, err := EnsureRootCACertificate(clientSet, "operator-namespace", "ca-secret-name")
		Expect(err).To(BeNil())
		Expect(resultingSecret.Namespace).To(Equal("operator-namespace"))
		Expect(resultingSecret.Name).To(Equal("ca-secret-name"))
	})

	It("must renew the CA certificate if it is not valid", func() {
		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-365 * 24 * time.Hour)
		ca, err := createCAWithValidity(notBefore, notAfter)
		Expect(err).To(BeNil())

		secret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
		clientSet := fake.NewSimpleClientset(secret)

		// The secret should have been renewed now
		resultingSecret, err := EnsureRootCACertificate(clientSet, "operator-namespace", "ca-secret-name")
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
		ca, _ := CreateCA()
		caSecret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
		clientSet := fake.NewSimpleClientset(caSecret)
		webhook := webhookEnvironmentTemplate

		It("should correctly generate a webhook certificate", func() {
			webhookSecret, err := webhook.EnsureCertificate(clientSet, caSecret)
			Expect(err).To(BeNil())
			Expect(webhookSecret.Name).To(Equal(webhook.SecretName))
			Expect(webhookSecret.Namespace).To(Equal(webhook.OperatorNamespace))

			pair, err := ParseServerSecret(webhookSecret)
			Expect(err).To(BeNil())

			cert, err := pair.ParseCertificate()
			Expect(err).To(BeNil())

			Expect(cert.NotBefore).To(BeTemporally("<", time.Now()))
			Expect(cert.NotAfter).To(BeTemporally(">", time.Now()))
		})
	})

	When("we have a valid CA and webhook secret", func() {
		ca, _ := CreateCA()
		caSecret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
		clientSet := fake.NewSimpleClientset(caSecret)
		webhook := webhookEnvironmentTemplate
		webhookSecret, _ := webhook.EnsureCertificate(clientSet, caSecret)

		It("must reuse them", func() {
			currentWebhookSecret, err := webhook.EnsureCertificate(clientSet, caSecret)
			Expect(err).To(BeNil())
			Expect(webhookSecret.Data).To(BeEquivalentTo(currentWebhookSecret.Data))
		})
	})

	When("we have a valid CA secret and expired webhook secret", func() {
		ca, _ := CreateCA()
		caSecret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")

		notAfter := time.Now().Add(-10 * time.Hour)
		notBefore := notAfter.Add(-365 * 24 * time.Hour)
		server, _ := ca.createAndSignPairWithValidity("this.server.com", notBefore, notAfter)
		serverSecret := server.GenerateServerSecret("operator-namespace", "webhook-secret-name")

		clientSet := fake.NewSimpleClientset(caSecret, serverSecret)
		webhook := webhookEnvironmentTemplate

		It("must renew the secret", func() {
			currentServerSecret, err := webhook.EnsureCertificate(clientSet, caSecret)
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
		ca, err := CreateCA()
		Expect(err).To(BeNil())
		caSecret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
		clientSet := fake.NewSimpleClientset(caSecret)
		webhook := webhookEnvironmentTemplate
		webhookSecret, err := webhook.EnsureCertificate(clientSet, caSecret)
		Expect(err).To(BeNil())

		tempDirName, err := ioutil.TempDir("/tmp", "cert_*")
		Expect(err).To(BeNil())
		defer func() {
			err = os.RemoveAll(tempDirName)
			Expect(err).To(BeNil())
		}()

		err = DumpSecretToDir(webhookSecret, tempDirName)
		Expect(err).To(BeNil())

		Expect(fileutils.FileExists(path.Join(tempDirName, "tls.key"))).To(BeTrue())
		Expect(fileutils.FileExists(path.Join(tempDirName, "tls.crt"))).To(BeTrue())
	})
})

var _ = Describe("TLS certificates injection", func() {
	webhook := webhookEnvironmentTemplate

	// Create a CA and the webhook secret
	ca, _ := CreateCA()
	caSecret := ca.GenerateCASecret("operator-namespace", "ca-secret-name")
	webhookPair, _ := ca.CreateAndSignPair("webhook-service.operator-namespace.svc")
	webhookSecret := webhookPair.GenerateServerSecret(webhook.OperatorNamespace, webhook.SecretName)

	It("inject the webhook certificate in the mutating webhook", func() {
		// Create the mutating webhook
		mutatingWebhook := mutatingWebhookTemplate
		clientSet := fake.NewSimpleClientset(caSecret, webhookSecret, &mutatingWebhook)

		err := webhook.InjectPublicKeyIntoMutatingWebhook(clientSet, webhookSecret)
		Expect(err).To(BeNil())

		updatedWebhook, err := clientSet.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(
			webhook.MutatingWebhookConfigurationName, metav1.GetOptions{})
		Expect(err).To(BeNil())

		Expect(updatedWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))
	})

	It("inject the webhook certificate in the validating webhook", func() {
		// Create the validating webhook
		validatingWebhook := validatingWebhookTemplate
		clientSet := fake.NewSimpleClientset(caSecret, webhookSecret, &validatingWebhook)

		err := webhook.InjectPublicKeyIntoValidatingWebhook(clientSet, webhookSecret)
		Expect(err).To(BeNil())

		updatedWebhook, err := clientSet.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(
			webhook.ValidatingWebhookConfigurationName, metav1.GetOptions{})
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

		webhook := webhookEnvironmentTemplate
		mutatingWebhook := mutatingWebhookTemplate
		validatingWebhook := validatingWebhookTemplate
		clientSet := fake.NewSimpleClientset(&mutatingWebhook, &validatingWebhook)

		err = webhook.Setup(clientSet)
		Expect(err).To(BeNil())

		webhookSecret, err := clientSet.CoreV1().Secrets(
			webhook.OperatorNamespace).Get(webhook.SecretName, metav1.GetOptions{})
		Expect(err).To(BeNil())
		Expect(webhookSecret.Namespace).To(Equal(webhook.OperatorNamespace))
		Expect(webhookSecret.Name).To(Equal(webhook.SecretName))

		updatedMutatingWebhook, err := clientSet.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(
			webhook.MutatingWebhookConfigurationName, metav1.GetOptions{})
		Expect(err).To(BeNil())
		Expect(updatedMutatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))

		updatedValidatingWebhook, err := clientSet.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(
			webhook.ValidatingWebhookConfigurationName, metav1.GetOptions{})
		Expect(err).To(BeNil())
		Expect(updatedValidatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(webhookSecret.Data["tls.crt"]))
	})
})
