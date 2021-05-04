/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package certs

import (
	"context"
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"

	"github.com/robfig/cron"
	v1 "k8s.io/api/core/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
)

var (
	log = ctrl.Log.WithName("pki")
)

// PublicKeyInfrastructure represent the PKI under which the operator and the WebHook server
// will work
type PublicKeyInfrastructure struct {
	// Where to store the certificates
	CertDir string

	// The name of the secret where the CA certificate will be stored
	CaSecretName string

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

	// The name of every CRD that has a reference to a conversion webhook
	// on which we need to inject our public key
	CustomResourceDefinitionsName []string
}

// EnsureRootCACertificate ensure that in the cluster there is a root CA Certificate
func EnsureRootCACertificate(
	ctx context.Context, client kubernetes.Interface, namespace string, name string) (*v1.Secret, error) {
	// Checking if the root CA already exist
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		// Verify the temporal validity of this CA and renew the secret if needed
		_, err := renewCACertificate(ctx, client, secret)
		if err != nil {
			return nil, err
		}

		return secret, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	// Let's create the CA
	pair, err := CreateRootCA(name, namespace)
	if err != nil {
		return nil, err
	}

	secret = pair.GenerateCASecret(namespace, name)
	createdSecret, err := client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return createdSecret, nil
}

// renewCACertificate renews a CA certificate if needed, returning the updated
// secret if the secret has been renewed
func renewCACertificate(ctx context.Context, client kubernetes.Interface, secret *v1.Secret) (*v1.Secret, error) {
	// Verify the temporal validity of this CA
	pair, err := ParseCASecret(secret)
	if err != nil {
		return nil, err
	}

	expiring, err := pair.IsExpiring()
	if err != nil {
		return nil, err
	}
	if !expiring {
		return secret, nil
	}

	privateKey, err := pair.ParseECPrivateKey()
	if err != nil {
		return nil, err
	}

	err = pair.RenewCertificate(privateKey, nil)
	if err != nil {
		return nil, err
	}

	secret.Data[CACertKey] = pair.Certificate
	updatedSecret, err := client.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}

	return updatedSecret, nil
}

// Setup will setup the PKI infrastructure that is needed for the operator
// to correctly work, and copy the certificates which are required for the webhook
// server to run in the right folder
func (pki PublicKeyInfrastructure) Setup(
	ctx context.Context,
	client kubernetes.Interface,
	apiClient apiextensionsclientset.Interface) error {
	caSecret, err := EnsureRootCACertificate(
		ctx,
		client,
		pki.OperatorNamespace,
		pki.CaSecretName)
	if err != nil {
		return err
	}

	if pki.CertDir != "" {
		if err = pki.setupWebhooksCertificate(ctx, client, apiClient, caSecret); err != nil {
			return err
		}
	}

	return nil
}

func (pki PublicKeyInfrastructure) setupWebhooksCertificate(
	ctx context.Context,
	client kubernetes.Interface,
	apiClient apiextensionsclientset.Interface,
	caSecret *v1.Secret,
) error {
	if err := fileutils.EnsureDirectoryExist(pki.CertDir); err != nil {
		return err
	}

	webhookSecret, err := pki.EnsureCertificate(ctx, client, caSecret)
	if err != nil {
		return err
	}

	err = DumpSecretToDir(webhookSecret, pki.CertDir, "apiserver")
	if err != nil {
		return err
	}

	err = pki.InjectPublicKeyIntoMutatingWebhook(
		ctx,
		client,
		webhookSecret)
	if err != nil && apierrors.IsNotFound(err) {
		log.Info("mutating pki configuration not found, cannot inject public key",
			"name", pki.MutatingWebhookConfigurationName)
	} else if err != nil {
		return err
	}

	err = pki.InjectPublicKeyIntoValidatingWebhook(
		ctx,
		client,
		webhookSecret)
	if err != nil && apierrors.IsNotFound(err) {
		log.Info("validating pki configuration not found, cannot inject public key",
			"name", pki.ValidatingWebhookConfigurationName)
	} else if err != nil {
		return err
	}

	for _, name := range pki.CustomResourceDefinitionsName {
		if err = pki.InjectPublicKeyIntoCRD(ctx, apiClient, name, webhookSecret); err != nil {
			return err
		}
	}

	return nil
}

// SchedulePeriodicMaintenance schedule a background periodic certificate maintenance,
// to automatically renew TLS certificates
func (pki PublicKeyInfrastructure) SchedulePeriodicMaintenance(
	ctx context.Context,
	client kubernetes.Interface,
	apiClient apiextensionsclientset.Interface) error {
	maintenance := func() {
		log.Info("Periodic TLS certificates maintenance")
		err := pki.Setup(ctx, client, apiClient)
		if err != nil {
			log.Error(err, "TLS maintenance failed")
		}
	}

	c := cron.New()
	err := c.AddFunc("@every 1h", maintenance)
	c.Start()

	if err != nil {
		return fmt.Errorf("error while scheduling CA maintenance: %w", err)
	}

	return nil
}

// EnsureCertificate will ensure that a webhook certificate exists and is usable
func (pki PublicKeyInfrastructure) EnsureCertificate(
	ctx context.Context, client kubernetes.Interface, caSecret *v1.Secret) (*v1.Secret, error) {
	// Checking if the secret already exist
	secret, err := client.CoreV1().Secrets(
		pki.OperatorNamespace).Get(ctx, pki.SecretName, metav1.GetOptions{})
	if err == nil {
		// Verify the temporal validity of this certificate and
		// renew it if needed
		return renewServerCertificate(ctx, client, *caSecret, secret)
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	// Let's generate the pki certificate
	caPair, err := ParseCASecret(caSecret)
	if err != nil {
		return nil, err
	}

	webhookHostname := fmt.Sprintf(
		"%v.%v.svc",
		pki.ServiceName,
		pki.OperatorNamespace)
	webhookPair, err := caPair.CreateAndSignPair(webhookHostname, CertTypeServer)
	if err != nil {
		return nil, err
	}

	secret = webhookPair.GenerateServerSecret(pki.OperatorNamespace, pki.SecretName)
	createdSecret, err := client.CoreV1().Secrets(
		pki.OperatorNamespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return createdSecret, nil
}

// RenewLeafCertificate renew a secret containing a server
// certificate given the secret containing the CA that will sign it.
// Returns true if the certificate has been renewed
func RenewLeafCertificate(caSecret *v1.Secret, secret *v1.Secret) (bool, error) {
	// Verify the temporal validity of this CA
	pair, err := ParseServerSecret(secret)
	if err != nil {
		return false, err
	}

	expiring, err := pair.IsExpiring()
	if err != nil {
		return false, err
	}
	if !expiring {
		return false, nil
	}

	// Parse the CA secret to get the private key
	caPair, err := ParseCASecret(caSecret)
	if err != nil {
		return false, err
	}

	caPrivateKey, err := caPair.ParseECPrivateKey()
	if err != nil {
		return false, err
	}

	caCertificate, err := caPair.ParseCertificate()
	if err != nil {
		return false, err
	}

	err = pair.RenewCertificate(caPrivateKey, caCertificate)
	if err != nil {
		return false, err
	}

	secret.Data["tls.crt"] = pair.Certificate

	return true, nil
}

// renewServerCertificate renews a server certificate if needed
// Returns the renewed secret or the original one if unchanged
func renewServerCertificate(
	ctx context.Context, client kubernetes.Interface, caSecret v1.Secret, secret *v1.Secret) (*v1.Secret, error) {
	hasBeenRenewed, err := RenewLeafCertificate(&caSecret, secret)
	if err != nil {
		return nil, err
	}

	if hasBeenRenewed {
		updatedSecret, err := client.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{})
		if err != nil {
			return nil, err
		}
		return updatedSecret, nil
	}

	return secret, nil
}

// DumpSecretToDir dumps the contents of a secret inside a directory creating
// a file to every key/value couple in the required Secret.
//
// The actual files written in the directory will be named accordingly to the
// basename, i.e., given a secret with the following data:
//
//     data:
//       test.crt: <test.crt.contents>
//       test.key: <test.key.contents>
//
// The following files will be written:
//
//     <certdir>/<basename>.crt
//     <certdir>/<basename>.key
func DumpSecretToDir(secret *v1.Secret, certDir string, basename string) error {
	resourceFileName := path.Join(certDir, "resource")

	oldVersionExist, err := fileutils.FileExists(resourceFileName)
	if err != nil {
		return err
	}
	if oldVersionExist {
		oldVersion, err := fileutils.ReadFile(resourceFileName)
		if err != nil {
			return err
		}

		if oldVersion == secret.ResourceVersion {
			// No need to rewrite certificates, the content
			// is just the same
			return nil
		}
	}

	for name, content := range secret.Data {
		extension := filepath.Ext(name)
		fileName := path.Join(certDir, basename+extension)
		if err := ioutil.WriteFile(fileName, content, 0600); err != nil {
			return err
		}
	}

	err = ioutil.WriteFile(resourceFileName, []byte(secret.ResourceVersion), 0600)
	if err != nil {
		return err
	}

	return nil
}

// InjectPublicKeyIntoMutatingWebhook inject the TLS public key into the admitted
// ones for a certain mutating webhook configuration
func (pki PublicKeyInfrastructure) InjectPublicKeyIntoMutatingWebhook(
	ctx context.Context, client kubernetes.Interface, tlsSecret *v1.Secret) error {
	config, err := client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(
		ctx, pki.MutatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for idx := range config.Webhooks {
		config.Webhooks[idx].ClientConfig.CABundle = tlsSecret.Data["tls.crt"]
	}

	_, err = client.AdmissionregistrationV1beta1().
		MutatingWebhookConfigurations().
		Update(ctx, config, metav1.UpdateOptions{})
	return err
}

// InjectPublicKeyIntoValidatingWebhook inject the TLS public key into the admitted
// ones for a certain validating webhook configuration
func (pki PublicKeyInfrastructure) InjectPublicKeyIntoValidatingWebhook(
	ctx context.Context, client kubernetes.Interface, tlsSecret *v1.Secret) error {
	config, err := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(
		ctx, pki.ValidatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for idx := range config.Webhooks {
		config.Webhooks[idx].ClientConfig.CABundle = tlsSecret.Data["tls.crt"]
	}

	_, err = client.AdmissionregistrationV1beta1().
		ValidatingWebhookConfigurations().
		Update(ctx, config, metav1.UpdateOptions{})
	return err
}

// InjectPublicKeyIntoCRD inject the TLS public key into the admitted
// ones from a certain conversion webhook inside a CRD
func (pki PublicKeyInfrastructure) InjectPublicKeyIntoCRD(
	ctx context.Context,
	apiClient apiextensionsclientset.Interface,
	name string,
	tlsSecret *v1.Secret) error {
	crd, err := apiClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if crd.Spec.Conversion != nil {
		if crd.Spec.Conversion.Webhook != nil {
			if crd.Spec.Conversion.Webhook.ClientConfig != nil {
				crd.Spec.Conversion.Webhook.ClientConfig.CABundle = tlsSecret.Data["tls.crt"]
			}
		}
	}
	_, err = apiClient.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, crd, metav1.UpdateOptions{})
	return err
}
