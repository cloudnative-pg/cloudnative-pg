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

	var cluster apiv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: role.Namespace,
		Name:      role.Spec.ClusterRef.Name,
	}, &cluster); apierrs.IsNotFound(err) {
		log.FromContext(ctx).Info("cluster not found, will retry when it appears",
			"cluster", role.Spec.ClusterRef.Name)
		setClientCertMessage(role, fmt.Sprintf(
			"Cluster %q not found: no client certificate can be issued until it exists",
			role.Spec.ClusterRef.Name))
		return nil
	} else if err != nil {
		return fmt.Errorf("while getting cluster %q: %w", role.Spec.ClusterRef.Name, err)
	}

	return r.issueClientCertificate(ctx, role, &cluster)
}

// issueClientCertificate ensures the TLS client certificate Secret for the
// role is present and up to date. The cluster must already be fetched by the
// caller. It modifies role.Status.ClientCertificate in memory; the caller is
// responsible for persisting the status.
func (r *DatabaseRoleReconciler) issueClientCertificate(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	cluster *apiv1.Cluster,
) error {
	contextLogger := log.FromContext(ctx)
	secretKey := client.ObjectKey{Namespace: role.Namespace, Name: role.GetClientCertSecretName()}

	var caSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.GetClientCASecretName(),
	}, &caSecret); apierrs.IsNotFound(err) {
		contextLogger.Info("client CA secret not found, will retry later",
			"caSecret", cluster.GetClientCASecretName())
		setClientCertMessage(role, fmt.Sprintf(
			"client CA secret %q not found: no client certificate can be issued until it exists",
			cluster.GetClientCASecretName()))
		return nil
	} else if err != nil {
		return fmt.Errorf("while getting client CA secret %q: %w", cluster.GetClientCASecretName(), err)
	}

	if _, ok := caSecret.Data[certs.CAPrivateKeyKey]; !ok {
		contextLogger.Info("client CA secret has no private key, cannot issue client certificate; "+
			"bring-your-own-CA clusters require manual certificate management",
			"caSecret", caSecret.Name)
		setClientCertMessage(role, fmt.Sprintf("client CA secret %q has no private key; "+
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
// refuses to touch a Secret the role does not own, and otherwise re-issues the
// certificate when the cluster's client CA has been rotated, when the stored
// private key no longer matches the certificate, when the role's requested
// lifetime no longer matches the issued one, or when the validity dates of the
// certificate call for a new one. When none of those apply the Secret is left
// alone, without a write. The returned owned flag is false when the Secret is
// not controlled by the role, in which case the caller must not record
// certificate status.
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
		setClientCertMessage(role, fmt.Sprintf(
			"Secret %q already exists and is not owned by this DatabaseRole", secretKey.Name))
		return false, nil
	}

	// A parse or chain-verification error means the cert is corrupt; treat it
	// as a re-issue trigger rather than error-looping.
	needsReissue := false
	signedByCurrentCA, leaf, err := clientCertSignedByCurrentCA(ctx, caSecret, certSecret)
	switch {
	case err != nil:
		contextLogger.Warning("client cert is unreadable, re-issuing",
			"secret", secretKey.Name, "err", err)
		needsReissue = true
	case !signedByCurrentCA:
		contextLogger.Info("client CA changed, re-issuing client certificate", "secret", secretKey.Name)
		needsReissue = true
	default:
		effectiveDuration := clientCertEffectiveDuration(role)
		renewBefore := clientCertRenewBefore(role, effectiveDuration)
		switch {
		case !clientCertKeyMatchesCert(certSecret):
			contextLogger.Warning("client cert private key is missing or does not match the certificate, re-issuing",
				"secret", secretKey.Name)
			needsReissue = true
		case leaf.NotAfter.Sub(leaf.NotBefore) != effectiveDuration.Truncate(time.Second):
			// The requested lifetime changed since the certificate was issued,
			// either because the role now sets a different duration or
			// because the operator-wide default did.
			contextLogger.Info("client certificate lifetime changed, re-issuing client certificate",
				"secret", secretKey.Name,
				"issuedLifetime", leaf.NotAfter.Sub(leaf.NotBefore).String(),
				"requestedLifetime", effectiveDuration.String())
			needsReissue = true
		case clientCertNeedsRenewal(leaf, renewBefore):
			contextLogger.Info("client certificate is due for renewal, re-issuing client certificate",
				"secret", secretKey.Name,
				"notBefore", leaf.NotBefore,
				"expiration", leaf.NotAfter,
				"renewBefore", renewBefore.String())
			needsReissue = true
		}
	}

	if !needsReissue {
		return true, nil
	}

	origSecret := certSecret.DeepCopy()
	newSecret, err := generateCertificateFromCA(
		caSecret, role.Spec.Name, certs.CertTypeClient, nil, secretKey, clientCertDuration(role))
	if err != nil {
		return false, fmt.Errorf("while re-signing client cert for role %q: %w", role.Spec.Name, err)
	}
	certSecret.Data = newSecret.Data

	if err := r.Patch(ctx, certSecret, client.MergeFrom(origSecret)); err != nil {
		return false, fmt.Errorf("while patching cert secret %q: %w", secretKey.Name, err)
	}
	return true, nil
}

// deleteOwnedCertSecret deletes the cert Secret if it exists and is owned by
// the given role. Unowned Secrets with the same name are left untouched.
func (r *DatabaseRoleReconciler) deleteOwnedCertSecret(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	secretKey client.ObjectKey,
) error {
	var secret corev1.Secret
	if err := r.Get(ctx, secretKey, &secret); apierrs.IsNotFound(err) {
		role.Status.ClientCertificate = nil
		return nil
	} else if err != nil {
		return fmt.Errorf("while getting cert secret %q: %w", secretKey.Name, err)
	}

	if metav1.IsControlledBy(&secret, role) {
		if err := r.Delete(ctx, &secret); err != nil && !apierrs.IsNotFound(err) {
			return fmt.Errorf("while deleting cert secret %q: %w", secretKey.Name, err)
		}
	} else {
		log.FromContext(ctx).Warning("cert secret exists but is not owned by this DatabaseRole, skipping deletion",
			"secret", secretKey.Name)
		setClientCertMessage(role, fmt.Sprintf(
			"Secret %q is not owned by this DatabaseRole and will not be deleted automatically",
			secretKey.Name,
		))
		return nil
	}

	role.Status.ClientCertificate = nil
	return nil
}

// setClientCertMessage records on the role's status why the client certificate
// could not be issued, or why an existing Secret was left alone. Any expiration
// already observed is kept: a certificate signed before the problem appeared is
// still usable, and its expiry is what tells the administrator for how long.
func setClientCertMessage(role *apiv1.DatabaseRole, message string) {
	if role.Status.ClientCertificate == nil {
		role.Status.ClientCertificate = &apiv1.ClientCertificateState{}
	}
	role.Status.ClientCertificate.Message = message
}

// clientCertSignedByCurrentCA reports whether the leaf certificate in certSecret
// is signed by, and chains to, the CA currently stored in caSecret, returning
// the parsed leaf so the caller can reuse it instead of parsing the Secret
// again. The check is performed at the certificate's own NotBefore time so
// that an imminent expiry does not mask a CA change; expiry is handled
// separately by renewal. A false result with no error means the certificate
// must be re-issued, typically because the cluster's client CA was rotated.
func clientCertSignedByCurrentCA(
	ctx context.Context, caSecret, certSecret *corev1.Secret,
) (bool, *x509.Certificate, error) {
	caPair := &certs.KeyPair{Certificate: caSecret.Data[certs.CACertKey]}

	certPEM, ok := certSecret.Data[certs.TLSCertKey]
	if !ok {
		return false, nil, fmt.Errorf("cert secret %q missing key %q", certSecret.Name, certs.TLSCertKey)
	}
	certPair := certs.KeyPair{Certificate: certPEM}
	leaf, err := certPair.ParseCertificate()
	if err != nil {
		return false, nil, fmt.Errorf("while parsing client cert in secret %q: %w", certSecret.Name, err)
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
		return false, leaf, nil
	}
	return true, leaf, nil
}

// clientCertKeyMatchesCert reports whether the Secret carries a private key
// that matches its certificate. A Secret whose key was deleted, or replaced
// with one belonging to another certificate, is useless for client
// authentication, so it is re-issued rather than left in place until its
// renewal window.
func clientCertKeyMatchesCert(certSecret *corev1.Secret) bool {
	_, err := certs.ParseServerSecret(certSecret)
	return err == nil
}

// clientCertDuration returns the lifetime of the role's client certificate, or
// zero to use the operator-wide default.
func clientCertDuration(role *apiv1.DatabaseRole) time.Duration {
	if role.Spec.ClientCertificate == nil || role.Spec.ClientCertificate.Duration == nil {
		return 0
	}
	return role.Spec.ClientCertificate.Duration.Duration
}

// clientCertRenewBefore returns how long before expiry the certificate is
// renewed: the explicit value when set, otherwise the operator's expiring-check
// threshold capped at half the lifetime. The cap matters because a short
// duration with no explicit renewBefore would otherwise leave the certificate
// permanently inside a threshold longer than its own lifetime; it is the same
// bound the API enforces on an explicit renewBefore, so a renewal window can
// never cover more than half of the certificate's lifetime.
func clientCertRenewBefore(role *apiv1.DatabaseRole, lifetime time.Duration) time.Duration {
	if role.Spec.ClientCertificate != nil && role.Spec.ClientCertificate.RenewBefore != nil {
		return role.Spec.ClientCertificate.RenewBefore.Duration
	}

	renewBefore := certs.CheckThreshold()
	if renewBefore > lifetime/2 {
		renewBefore = lifetime / 2
	}
	return renewBefore
}

// clientCertNeedsRenewal reports whether the validity dates of the certificate
// call for a new one. Besides having entered its renewal window, a certificate
// whose validity has not started yet is also renewed: it is unusable until
// wall-clock time reaches its notBefore, which happens when the clock the
// certificate was signed with ran ahead of the current one.
func clientCertNeedsRenewal(leaf *x509.Certificate, renewBefore time.Duration) bool {
	now := time.Now()
	return now.Before(leaf.NotBefore) || now.Add(renewBefore).After(leaf.NotAfter)
}

// clientCertEffectiveDuration returns the lifetime the role's client
// certificate is issued with, resolving an unset duration to the operator-wide
// default.
func clientCertEffectiveDuration(role *apiv1.DatabaseRole) time.Duration {
	if lifetime := clientCertDuration(role); lifetime > 0 {
		return lifetime
	}
	return certs.CertificateDuration()
}

// clientCertRequeueAfter returns how long to wait before looking at the role's
// client certificate again. Checking twice per renewal window is enough to
// renew before expiry, while clientCertReconcileInterval keeps a long-lived
// certificate from waking the reconciler up more often than hourly. The API
// floors on duration and renewBefore keep the result well above the point
// where requeueing would become a busy loop.
func clientCertRequeueAfter(role *apiv1.DatabaseRole) time.Duration {
	renewBefore := clientCertRenewBefore(role, clientCertEffectiveDuration(role))
	return min(renewBefore/2, clientCertReconcileInterval)
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
