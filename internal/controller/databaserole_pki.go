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
// depending on spec.issueClientCertificate.
func (r *DatabaseRoleReconciler) reconcileClientCertificate(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	cluster *apiv1.Cluster,
) error {
	if role.Spec.IssueClientCertificate {
		return r.issueClientCertificate(ctx, role, cluster)
	}

	return r.deleteOwnedCertSecret(
		ctx,
		role,
		client.ObjectKey{
			Namespace: role.Namespace,
			Name:      role.GetClientCertSecretName(),
		},
	)
}

// issueClientCertificate ensures the TLS client certificate Secret for the
// role is present and up to date. The cluster must already be fetched by the
// caller. It modifies role.Status.ClientCertificateExpiration in memory; the
// caller is responsible for persisting the status.
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
		origSecret := certSecret.DeepCopy()
		renewed, err := certs.RenewLeafCertificate(&caSecret, &certSecret, nil)
		if err != nil {
			return fmt.Errorf("while renewing client cert for role %q: %w", role.Spec.Name, err)
		}
		if renewed {
			if err := r.Patch(ctx, &certSecret, client.MergeFrom(origSecret)); err != nil {
				return fmt.Errorf("while patching cert secret %q: %w", secretKey.Name, err)
			}
		}

	case apierrs.IsNotFound(err):
		caPair, err := certs.ParseCASecret(&caSecret)
		if err != nil {
			return fmt.Errorf("while parsing client CA secret: %w", err)
		}

		pair, err := caPair.CreateAndSignPair(role.Spec.Name, certs.CertTypeClient, nil)
		if err != nil {
			return fmt.Errorf("while signing client cert for role %q: %w", role.Spec.Name, err)
		}

		newSecret := pair.GenerateCertificateSecret(role.Namespace, secretKey.Name)
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
	}

	role.Status.ClientCertificate = nil
	return nil
}

// clientCertExpiration returns the NotAfter time of the certificate stored in
// the Secret as an RFC3339 string.
func clientCertExpiration(secret *corev1.Secret) (string, error) {
	certPEM, ok := secret.Data[certs.TLSCertKey]
	if !ok {
		return "", fmt.Errorf("secret %q missing key %q", secret.Name, certs.TLSCertKey)
	}
	pair := certs.KeyPair{Certificate: certPEM}
	_, notAfter, err := pair.IsExpiring()
	if err != nil {
		return "", fmt.Errorf("while reading expiration from cert secret %q: %w", secret.Name, err)
	}
	if notAfter == nil {
		return "", nil
	}
	return notAfter.UTC().Format(time.RFC3339), nil
}
