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
	"errors"
	"fmt"
	"path"
	"reflect"
	"time"

	"github.com/robfig/cron"
	v1 "k8s.io/api/core/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

var (
	pkiLog = log.WithName("pki")

	// errSecretsMountNotRefreshed is the error returned when the kubelet has not yet updated the mounted secret files
	// to the latest version
	errSecretsMountNotRefreshed = errors.New("secrets mount still not refreshed")

	mountedSecretCheckBackoff = wait.Backoff{
		Duration: 10 * time.Millisecond,
		Jitter:   0.1,
		Factor:   2,
		Steps:    10,
	}
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

	// The labelSelector to be used to get the operators deployment,
	// e.g. "app.kubernetes.io/name=cloudnative-pg"
	OperatorDeploymentLabelSelector string
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

	expiring, _, err := pair.IsExpiring()
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

// Setup ensures that we have the required PKI infrastructure to make the operator and the clusters working
func (pki *PublicKeyInfrastructure) Setup(
	ctx context.Context,
	clientSet *kubernetes.Clientset,
	apiClientSet *apiextensionsclientset.Clientset,
) error {
	err := retry.OnError(retry.DefaultRetry, func(err error) bool {
		return apierrors.IsNotFound(err) || apierrors.IsAlreadyExists(err) || isSecretsMountNotRefreshedError(err)
	}, func() error {
		return pki.ensureCertificatesAreUpToDate(ctx, clientSet, apiClientSet)
	})
	if err != nil {
		return err
	}

	err = pki.schedulePeriodicMaintenance(ctx, clientSet, apiClientSet)
	if err != nil {
		return err
	}

	return nil
}

// ensureRootCACertificate ensure that in the cluster there is a root CA Certificate
func (pki *PublicKeyInfrastructure) ensureRootCACertificate(
	ctx context.Context,
	client kubernetes.Interface,
) (*v1.Secret, error) {
	// Checking if the root CA already exist
	secret, err := client.CoreV1().Secrets(pki.OperatorNamespace).Get(ctx, pki.CaSecretName, metav1.GetOptions{})
	if err == nil {
		// Verify the temporal validity of this CA and renew the secret if needed
		secret, err = renewCACertificate(ctx, client, secret)
		if err != nil {
			return nil, err
		}

		return secret, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	// Let's create the CA
	pair, err := CreateRootCA(pki.CaSecretName, pki.OperatorNamespace)
	if err != nil {
		return nil, err
	}

	secret = pair.GenerateCASecret(pki.OperatorNamespace, pki.CaSecretName)
	err = utils.SetAsOwnedByOperatorDeployment(ctx, client, &secret.ObjectMeta, pki.OperatorDeploymentLabelSelector)
	if err != nil {
		return nil, err
	}

	createdSecret, err := client.CoreV1().Secrets(pki.OperatorNamespace).Create(ctx, secret, metav1.CreateOptions{})
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

	expiring, _, err := pair.IsExpiring()
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

// ensureCertificatesAreUpToDate will setup the PKI infrastructure that is needed for the operator
// to correctly work and makes sure that the mounted certificates are the latest.
func (pki PublicKeyInfrastructure) ensureCertificatesAreUpToDate(
	ctx context.Context,
	client kubernetes.Interface,
	apiClient apiextensionsclientset.Interface,
) error {
	caSecret, err := pki.ensureRootCACertificate(
		ctx,
		client,
	)
	if err != nil {
		return err
	}

	webhookSecret, err := pki.setupWebhooksCertificate(ctx, client, apiClient, caSecret)
	if err != nil {
		return err
	}

	// When we create/update the secret it will take some seconds for the kubelet to create/update the mounted files.
	// This retry logic ensures that:
	// - on secret creation we wait for the certificates to be created inside the pod.
	//   This avoids pods restart caused by the missing certificate error
	// - on secret update we wait for the files to be updated to the latest version.
	return retry.OnError(mountedSecretCheckBackoff, isSecretsMountNotRefreshedError, func() error {
		return ensureMountedSecretsAreInSync(webhookSecret, pki.CertDir)
	})
}

func (pki PublicKeyInfrastructure) setupWebhooksCertificate(
	ctx context.Context,
	client kubernetes.Interface,
	apiClient apiextensionsclientset.Interface,
	caSecret *v1.Secret,
) (*v1.Secret, error) {
	if err := fileutils.EnsureDirectoryExist(pki.CertDir); err != nil {
		return nil, err
	}

	webhookSecret, err := pki.ensureCertificate(ctx, client, caSecret)
	if err != nil {
		return nil, err
	}

	if err := pki.injectPublicKeyIntoMutatingWebhook(
		ctx,
		client,
		webhookSecret); err != nil {
		return nil, err
	}

	if err := pki.injectPublicKeyIntoValidatingWebhook(
		ctx,
		client,
		webhookSecret); err != nil {
		return nil, err
	}

	for _, name := range pki.CustomResourceDefinitionsName {
		if err := pki.injectPublicKeyIntoCRD(ctx, apiClient, name, webhookSecret); err != nil {
			return nil, err
		}
	}

	return webhookSecret, nil
}

// schedulePeriodicMaintenance schedule a background periodic certificate maintenance,
// to automatically renew TLS certificates
func (pki PublicKeyInfrastructure) schedulePeriodicMaintenance(
	ctx context.Context,
	client kubernetes.Interface,
	apiClient apiextensionsclientset.Interface,
) error {
	maintenance := func() {
		pkiLog.Info("Periodic TLS certificates maintenance")
		err := pki.ensureCertificatesAreUpToDate(ctx, client, apiClient)
		if err != nil {
			pkiLog.Error(err, "TLS maintenance failed")
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

// ensureCertificate will ensure that a webhook certificate exists and is usable
func (pki PublicKeyInfrastructure) ensureCertificate(
	ctx context.Context, client kubernetes.Interface, caSecret *v1.Secret,
) (*v1.Secret, error) {
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
	webhookPair, err := caPair.CreateAndSignPair(webhookHostname, CertTypeServer, nil)
	if err != nil {
		return nil, err
	}

	secret = webhookPair.GenerateCertificateSecret(pki.OperatorNamespace, pki.SecretName)
	err = utils.SetAsOwnedByOperatorDeployment(ctx, client, &secret.ObjectMeta, pki.OperatorDeploymentLabelSelector)
	if err != nil {
		return nil, err
	}

	createdSecret, err := client.CoreV1().Secrets(pki.OperatorNamespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return createdSecret, nil
}

// renewServerCertificate renews a server certificate if needed
// Returns the renewed secret or the original one if unchanged
func renewServerCertificate(
	ctx context.Context, client kubernetes.Interface, caSecret v1.Secret, secret *v1.Secret,
) (*v1.Secret, error) {
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

// ensureMountedSecretsAreInSync returns errSecretsMountNotRefreshed if secrets are not yet refreshed by the kubelet
// or any other error encountered while reading the file
func ensureMountedSecretsAreInSync(secret *v1.Secret, certDir string) error {
	for name, content := range secret.Data {
		fileName := path.Join(certDir, name)
		mountedVersion, err := fileutils.ReadFile(fileName)
		if err != nil {
			return err
		}
		if string(mountedVersion) != string(content) {
			return errSecretsMountNotRefreshed
		}
	}
	return nil
}

// injectPublicKeyIntoMutatingWebhook inject the TLS public key into the admitted
// ones for a certain mutating webhook configuration
func (pki PublicKeyInfrastructure) injectPublicKeyIntoMutatingWebhook(
	ctx context.Context, client kubernetes.Interface, tlsSecret *v1.Secret,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		config, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(
			ctx, pki.MutatingWebhookConfigurationName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(config.Webhooks) == 0 {
			return nil
		}

		oldConfig := config.DeepCopy()

		for idx := range config.Webhooks {
			config.Webhooks[idx].ClientConfig.CABundle = tlsSecret.Data["tls.crt"]
		}

		if reflect.DeepEqual(oldConfig.Webhooks, config.Webhooks) {
			return nil
		}

		_, err = client.AdmissionregistrationV1().
			MutatingWebhookConfigurations().
			Update(ctx, config, metav1.UpdateOptions{})
		return err
	})
}

// injectPublicKeyIntoValidatingWebhook inject the TLS public key into the admitted
// ones for a certain validating webhook configuration
func (pki PublicKeyInfrastructure) injectPublicKeyIntoValidatingWebhook(
	ctx context.Context, client kubernetes.Interface, tlsSecret *v1.Secret,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		config, err := client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
			ctx, pki.ValidatingWebhookConfigurationName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(config.Webhooks) == 0 {
			return nil
		}

		oldConfig := config.DeepCopy()

		for idx := range config.Webhooks {
			config.Webhooks[idx].ClientConfig.CABundle = tlsSecret.Data["tls.crt"]
		}

		if reflect.DeepEqual(oldConfig.Webhooks, config.Webhooks) {
			return nil
		}

		_, err = client.AdmissionregistrationV1().
			ValidatingWebhookConfigurations().
			Update(ctx, config, metav1.UpdateOptions{})
		return err
	})
}

// injectPublicKeyIntoCRD inject the TLS public key into the admitted
// ones from a certain conversion webhook inside a CRD
func (pki PublicKeyInfrastructure) injectPublicKeyIntoCRD(
	ctx context.Context,
	apiClient apiextensionsclientset.Interface,
	name string,
	tlsSecret *v1.Secret,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		crd, err := apiClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if crd.Spec.Conversion == nil ||
			crd.Spec.Conversion.Webhook == nil ||
			crd.Spec.Conversion.Webhook.ClientConfig == nil ||
			reflect.DeepEqual(crd.Spec.Conversion.Webhook.ClientConfig.CABundle, tlsSecret.Data["tls.crt"]) {
			return nil
		}

		crd.Spec.Conversion.Webhook.ClientConfig.CABundle = tlsSecret.Data["tls.crt"]

		_, err = apiClient.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, crd, metav1.UpdateOptions{})
		return err
	})
}

func isSecretsMountNotRefreshedError(err error) bool {
	return err == errSecretsMountNotRefreshed
}
