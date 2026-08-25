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

// reconcileClientCertificate issues, renews or deletes the role's client
// certificate, depending on whether issuance is enabled.
func (r *DatabaseRoleReconciler) reconcileClientCertificate(
	ctx context.Context,
	role *apiv1.DatabaseRole,
) error {
	// Cleaning up a previously generated Secret does not need the cluster.
	if !role.IsClientCertificateEnabled() {
		return r.deleteOwnedCertSecret(ctx, role)
	}

	var cluster apiv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: role.Namespace,
		Name:      role.Spec.ClusterRef.Name,
	}, &cluster); apierrs.IsNotFound(err) {
		setClientCertMessage(ctx, role, fmt.Sprintf(
			"Cluster %q not found: no client certificate can be issued until it exists",
			role.Spec.ClusterRef.Name))
		return nil
	} else if err != nil {
		return fmt.Errorf("while getting cluster %q: %w", role.Spec.ClusterRef.Name, err)
	}

	return r.issueClientCertificate(ctx, role, &cluster)
}

// issueClientCertificate ensures the client certificate Secret is present and up
// to date. It sets role.Status.ClientCertificate in memory, leaving the caller to
// persist it.
func (r *DatabaseRoleReconciler) issueClientCertificate(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	cluster *apiv1.Cluster,
) error {
	secretKey := clientCertSecretKey(role)

	var caSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.GetClientCASecretName(),
	}, &caSecret); apierrs.IsNotFound(err) {
		setClientCertMessage(ctx, role, fmt.Sprintf(
			"client CA secret %q not found: no client certificate can be issued until it exists",
			cluster.GetClientCASecretName()))
		return nil
	} else if err != nil {
		return fmt.Errorf("while getting client CA secret %q: %w", cluster.GetClientCASecretName(), err)
	}

	if _, ok := caSecret.Data[certs.CAPrivateKeyKey]; !ok {
		setClientCertMessage(ctx, role, fmt.Sprintf("client CA secret %q has no private key; "+
			"bring-your-own-CA clusters require manual certificate management", caSecret.Name))
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
		newSecret, err := generateCertificateFromCA(
			&caSecret, role.Spec.Name, certs.CertTypeClient, nil, secretKey, clientCertDuration(role))
		if err != nil {
			return fmt.Errorf("while signing client cert for role %q: %w", role.Spec.Name, err)
		}
		if err := ctrl.SetControllerReference(role, newSecret, r.Scheme); err != nil {
			return fmt.Errorf("while setting owner reference on cert secret %q: %w", secretKey.Name, err)
		}
		if err := r.Create(ctx, newSecret); err != nil {
			return fmt.Errorf("while creating cert secret %q: %w", secretKey.Name, err)
		}

		certSecret = *newSecret

	default:
		return fmt.Errorf("while getting cert secret %q: %w", secretKey.Name, err)
	}

	leaf, err := parseClientCert(&certSecret)
	if err != nil {
		return err
	}
	role.Status.ClientCertificate = &apiv1.ClientCertificateState{
		Expiration: leaf.NotAfter.UTC().Format(time.RFC3339),
	}
	return nil
}

// ensureOwnedCertSecretUpToDate re-issues the certificate in an existing Secret
// when it is no longer usable, and leaves it alone otherwise. The returned owned
// flag is false when the Secret is not controlled by the role, in which case the
// caller must not record certificate status.
func (r *DatabaseRoleReconciler) ensureOwnedCertSecretUpToDate(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	caSecret, certSecret *corev1.Secret,
	secretKey client.ObjectKey,
) (owned bool, err error) {
	if !metav1.IsControlledBy(certSecret, role) {
		setClientCertMessage(ctx, role, fmt.Sprintf(
			"Secret %q already exists and is not owned by this DatabaseRole", secretKey.Name))
		return false, nil
	}

	// An unreadable certificate is re-issued rather than error-looped on, and
	// getting it out of the way first leaves leaf usable by every check below.
	leaf, err := parseClientCert(certSecret)
	if err != nil {
		return true, r.reissueClientCert(ctx, role, caSecret, certSecret, secretKey,
			fmt.Sprintf("the certificate is unreadable: %v", err))
	}

	_, keyErr := certs.ParseServerSecret(certSecret)
	lifetime := clientCertDuration(role)
	renewBefore := clientCertRenewBefore(role, lifetime)

	var reason string
	switch {
	case !clientCertSignedByCurrentCA(ctx, caSecret, certSecret, leaf):
		reason = "the client CA changed"
	case keyErr != nil:
		reason = fmt.Sprintf("the private key is missing or does not match the certificate: %v", keyErr)
	case leaf.NotAfter.Sub(leaf.NotBefore) != lifetime.Truncate(time.Second):
		reason = fmt.Sprintf("the requested lifetime %s differs from the issued %s",
			lifetime, leaf.NotAfter.Sub(leaf.NotBefore))
	case clientCertNeedsRenewal(leaf, renewBefore):
		reason = fmt.Sprintf("it is inside the %s renewal window of the %s expiry",
			renewBefore, leaf.NotAfter.UTC().Format(time.RFC3339))
	}

	if reason == "" {
		return true, nil
	}
	return true, r.reissueClientCert(ctx, role, caSecret, certSecret, secretKey, reason)
}

// reissueClientCert signs a new certificate for the role and patches it into the
// Secret it already owns, replacing the private key along with it.
func (r *DatabaseRoleReconciler) reissueClientCert(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	caSecret, certSecret *corev1.Secret,
	secretKey client.ObjectKey,
	reason string,
) error {
	log.FromContext(ctx).Info("re-issuing client certificate", "secret", secretKey.Name, "reason", reason)

	origSecret := certSecret.DeepCopy()
	newSecret, err := generateCertificateFromCA(
		caSecret, role.Spec.Name, certs.CertTypeClient, nil, secretKey, clientCertDuration(role))
	if err != nil {
		return fmt.Errorf("while re-signing client cert for role %q: %w", role.Spec.Name, err)
	}
	certSecret.Data = newSecret.Data

	if err := r.Patch(ctx, certSecret, client.MergeFrom(origSecret)); err != nil {
		return fmt.Errorf("while patching cert secret %q: %w", secretKey.Name, err)
	}
	return nil
}

// deleteOwnedCertSecret deletes the cert Secret if it exists and is owned by
// the given role. Unowned Secrets with the same name are left untouched.
func (r *DatabaseRoleReconciler) deleteOwnedCertSecret(
	ctx context.Context,
	role *apiv1.DatabaseRole,
) error {
	secretKey := clientCertSecretKey(role)

	var secret corev1.Secret
	if err := r.Get(ctx, secretKey, &secret); apierrs.IsNotFound(err) {
		role.Status.ClientCertificate = nil
		return nil
	} else if err != nil {
		return fmt.Errorf("while getting cert secret %q: %w", secretKey.Name, err)
	}

	if !metav1.IsControlledBy(&secret, role) {
		setClientCertMessage(ctx, role, fmt.Sprintf(
			"Secret %q is not owned by this DatabaseRole and will not be deleted automatically",
			secretKey.Name,
		))
		return nil
	}

	if err := r.Delete(ctx, &secret); err != nil && !apierrs.IsNotFound(err) {
		return fmt.Errorf("while deleting cert secret %q: %w", secretKey.Name, err)
	}

	role.Status.ClientCertificate = nil
	return nil
}

// clientCertSecretKey returns the key of the role's client certificate Secret.
func clientCertSecretKey(role *apiv1.DatabaseRole) client.ObjectKey {
	return client.ObjectKey{Namespace: role.Namespace, Name: role.GetClientCertSecretName()}
}

// setClientCertMessage records why the client certificate could not be issued,
// keeping any expiration already observed: a certificate signed before the
// problem appeared is still usable until it expires.
func setClientCertMessage(ctx context.Context, role *apiv1.DatabaseRole, message string) {
	log.FromContext(ctx).Info(message, "role", role.Name)
	if role.Status.ClientCertificate == nil {
		role.Status.ClientCertificate = &apiv1.ClientCertificateState{}
	}
	role.Status.ClientCertificate.Message = message
}

// parseClientCert returns the leaf certificate stored in the Secret.
func parseClientCert(secret *corev1.Secret) (*x509.Certificate, error) {
	certPEM, ok := secret.Data[certs.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf("cert secret %q missing key %q", secret.Name, certs.TLSCertKey)
	}
	pair := certs.KeyPair{Certificate: certPEM}
	leaf, err := pair.ParseCertificate()
	if err != nil {
		return nil, fmt.Errorf("while parsing client cert in secret %q: %w", secret.Name, err)
	}
	return leaf, nil
}

// clientCertSignedByCurrentCA reports whether the already parsed leaf, stored in
// certSecret, chains to the CA currently in caSecret. A false result means the
// CA was rotated.
func clientCertSignedByCurrentCA(
	ctx context.Context, caSecret, certSecret *corev1.Secret, leaf *x509.Certificate,
) bool {
	caPair := &certs.KeyPair{Certificate: caSecret.Data[certs.CACertKey]}
	certPair := certs.KeyPair{Certificate: certSecret.Data[certs.TLSCertKey]}

	// Verifying at the certificate's own NotBefore keeps an imminent expiry from
	// being reported as a CA change. An empty KeyUsages would default to
	// server-auth and reject a correctly signed client certificate.
	opts := &x509.VerifyOptions{
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		CurrentTime: leaf.NotBefore,
	}
	if err := certPair.IsValid(caPair, opts); err != nil {
		log.FromContext(ctx).Debug("cert chain validation failed, treating as CA change",
			"secret", certSecret.Name, "err", err)
		return false
	}
	return true
}

// clientCertDuration returns the lifetime the role's client certificate is issued
// with, falling back to the operator-wide default.
func clientCertDuration(role *apiv1.DatabaseRole) time.Duration {
	if role.Spec.ClientCertificate == nil || role.Spec.ClientCertificate.Duration == nil {
		return certs.CertificateDuration()
	}
	return role.Spec.ClientCertificate.Duration.Duration
}

// clientCertRenewBefore returns how long before expiry the certificate is
// renewed: the explicit value when set, otherwise the operator's expiring-check
// threshold. The cap is the bound the API enforces on an explicit renewBefore,
// and keeps a short-lived certificate from being born inside its own window.
func clientCertRenewBefore(role *apiv1.DatabaseRole, lifetime time.Duration) time.Duration {
	if role.Spec.ClientCertificate != nil && role.Spec.ClientCertificate.RenewBefore != nil {
		return role.Spec.ClientCertificate.RenewBefore.Duration
	}

	return min(certs.CheckThreshold(), lifetime/2)
}

// clientCertNeedsRenewal reports whether the validity dates of the certificate
// call for a new one. A certificate whose validity has not started yet is
// renewed too: it was signed by a clock running ahead of this one, and is
// unusable until wall-clock time reaches its notBefore.
func clientCertNeedsRenewal(leaf *x509.Certificate, renewBefore time.Duration) bool {
	now := time.Now()
	return now.Before(leaf.NotBefore) || now.Add(renewBefore).After(leaf.NotAfter)
}

// clientCertReconcileInterval is the longest a role with client certificate
// issuance enabled goes unchecked.
const clientCertReconcileInterval = time.Hour

// clientCertRequeueAfter returns how long to wait before looking at the role's
// client certificate again: twice per renewal window is enough to renew before
// expiry, and the interval keeps a long-lived certificate from waking the
// reconciler more often than hourly.
func clientCertRequeueAfter(role *apiv1.DatabaseRole) time.Duration {
	renewBefore := clientCertRenewBefore(role, clientCertDuration(role))
	return min(renewBefore/2, clientCertReconcileInterval)
}
