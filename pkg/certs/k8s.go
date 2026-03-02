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
	"errors"
	"fmt"
	"path"
	"reflect"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/robfig/cron"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	pkiLog = log.WithName("pki")

	// errSecretsMountNotRefreshed is the error returned when the kubelet has not yet updated the mounted secret files
	// to the latest version
	errSecretsMountNotRefreshed = errors.New("secrets mount still not refreshed")

	mountedSecretCheckBackoff = wait.Backoff{
		Duration: 60 * time.Millisecond,
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

	// The labelSelector to be used to get the operators deployment,
	// e.g. "app.kubernetes.io/name=cloudnative-pg"
	OperatorDeploymentLabelSelector string
}

// RenewLeafCertificate renew a secret containing a server
// certificate given the secret containing the CA that will sign it.
// Returns true if the certificate has been renewed
func RenewLeafCertificate(caSecret *corev1.Secret, secret *corev1.Secret, altDNSNames []string) (bool, error) {
	// Verify the temporal validity of this CA
	pair, err := ParseServerSecret(secret)
	if err != nil {
		return false, err
	}

	expiring, _, err := pair.IsExpiring()
	if err != nil {
		return false, err
	}

	altDNSNamesMatch, err := pair.DoAltDNSNamesMatch(altDNSNames)
	if err != nil {
		return false, err
	}

	if !expiring && altDNSNamesMatch {
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

	err = pair.RenewCertificate(caPrivateKey, caCertificate, altDNSNames)
	if err != nil {
		return false, err
	}

	secret.Data["tls.crt"] = pair.Certificate

	return true, nil
}

// Setup ensures that we have the required PKI infrastructure to make the operator and the clusters working
func (pki *PublicKeyInfrastructure) Setup(
	ctx context.Context,
	kubeClient client.Client,
) error {
	err := retry.OnError(retry.DefaultRetry, func(err error) bool {
		return apierrors.IsNotFound(err) || apierrors.IsAlreadyExists(err) || isSecretsMountNotRefreshedError(err)
	}, func() error {
		return pki.ensureCertificatesAreUpToDate(ctx, kubeClient)
	})
	if err != nil {
		return err
	}

	err = pki.schedulePeriodicMaintenance(ctx, kubeClient)
	if err != nil {
		return err
	}

	return nil
}

// ensureRootCACertificate ensure that in the cluster there is a root CA Certificate
func (pki *PublicKeyInfrastructure) ensureRootCACertificate(
	ctx context.Context,
	kubeClient client.Client,
) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	// Checking if the root CA already exist
	err := kubeClient.Get(ctx, types.NamespacedName{Namespace: pki.OperatorNamespace, Name: pki.CaSecretName}, secret)
	if err == nil {
		// Verify the temporal validity of this CA and renew the secret if needed
		secret, err = renewCACertificate(ctx, kubeClient, secret)
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
	err = SetAsOwnedByOperatorDeployment(ctx, kubeClient, &secret.ObjectMeta, pki.OperatorDeploymentLabelSelector)
	if err != nil {
		return nil, err
	}

	err = kubeClient.Create(ctx, secret)
	if err != nil {
		return nil, err
	}
	return secret, nil
}

// renewCACertificate renews a CA certificate if needed, returning the updated
// secret if the secret has been renewed
func renewCACertificate(ctx context.Context, kubeClient client.Client, secret *corev1.Secret) (*corev1.Secret, error) {
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

	err = pair.RenewCertificate(privateKey, nil, nil)
	if err != nil {
		return nil, err
	}

	secret.Data[CACertKey] = pair.Certificate
	err = kubeClient.Update(ctx, secret)
	if err != nil {
		return nil, err
	}

	return secret, nil
}

// ensureCertificatesAreUpToDate will setup the PKI infrastructure that is needed for the operator
// to correctly work and makes sure that the mounted certificates are the latest.
func (pki PublicKeyInfrastructure) ensureCertificatesAreUpToDate(
	ctx context.Context,
	kubeClient client.Client,
) error {
	caSecret, err := pki.ensureRootCACertificate(
		ctx,
		kubeClient,
	)
	if err != nil {
		return err
	}

	webhookSecret, err := pki.setupWebhooksCertificate(ctx, kubeClient, caSecret)
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
	kubeClient client.Client,
	caSecret *corev1.Secret,
) (*corev1.Secret, error) {
	if err := fileutils.EnsureDirectoryExists(pki.CertDir); err != nil {
		return nil, err
	}

	webhookSecret, err := pki.ensureCertificate(ctx, kubeClient, caSecret)
	if err != nil {
		return nil, err
	}

	if err := pki.injectPublicKeyIntoMutatingWebhook(
		ctx,
		kubeClient,
		webhookSecret); err != nil {
		return nil, err
	}

	if err := pki.injectPublicKeyIntoValidatingWebhook(
		ctx,
		kubeClient,
		webhookSecret); err != nil {
		return nil, err
	}

	return webhookSecret, nil
}

// schedulePeriodicMaintenance schedule a background periodic certificate maintenance,
// to automatically renew TLS certificates
func (pki PublicKeyInfrastructure) schedulePeriodicMaintenance(
	ctx context.Context,
	kubeClient client.Client,
) error {
	maintenance := func() {
		pkiLog.Info("Periodic TLS certificates maintenance")
		err := pki.ensureCertificatesAreUpToDate(ctx, kubeClient)
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
	ctx context.Context, kubeClient client.Client, caSecret *corev1.Secret,
) (*corev1.Secret, error) {
	webhookHostname := fmt.Sprintf(
		"%v.%v.svc",
		pki.ServiceName,
		pki.OperatorNamespace)
	secret := &corev1.Secret{}
	// Checking if the secret already exist
	if err := kubeClient.Get(
		ctx,
		types.NamespacedName{Namespace: pki.OperatorNamespace, Name: pki.SecretName},
		secret,
	); err == nil {
		// Verify the temporal validity of this certificate and
		// renew it if needed
		return renewServerCertificate(ctx, kubeClient, *caSecret, secret, []string{webhookHostname})
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	// Let's generate the pki certificate
	caPair, err := ParseCASecret(caSecret)
	if err != nil {
		return nil, err
	}

	webhookPair, err := caPair.CreateAndSignPair(webhookHostname, CertTypeServer, nil)
	if err != nil {
		return nil, err
	}

	// Use GenerateWebhookCertificateSecret to include the CA certificate in the secret
	// This is required for webhook validation as the caBundle needs to verify the certificate chain
	secret = webhookPair.GenerateWebhookCertificateSecret(pki.OperatorNamespace, pki.SecretName, caPair.Certificate)
	if err := SetAsOwnedByOperatorDeployment(
		ctx,
		kubeClient,
		&secret.ObjectMeta,
		pki.OperatorDeploymentLabelSelector,
	); err != nil {
		return nil, err
	}

	if err := kubeClient.Create(ctx, secret); err != nil {
		return nil, err
	}

	return secret, nil
}

// renewServerCertificate renews a server certificate if needed
// Returns the renewed secret or the original one if unchanged
func renewServerCertificate(
	ctx context.Context, kubeClient client.Client, caSecret corev1.Secret, secret *corev1.Secret, altDNSNames []string,
) (*corev1.Secret, error) {
	origSecret := secret.DeepCopy()
	hasBeenRenewed, err := RenewLeafCertificate(&caSecret, secret, altDNSNames)
	if err != nil {
		return nil, err
	}

	if hasBeenRenewed {
		// If this is a webhook secret, preserve the CA during renewal
		// The CA is needed for webhook validation (caBundle)
		if caCert, ok := caSecret.Data[CACertKey]; ok && secret.Type == corev1.SecretTypeTLS {
			secret.Data[CACertKey] = caCert
		}

		if err := kubeClient.Patch(ctx, secret, client.MergeFrom(origSecret)); err != nil {
			return nil, err
		}
		return secret, nil
	}

	return secret, nil
}

// ensureMountedSecretsAreInSync returns errSecretsMountNotRefreshed if secrets are not yet refreshed by the kubelet
// or any other error encountered while reading the file
func ensureMountedSecretsAreInSync(secret *corev1.Secret, certDir string) error {
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
	ctx context.Context, kubeClient client.Client, tlsSecret *corev1.Secret,
) error {
	config := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Name: pki.MutatingWebhookConfigurationName}, config); err != nil {
		return err
	}
	if len(config.Webhooks) == 0 {
		return nil
	}

	oldConfig := config.DeepCopy()

	// Use the CA certificate from the secret for the CABundle
	// If ca.crt is not present (legacy secrets), fall back to tls.crt
	caBundle := tlsSecret.Data[CACertKey]
	if len(caBundle) == 0 {
		caBundle = tlsSecret.Data[TLSCertKey]
	}

	for idx := range config.Webhooks {
		config.Webhooks[idx].ClientConfig.CABundle = caBundle
	}

	if reflect.DeepEqual(oldConfig.Webhooks, config.Webhooks) {
		return nil
	}

	return kubeClient.Patch(ctx, config, client.MergeFrom(oldConfig))
}

// injectPublicKeyIntoValidatingWebhook inject the TLS public key into the admitted
// ones for a certain validating webhook configuration
func (pki PublicKeyInfrastructure) injectPublicKeyIntoValidatingWebhook(
	ctx context.Context, kubeClient client.Client, tlsSecret *corev1.Secret,
) error {
	config := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Name: pki.ValidatingWebhookConfigurationName}, config); err != nil {
		return err
	}

	if len(config.Webhooks) == 0 {
		return nil
	}

	oldConfig := config.DeepCopy()

	// Use the CA certificate from the secret for the CABundle
	// If ca.crt is not present (legacy secrets), fall back to tls.crt
	caBundle := tlsSecret.Data[CACertKey]
	if len(caBundle) == 0 {
		caBundle = tlsSecret.Data[TLSCertKey]
	}

	for idx := range config.Webhooks {
		config.Webhooks[idx].ClientConfig.CABundle = caBundle
	}

	if reflect.DeepEqual(oldConfig.Webhooks, config.Webhooks) {
		return nil
	}

	return kubeClient.Patch(ctx, config, client.MergeFrom(oldConfig))
}

func isSecretsMountNotRefreshedError(err error) bool {
	return err == errSecretsMountNotRefreshed
}
