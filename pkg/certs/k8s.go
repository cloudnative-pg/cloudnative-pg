/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package certs

import (
	"fmt"
	"io/ioutil"
	"path"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	log = ctrl.Log.WithName("pki")
)

// WebhookEnvironment represent the environment under which the WebHook server will work
type WebhookEnvironment struct {
	// Where to store the certificates
	CertDir string

	// The name of the secret where the certificates will be stored
	SecretName string

	// The name of the service where the webhook server will be reachable
	ServiceName string

	// The name of the namespace where the operator is set up
	OperatorNamespace string

	// The name of the mutating webhook configuration in k8s, used to
	// inject the caBundle
	MutatingWebhookConfigurationName string

	// The name of the validating webhook configuration in k8s, used
	// to inject the caBundle
	ValidatingWebhookConfigurationName string
}

// EnsureRootCACertificate ensure that in the cluster there is a root CA Certificate
func EnsureRootCACertificate(client *kubernetes.Clientset, namespace string, name string) (*v1.Secret, error) {
	// Checking if the root CA already exist
	secret, err := client.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
	if err == nil {
		return secret, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	// Let's create the CA
	pair, err := CreateCA()
	if err != nil {
		return nil, err
	}

	secret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"ca.key": pair.Private,
			"ca.crt": pair.Certificate,
		},
		Type: v1.SecretTypeOpaque,
	}

	createdSecret, err := client.CoreV1().Secrets(namespace).Create(secret)
	if err != nil {
		return nil, err
	}
	return createdSecret, nil
}

// Setup will setup the PKI infrastructure that is needed for the operator
// to correctly work, and copy the certificates which are required for the webhook
// server to run in the right folder
func (webhook WebhookEnvironment) Setup(client *kubernetes.Clientset, caSecret *v1.Secret) error {
	webhookSecret, err := webhook.EnsureCertificate(client, caSecret)
	if err != nil {
		return err
	}

	err = DumpSecretToDir(webhookSecret, webhook.CertDir)
	if err != nil {
		return err
	}

	err = InjectPublicKeyIntoMutatingWebhook(
		client,
		webhook.MutatingWebhookConfigurationName,
		webhookSecret)
	if err != nil && apierrors.IsNotFound(err) {
		log.Info("mutating webhook configuration not found, cannot inject public key",
			"name", webhook.MutatingWebhookConfigurationName)
	} else if err != nil {
		return err
	}

	err = InjectPublicKeyIntoValidatingWebhook(
		client,
		webhook.ValidatingWebhookConfigurationName,
		webhookSecret)
	if err != nil && apierrors.IsNotFound(err) {
		log.Info("validating webhook configuration not found, cannot inject public key",
			"name", webhook.ValidatingWebhookConfigurationName)
	} else if err != nil {
		return err
	}

	return nil
}

// EnsureCertificate will ensure that a webhook certificate exists and is usable
func (webhook WebhookEnvironment) EnsureCertificate(
	client *kubernetes.Clientset, caSecret *v1.Secret) (*v1.Secret, error) {
	// Checking if the secret already exist
	secret, err := client.CoreV1().Secrets(
		webhook.OperatorNamespace).Get(webhook.SecretName, metav1.GetOptions{})
	if err == nil {
		return secret, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	// Let's generate the webhook certificate
	caPair, err := ParseCASecret(caSecret)
	if err != nil {
		return nil, err
	}

	webhookHostname := fmt.Sprintf(
		"%v.%v.svc",
		webhook.ServiceName,
		webhook.OperatorNamespace)
	webhookPair, err := caPair.CreateAndSignPair(webhookHostname)
	if err != nil {
		return nil, err
	}

	secret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhook.SecretName,
			Namespace: webhook.OperatorNamespace,
		},
		Data: map[string][]byte{
			v1.TLSPrivateKeyKey: webhookPair.Private,
			v1.TLSCertKey:       webhookPair.Certificate,
		},
		Type: v1.SecretTypeTLS,
	}

	createdSecret, err := client.CoreV1().Secrets(
		webhook.OperatorNamespace).Create(secret)
	if err != nil {
		return nil, err
	}
	return createdSecret, nil
}

// ParseCASecret parse a CA secret to a key pair
func ParseCASecret(secret *v1.Secret) (*KeyPair, error) {
	privateKey, ok := secret.Data["ca.key"]
	if !ok {
		return nil, fmt.Errorf("missing ca.key secret data")
	}

	publicKey, ok := secret.Data["ca.crt"]
	if !ok {
		return nil, fmt.Errorf("missing ca.crt secret data")
	}

	return &KeyPair{
		Private:     privateKey,
		Certificate: publicKey,
	}, nil
}

// DumpSecretToDir dumps the contents of a secret inside a directory, and this
// is useful for the webhook server to correctly run
func DumpSecretToDir(secret *v1.Secret, certDir string) error {
	for name, content := range secret.Data {
		fileName := path.Join(certDir, name)
		if err := ioutil.WriteFile(fileName, content, 0600); err != nil {
			return err
		}
	}

	return nil
}

// InjectPublicKeyIntoMutatingWebhook inject the TLS public key into the admitted
// ones for a certain mutating webhook configuration
func InjectPublicKeyIntoMutatingWebhook(client *kubernetes.Clientset, name string, tlsSecret *v1.Secret) error {
	config, err := client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for idx := range config.Webhooks {
		config.Webhooks[idx].ClientConfig.CABundle = tlsSecret.Data["tls.crt"]
	}

	_, err = client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Update(config)
	return err
}

// InjectPublicKeyIntoValidatingWebhook inject the TLS public key into the admitted
// ones for a certain validating webhook configuration
func InjectPublicKeyIntoValidatingWebhook(client *kubernetes.Clientset, name string, tlsSecret *v1.Secret) error {
	config, err := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for idx := range config.Webhooks {
		config.Webhooks[idx].ClientConfig.CABundle = tlsSecret.Data["tls.crt"]
	}

	_, err = client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Update(config)
	return err
}
