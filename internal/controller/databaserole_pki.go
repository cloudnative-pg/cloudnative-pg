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

package controller

import (
	"context"
	"crypto/x509"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
)

// reconcileClientCertificate is the top-level entry point for certificate
// lifecycle management. It either issues/renews the certificate or deletes it,
// depending on whether clientCertificate issuance is enabled.
func (r *DatabaseRoleReconciler) reconcileClientCertificate(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	cluster *apiv1.Cluster,
) error {
	secretKey := client.ObjectKey{
		Namespace: role.Namespace,
		Name:      role.GetClientCertSecretName(),
	}

	// When issuance is disabled we only need to clean up a previously generated
	// Secret; the cluster is not required for that.
	if !role.IsClientCertificateEnabled() {
		return r.deleteOwnedCertSecret(ctx, role, secretKey)
	}

	if cluster == nil {
		return nil
	}

	return r.issueClientCertificate(ctx, role, cluster, secretKey)
}

// issueClientCertificate ensures the TLS client certificate Secret for the
// role is present and up to date. The cluster must already be fetched by the
// caller. It modifies role.Status.ClientCertificate in memory; the caller is
// responsible for persisting the status.
func (r *DatabaseRoleReconciler) issueClientCertificate(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	cluster *apiv1.Cluster,
	secretKey client.ObjectKey,
) error {
	contextLogger := log.FromContext(ctx)

	var caSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.GetClientCASecretName(),
	}, &caSecret); apierrs.IsNotFound(err) {
		contextLogger.Info("client CA secret not found, will retry later",
			"caSecret", cluster.GetClientCASecretName())
		return nil
	} else if err != nil {
		return fmt.Errorf("while getting client CA secret %q: %w", cluster.GetClientCASecretName(), err)
	}

	if _, ok := caSecret.Data[certs.CAPrivateKeyKey]; !ok {
		contextLogger.Info("client CA secret has no private key, cannot issue client certificate; "+
			"bring-your-own-CA clusters require manual certificate management",
			"caSecret", caSecret.Name)
		role.Status.ClientCertificate = &apiv1.ClientCertificateState{
			Message: fmt.Sprintf("client CA secret %q has no private key; "+
				"bring-your-own-CA clusters require manual certificate management", caSecret.Name),
		}
		return nil
	}

	var certSecret corev1.Secret
	err := r.Get(ctx, secretKey, &certSecret)
	switch {
	case err == nil:
		owned, err := r.ensureOwnedCertSecretUpToDate(ctx, role, &caSecret, &certSecret, secretKey)
		if err != nil {
			return err
		}
		if !owned {
			return nil
		}

	case apierrs.IsNotFound(err):
		newSecret, err := generateCertificateFromCA(&caSecret, role.Spec.Name, certs.CertTypeClient, nil, secretKey)
		if err != nil {
			return fmt.Errorf("while signing client cert for role %q: %w", role.Spec.Name, err)
		}
		if err := ctrl.SetControllerReference(role, newSecret, r.Scheme); err != nil {
			return fmt.Errorf("while setting owner reference on cert secret %q: %w", secretKey.Name, err)
		}
		if err := r.Create(ctx, newSecret); err != nil {
			return fmt.Errorf("while creating cert secret %q: %w", secretKey.Name, err)
		}
		r.Recorder.Eventf(role, "Normal", "ClientCertificateIssued",
			"Issued a client certificate for role %q into Secret %q", role.Spec.Name, secretKey.Name)

		certSecret = *newSecret

	default:
		return fmt.Errorf("while getting cert secret %q: %w", secretKey.Name, err)
	}

	expiration, err := clientCertExpiration(&certSecret)
	if err != nil {
		return err
	}
	role.Status.ClientCertificate = &apiv1.ClientCertificateState{
		Expiration: expiration,
	}
	return nil
}

// ensureOwnedCertSecretUpToDate reconciles an already-existing cert Secret. It
// refuses to touch a Secret the role does not own, re-issues the certificate
// when the cluster's client CA has been rotated, and otherwise renews it as it
// approaches expiry. The returned owned flag is false when the Secret is not
// controlled by the role, in which case the caller must not record certificate
// status.
func (r *DatabaseRoleReconciler) ensureOwnedCertSecretUpToDate(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	caSecret, certSecret *corev1.Secret,
	secretKey client.ObjectKey,
) (owned bool, err error) {
	contextLogger := log.FromContext(ctx)

	// Never touch a same-named Secret the operator does not own: it may belong
	// to the user or another component. Report the conflict in the status
	// rather than overwriting it.
	if !metav1.IsControlledBy(certSecret, role) {
		contextLogger.Warning("cert secret exists but is not owned by this DatabaseRole, skipping issuance",
			"secret", secretKey.Name)
		role.Status.ClientCertificate = &apiv1.ClientCertificateState{
			Message: fmt.Sprintf(secretNotOwnedMessage, secretKey.Name),
		}
		return false, nil
	}

	origSecret := certSecret.DeepCopy()

	// Set to why the certificate must be re-issued rather than renewed in
	// place: RenewLeafCertificate alone doesn't handle a CA rotation, or a
	// certificate that can't be read or renewed.
	var reissueReason string

	signedByCurrentCA, readErr := clientCertSignedByCurrentCA(ctx, caSecret, certSecret)
	renewed, renewErr := false, error(nil)
	if readErr == nil && signedByCurrentCA {
		renewed, renewErr = certs.RenewLeafCertificate(caSecret, certSecret, nil)
	}

	switch {
	case readErr != nil:
		contextLogger.Warning("client cert is unreadable, re-issuing",
			"secret", secretKey.Name, "err", readErr)
		reissueReason = "it could not be read"

	case !signedByCurrentCA:
		contextLogger.Info("client CA changed, re-issuing client certificate", "secret", secretKey.Name)
		reissueReason = "the client CA of the cluster was rotated"

	case renewErr != nil:
		contextLogger.Warning("client cert renewal failed, re-issuing",
			"secret", secretKey.Name, "err", renewErr)
		reissueReason = "it could not be renewed"

	case !renewed:
		// The certificate is still the one the role should present.
		return true, nil
	}

	// The reason recorded on the event below: a renewal, a re-issue after a CA
	// rotation, or a replacement of an unreadable certificate.
	reason := "it was approaching its expiration"
	if reissueReason != "" {
		reason = reissueReason
		newSecret, err := generateCertificateFromCA(caSecret, role.Spec.Name, certs.CertTypeClient, nil, secretKey)
		if err != nil {
			return false, fmt.Errorf("while re-signing client cert for role %q: %w", role.Spec.Name, err)
		}
		certSecret.Data = newSecret.Data
	}

	if err := r.Patch(ctx, certSecret, client.MergeFrom(origSecret)); err != nil {
		return false, fmt.Errorf("while patching cert secret %q: %w", secretKey.Name, err)
	}

	// Recorded once the new certificate has reached its Secret: the previous one
	// stops being the credential of the role at that point, and a client that
	// mounted it has to read the Secret again.
	r.Recorder.Eventf(role, "Normal", "ClientCertificateRenewed",
		"Renewed the client certificate of role %q in Secret %q, since %s",
		role.Spec.Name, secretKey.Name, reason)
	return true, nil
}

// certificateMessage returns the explanation the operator recorded about the
// client certificate of the role, if it recorded one.
func certificateMessage(role *apiv1.DatabaseRole) string {
	if role.Status.ClientCertificate == nil {
		return ""
	}
	return role.Status.ClientCertificate.Message
}

// deleteOwnedCertSecret deletes the cert Secret if it exists and is owned by
// the given role. Unowned Secrets with the same name are left untouched.
func (r *DatabaseRoleReconciler) deleteOwnedCertSecret(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	secretKey client.ObjectKey,
) error {
	deleted, err := r.deleteOwnedSecret(ctx, role, secretKey)
	if err != nil {
		return err
	}
	if !deleted {
		role.Status.ClientCertificate = &apiv1.ClientCertificateState{
			Message: fmt.Sprintf(secretNotDeletableMessage, secretKey.Name),
		}
		return nil
	}

	role.Status.ClientCertificate = nil
	return nil
}

// clientCertSignedByCurrentCA reports whether the leaf certificate in certSecret
// is signed by, and chains to, the CA currently stored in caSecret. The check is
// performed at the certificate's own NotBefore time so that an imminent expiry
// does not mask a CA change; expiry is handled separately by renewal. A false
// result with no error means the certificate must be re-issued, typically
// because the cluster's client CA was rotated.
func clientCertSignedByCurrentCA(ctx context.Context, caSecret, certSecret *corev1.Secret) (bool, error) {
	caPair := &certs.KeyPair{Certificate: caSecret.Data[certs.CACertKey]}

	certPEM, ok := certSecret.Data[certs.TLSCertKey]
	if !ok {
		return false, fmt.Errorf("cert secret %q missing key %q", certSecret.Name, certs.TLSCertKey)
	}
	certPair := certs.KeyPair{Certificate: certPEM}
	leaf, err := certPair.ParseCertificate()
	if err != nil {
		return false, fmt.Errorf("while parsing client cert in secret %q: %w", certSecret.Name, err)
	}

	// Pin verification time to the certificate's NotBefore and require client
	// auth usage: an empty KeyUsages would default to server-auth and reject a
	// correctly signed client certificate.
	opts := &x509.VerifyOptions{
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		CurrentTime: leaf.NotBefore,
	}
	if err := certPair.IsValid(caPair, opts); err != nil {
		log.FromContext(ctx).Debug("cert chain validation failed, treating as CA change",
			"secret", certSecret.Name, "err", err)
		return false, nil
	}
	return true, nil
}

// clientCertExpiration returns the NotAfter time of the certificate stored in
// the Secret as an RFC3339 string.
func clientCertExpiration(secret *corev1.Secret) (string, error) {
	certPEM, ok := secret.Data[certs.TLSCertKey]
	if !ok {
		return "", fmt.Errorf("secret %q missing key %q", secret.Name, certs.TLSCertKey)
	}
	pair := certs.KeyPair{Certificate: certPEM}
	cert, err := pair.ParseCertificate()
	if err != nil {
		return "", fmt.Errorf("while reading expiration from cert secret %q: %w", secret.Name, err)
	}
	return cert.NotAfter.UTC().Format(time.RFC3339), nil
}
