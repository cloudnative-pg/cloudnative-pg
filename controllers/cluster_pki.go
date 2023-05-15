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

package controllers

import (
	"context"
	"crypto/x509"
	"fmt"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// setupPostgresPKI create all the PKI infrastructure that PostgreSQL need to work
// if using ssl=on
func (r *ClusterReconciler) setupPostgresPKI(ctx context.Context, cluster *apiv1.Cluster) error {
	// This is the CA of cluster
	serverCaSecret, err := r.ensureServerCASecret(ctx, cluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("missing specified server CA secret %s: %w", cluster.GetServerCASecretName(), err)
		}
		return fmt.Errorf("generating server CA certificate: %w", err)
	}

	// This is the certificate for the server
	serverCertificateName := client.ObjectKey{Namespace: cluster.GetNamespace(), Name: cluster.GetServerTLSSecretName()}
	opts := x509.VerifyOptions{KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	err = r.ensureServerLeafCertificate(
		ctx,
		cluster,
		serverCertificateName,
		cluster.GetServiceReadWriteName(),
		serverCaSecret,
		certs.CertTypeServer,
		cluster.GetClusterAltDNSNames(),
		&opts)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("missing specified server TLS secret %s: %w",
				cluster.Status.Certificates.ServerTLSSecret, err)
		}
		return fmt.Errorf("generating server TLS certificate: %w", err)
	}

	clientCaSecret, err := r.ensureClientCASecret(ctx, cluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("missing specified client CA secret %s: %w", cluster.GetClientCASecretName(), err)
		}
		return fmt.Errorf("generating client CA certificate: %w", err)
	}

	// Generating postgres client certificate
	replicationSecretName := client.ObjectKey{
		Namespace: cluster.GetNamespace(),
		Name:      cluster.GetReplicationSecretName(),
	}
	err = r.ensureLeafCertificate(
		ctx,
		cluster,
		replicationSecretName,
		apiv1.StreamingReplicationUser,
		clientCaSecret,
		certs.CertTypeClient,
		nil,
		nil)
	if err != nil {
		return fmt.Errorf("generating streaming replication client certificate: %w", err)
	}

	return nil
}

// ensureClientCASecret ensure that the cluster CA really exist and is valid
func (r *ClusterReconciler) ensureClientCASecret(ctx context.Context, cluster *apiv1.Cluster) (*v1.Secret, error) {
	if cluster.Spec.Certificates == nil || cluster.Spec.Certificates.ClientCASecret == "" {
		return r.ensureCASecret(ctx, cluster, cluster.GetClientCASecretName())
	}

	var secret v1.Secret
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.GetNamespace(), Name: cluster.GetClientCASecretName()},
		&secret)
	// If specified and error, bubble up
	if err != nil {
		r.Recorder.Event(cluster, "Warning", "SecretNotFound",
			"Getting secret "+cluster.GetClientCASecretName())
		return nil, err
	}

	err = r.verifyCAValidity(secret, cluster)
	if err != nil {
		return nil, err
	}

	// Validate also ca.key if needed
	if cluster.Spec.Certificates.ReplicationTLSSecret == "" {
		_, err = certs.ParseCASecret(&secret)
		if err != nil {
			r.Recorder.Event(cluster, "Warning", "InvalidCASecret",
				fmt.Sprintf("Parsing client secret %s: %s", secret.Name, err.Error()))
			return nil, err
		}
	}

	// If specified and found, go on
	return &secret, nil
}

// ensureServerCASecret ensure that the cluster CA really exist and is valid
func (r *ClusterReconciler) ensureServerCASecret(ctx context.Context, cluster *apiv1.Cluster) (*v1.Secret, error) {
	// If not specified, use default amd renew/generate
	certificates := cluster.Spec.Certificates
	if certificates == nil || certificates.ServerCASecret == "" {
		return r.ensureCASecret(ctx, cluster, cluster.GetServerCASecretName())
	}

	var secret v1.Secret
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.GetNamespace(), Name: cluster.GetServerCASecretName()},
		&secret)
	// If specified and error, bubble up
	if err != nil {
		r.Recorder.Event(cluster, "Warning", "SecretNotFound",
			"Getting secret "+cluster.GetServerCASecretName())
		return nil, err
	}

	err = r.verifyCAValidity(secret, cluster)
	if err != nil {
		return nil, err
	}

	// validate also ca.key if needed
	if cluster.Spec.Certificates.ServerTLSSecret == "" {
		_, err = certs.ParseCASecret(&secret)
		if err != nil {
			r.Recorder.Event(cluster, "Warning", "InvalidCASecret",
				fmt.Sprintf("Parsing server secret %s: %s", secret.Name, err.Error()))
			return nil, err
		}
	}

	// If specified and found, go on
	return &secret, nil
}

func (r *ClusterReconciler) verifyCAValidity(secret v1.Secret, cluster *apiv1.Cluster) error {
	// Verify validity of the CA and expiration (only ca.crt)
	publicKey, ok := secret.Data[certs.CACertKey]
	if !ok {
		return fmt.Errorf("missing %s secret data", certs.CACertKey)
	}

	caPair := &certs.KeyPair{
		Certificate: publicKey,
	}

	isExpiring, _, err := caPair.IsExpiring()
	if err != nil {
		return err
	} else if isExpiring {
		r.Recorder.Event(cluster, "Warning", "SecretIsExpiring",
			"Checking expiring date of secret "+secret.Name)
		log.Info("CA certificate is expiring or is already expired", "secret", secret.Name)
	}

	return nil
}

func (r *ClusterReconciler) ensureCASecret(ctx context.Context, cluster *apiv1.Cluster,
	secretName string,
) (*v1.Secret, error) {
	var secret v1.Secret
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.GetNamespace(), Name: secretName}, &secret)
	if err == nil {
		// Verify the validity of this CA and renew it if needed
		err = r.renewCASecret(ctx, &secret)
		if err != nil {
			return nil, err
		}

		return &secret, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	caPair, err := certs.CreateRootCA(cluster.Name, cluster.Namespace)
	if err != nil {
		return nil, fmt.Errorf("while creating the CA of the cluster: %w", err)
	}

	derivedCaSecret := caPair.GenerateCASecret(cluster.Namespace, secretName)
	utils.SetAsOwnedBy(&derivedCaSecret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	err = r.Create(ctx, derivedCaSecret)

	return derivedCaSecret, err
}

// renewCASecret check if this CA secret is valid and renew it if needed
func (r *ClusterReconciler) renewCASecret(ctx context.Context, secret *v1.Secret) error {
	pair, err := certs.ParseCASecret(secret)
	if err != nil {
		return err
	}

	expiring, _, err := pair.IsExpiring()
	if err != nil {
		return err
	}
	if !expiring {
		return nil
	}

	privateKey, err := pair.ParseECPrivateKey()
	if err != nil {
		return err
	}

	err = pair.RenewCertificate(privateKey, nil)
	if err != nil {
		return err
	}

	secret.Data[certs.CACertKey] = pair.Certificate
	return r.Update(ctx, secret)
}

// ensureServerLeafCertificate checks if we have a certificate for PostgreSQL and generate/renew it
func (r *ClusterReconciler) ensureServerLeafCertificate(
	ctx context.Context,
	cluster *apiv1.Cluster,
	secretName client.ObjectKey,
	commonName string,
	caSecret *v1.Secret,
	usage certs.CertType,
	altDNSNames []string,
	opts *x509.VerifyOptions,
) error {
	// If not specified generate/renew
	if cluster.Spec.Certificates == nil || cluster.Spec.Certificates.ServerTLSSecret == "" {
		return r.ensureLeafCertificate(ctx, cluster, secretName, commonName, caSecret, usage, altDNSNames, nil)
	}

	var serverSecret v1.Secret
	err := r.Get(ctx, secretName, &serverSecret)
	if err != nil {
		return err
	}

	return validateLeafCertificate(caSecret, &serverSecret, opts)
}

func validateLeafCertificate(caSecret *v1.Secret, serverSecret *v1.Secret, opts *x509.VerifyOptions) error {
	publicKey, ok := caSecret.Data[certs.CACertKey]
	if !ok {
		return fmt.Errorf("missing %s secret data", certs.CACertKey)
	}

	caPair := &certs.KeyPair{Certificate: publicKey}

	serverPair, err := certs.ParseServerSecret(serverSecret)
	if err != nil {
		return err
	}

	return serverPair.IsValid(caPair, opts)
}

// ensureLeafCertificate check if we have a certificate for PostgreSQL and generate/renew it
func (r *ClusterReconciler) ensureLeafCertificate(
	ctx context.Context,
	cluster *apiv1.Cluster,
	secretName client.ObjectKey,
	commonName string,
	caSecret *v1.Secret,
	usage certs.CertType,
	altDNSNames []string,
	additionalLabels map[string]string,
) error {
	var secret v1.Secret
	err := r.Get(ctx, secretName, &secret)
	if err == nil {
		return r.renewAndUpdateCertificate(ctx, caSecret, &secret)
	}

	serverSecret, err := generateCertificateFromCA(caSecret, commonName, usage, altDNSNames, secretName)
	if err != nil {
		return err
	}

	utils.SetAsOwnedBy(&serverSecret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	for k, v := range additionalLabels {
		if serverSecret.Labels == nil {
			serverSecret.Labels = make(map[string]string)
		}
		serverSecret.Labels[k] = v
	}
	return r.Create(ctx, serverSecret)
}

// generateCertificateFromCA create a certificate secret using the provided CA secret
func generateCertificateFromCA(
	caSecret *v1.Secret,
	commonName string,
	usage certs.CertType,
	altDNSNames []string,
	secretName client.ObjectKey,
) (*v1.Secret, error) {
	caPair, err := certs.ParseCASecret(caSecret)
	if err != nil {
		return nil, err
	}

	serverPair, err := caPair.CreateAndSignPair(commonName, usage, altDNSNames)
	if err != nil {
		return nil, err
	}

	serverSecret := serverPair.GenerateCertificateSecret(secretName.Namespace, secretName.Name)
	return serverSecret, nil
}

// renewAndUpdateCertificate renew a certificate giving the certificate that contains the CA that sign it and update
// the secret
func (r *ClusterReconciler) renewAndUpdateCertificate(
	ctx context.Context,
	caSecret *v1.Secret,
	secret *v1.Secret,
) error {
	origSecret := secret.DeepCopy()
	hasBeenRenewed, err := certs.RenewLeafCertificate(caSecret, secret)
	if err != nil {
		return err
	}
	if hasBeenRenewed {
		return r.Patch(ctx, secret, client.MergeFrom(origSecret))
	}

	return nil
}
