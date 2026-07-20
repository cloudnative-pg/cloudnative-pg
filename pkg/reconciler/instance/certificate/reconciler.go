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

package certificate

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	postgresSpec "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// Reconciler returns a certificate reconciler
type Reconciler struct {
	cli                      client.Client
	serverCertificateHandler serverCertificateHandler
}

// ErrNoServerCertificateHandler is raised when a new server
// certificate has been detected by the instance reconciler
// but no handler has been set.
var ErrNoServerCertificateHandler = fmt.Errorf("no server certificate handler")

// NewReconciler creates a new certificate reconciler
func NewReconciler(cli client.Client, serverHandler serverCertificateHandler) *Reconciler {
	return &Reconciler{
		cli:                      cli,
		serverCertificateHandler: serverHandler,
	}
}

type serverCertificateHandler interface {
	SetServerCertificate(certificate *tls.Certificate)
	GetServerCertificate() *tls.Certificate
}

// RefreshSecrets is called when the PostgreSQL secrets are changed
// and will refresh the contents of the file inside the Pod, without
// reloading the actual PostgreSQL instance.
//
// It returns a boolean flag telling if something changed. Usually
// the invoker will check that flag and reload the PostgreSQL
// instance it is up.
func (r *Reconciler) RefreshSecrets(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (bool, error) {
	type executor func(context.Context, *apiv1.Cluster) (bool, error)

	contextLogger := log.FromContext(ctx)

	var changed bool

	secretRefresher := func(cb executor) error {
		localChanged, err := cb(ctx, cluster)
		if err == nil {
			changed = changed || localChanged
			return nil
		}

		if !apierrors.IsNotFound(err) {
			return err
		}

		return nil
	}

	if err := secretRefresher(r.refreshServerCertificateFiles); err != nil {
		contextLogger.Error(err, "Error while getting server secret")
		return changed, err
	}
	if err := secretRefresher(r.refreshReplicationUserCertificate); err != nil {
		contextLogger.Error(err, "Error while getting streaming replication secret")
		return changed, err
	}
	if err := secretRefresher(r.refreshClientCA); err != nil {
		contextLogger.Error(err, "Error while getting cluster CA Client secret")
		return changed, err
	}
	if err := secretRefresher(r.refreshServerCA); err != nil {
		contextLogger.Error(err, "Error while getting cluster CA Server secret")
		return changed, err
	}
	if err := secretRefresher(r.refreshBarmanEndpointCA); err != nil {
		contextLogger.Error(err, "Error while getting barman endpoint CA secret")
		return changed, err
	}

	return changed, nil
}

// EnsureServerCertificateLoaded makes sure the status-port web server has a
// server certificate loaded in memory, reading it from its secret when missing.
// It does not write the on-disk certificate files and does not signal a
// PostgreSQL reload: those remain the responsibility of RefreshSecrets.
//
// The instance reconciler calls this before the admission guard so the kubelet
// liveness/readiness/startup probes, served over TLS, keep working even when
// the cached Cluster fails in-pod validation and the guard short-circuits the
// rest of the reconcile loop (RefreshSecrets included). Without it
// GetServerCertificate() stays nil, every probe handshake fails and the pod can
// never recover.
//
// It short-circuits once a certificate is loaded: the probe server only needs a
// usable certificate to be present, and ongoing rotation is handled by
// RefreshSecrets when the cluster is valid. This avoids re-reading the secret on
// every reconcile, since it is never cached in the instance manager.
func (r *Reconciler) EnsureServerCertificateLoaded(ctx context.Context, cluster *apiv1.Cluster) error {
	if r.serverCertificateHandler.GetServerCertificate() != nil {
		return nil
	}

	secretName := cluster.Status.Certificates.ServerTLSSecret
	if secretName == "" {
		// The operator has not populated the certificate status yet; a later
		// reconcile loads the certificate once the secret name is known.
		return nil
	}

	var secret corev1.Secret
	if err := r.cli.Get(
		ctx,
		client.ObjectKey{Namespace: cluster.Namespace, Name: secretName},
		&secret,
	); err != nil {
		// Tolerate a missing secret as RefreshSecrets does: a later reconcile
		// loads the certificate once the secret exists.
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if err := r.refreshInstanceCertificateFromSecret(&secret); err != nil {
		return err
	}

	log.FromContext(ctx).Info(
		"Preloaded TLS certificate for the health probe server",
		"secret", secretName,
	)

	return nil
}

// refreshServerCertificateFiles updates the latest server certificate files
// from the secrets and updates the instance certificate if it is missing or
// outdated.
// It returns true if the configuration has been changed or the instance
// certificate has been updated.
func (r *Reconciler) refreshServerCertificateFiles(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	contextLogger := log.FromContext(ctx)

	var secret corev1.Secret

	err := retry.OnError(retry.DefaultBackoff, func(error) bool { return true },
		func() error {
			err := r.cli.Get(
				ctx,
				client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Status.Certificates.ServerTLSSecret},
				&secret)
			if err != nil {
				contextLogger.Info("Error accessing server TLS Certificate. Retrying with exponential backoff.",
					"secret", cluster.Status.Certificates.ServerTLSSecret)
				return err
			}
			return nil
		})
	if err != nil {
		return false, err
	}

	changed, err := r.refreshCertificateFilesFromSecret(
		ctx,
		&secret,
		postgresSpec.ServerCertificateLocation,
		postgresSpec.ServerKeyLocation)
	if err != nil {
		return changed, err
	}

	if r.serverCertificateHandler.GetServerCertificate() == nil || changed {
		return changed, r.refreshInstanceCertificateFromSecret(&secret)
	}

	return changed, nil
}

// refreshReplicationUserCertificate updates the latest replication user certificates
// from the secrets and updates the corresponding files.
// It returns true if the configuration has been changed.
func (r *Reconciler) refreshReplicationUserCertificate(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (bool, error) {
	var secret corev1.Secret
	err := r.cli.Get(
		ctx,
		client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Status.Certificates.ReplicationTLSSecret},
		&secret)
	if err != nil {
		return false, err
	}

	return r.refreshCertificateFilesFromSecret(
		ctx,
		&secret,
		postgresSpec.StreamingReplicaCertificateLocation,
		postgresSpec.StreamingReplicaKeyLocation)
}

// refreshClientCA updates the latest client CA certificates from the secrets.
// It returns true if the configuration has been changed.
func (r *Reconciler) refreshClientCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	var secret corev1.Secret
	err := r.cli.Get(
		ctx,
		client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Status.Certificates.ClientCASecret},
		&secret)
	if err != nil {
		return false, err
	}

	return r.refreshCAFromSecret(ctx, &secret, postgresSpec.ClientCACertificateLocation)
}

// refreshServerCA gets the latest server CA certificates from the secrets.
// It returns true if the configuration has been changed.
func (r *Reconciler) refreshServerCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	var secret corev1.Secret
	err := r.cli.Get(
		ctx,
		client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Status.Certificates.ServerCASecret},
		&secret)
	if err != nil {
		return false, err
	}

	return r.refreshCAFromSecret(ctx, &secret, postgresSpec.ServerCACertificateLocation)
}

// refreshBarmanEndpointCA updates the barman endpoint CA certificates from the secrets.
// It returns true if the configuration has been changed.
//
// Important: this function is deprecated and will be replaced by the relative feature
// in the plugin-barman-cloud project
func (r *Reconciler) refreshBarmanEndpointCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	endpointCAs := map[string]*apiv1.SecretKeySelector{}
	if cluster.Spec.Backup.IsBarmanEndpointCASet() {
		endpointCAs[postgresSpec.BarmanBackupEndpointCACertificateLocation] = cluster.Spec.Backup.BarmanObjectStore.EndpointCA
	}
	if replicaBarmanCA := cluster.GetBarmanEndpointCAForReplicaCluster(); replicaBarmanCA != nil {
		endpointCAs[postgresSpec.BarmanRestoreEndpointCACertificateLocation] = replicaBarmanCA
	}
	if len(endpointCAs) == 0 {
		return false, nil
	}

	var changed bool
	for target, secretKeySelector := range endpointCAs {
		c, err := writeEndpointCAFromSecret(ctx, r.cli, cluster.Namespace, secretKeySelector, target)
		changed = changed || c
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// RefreshRecoveryBarmanEndpointCA writes to disk the barman endpoint CA of the
// recovery source, so that an instance initializing its data directory in-process
// (a restore, or a volume snapshot restore with PITR) can reach a TLS-protected
// object store before the instance manager, and this certificate reconciler, are
// running. It mirrors the recovery-source precedence the bootstrap Job used to
// encode as a Secret volume mount, and writes to the restore CA location the
// barman-cloud commands read through AWS_CA_BUNDLE. It is a no-op when the
// recovery source has no endpoint CA.
func RefreshRecoveryBarmanEndpointCA(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) (bool, error) {
	endpointCA := recoveryBarmanEndpointCA(cluster, backup)
	if endpointCA == nil || endpointCA.Name == "" || endpointCA.Key == "" {
		return false, nil
	}

	return writeEndpointCAFromSecret(
		ctx, cli, cluster.Namespace, endpointCA, postgresSpec.BarmanRestoreEndpointCACertificateLocation)
}

// recoveryBarmanEndpointCA selects the endpoint CA of the recovery source with
// the same precedence the bootstrap Job builders used: the recovery backup
// reference, then the origin backup status, then the recovery source external
// cluster. It returns nil when the cluster does not bootstrap from recovery or
// the source carries no endpoint CA.
func recoveryBarmanEndpointCA(cluster *apiv1.Cluster, backup *apiv1.Backup) *apiv1.SecretKeySelector {
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.Recovery == nil {
		return nil
	}
	recovery := cluster.Spec.Bootstrap.Recovery

	switch {
	case recovery.Backup != nil && recovery.Backup.EndpointCA != nil:
		return recovery.Backup.EndpointCA
	case backup != nil && backup.Status.EndpointCA != nil:
		return backup.Status.EndpointCA
	case recovery.Source != "":
		if externalCluster, ok := cluster.ExternalCluster(recovery.Source); ok &&
			externalCluster.BarmanObjectStore != nil {
			return externalCluster.BarmanObjectStore.EndpointCA
		}
	}

	return nil
}

// writeEndpointCAFromSecret fetches the referenced Secret and writes the selected
// key to destLocation with an atomic write. It is the single writer of the barman
// endpoint CA files, shared by the steady-state reconcile and the in-process
// bootstrap, so the write logic is not duplicated.
func writeEndpointCAFromSecret(
	ctx context.Context,
	cli client.Client,
	namespace string,
	selector *apiv1.SecretKeySelector,
	destLocation string,
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	var secret corev1.Secret
	if err := cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: selector.Name}, &secret); err != nil {
		return false, err
	}

	data, ok := secret.Data[selector.Key]
	if !ok {
		return false, fmt.Errorf("missing %s entry in Secret", selector.Key)
	}

	changed, err := fileutils.WriteFileAtomic(destLocation, data, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing file: %w", err)
	}

	if changed {
		contextLogger.Info("Refreshed configuration file",
			"filename", destLocation,
			"secret", secret.Name,
			"key", selector.Key)
	}

	return changed, nil
}

// refreshCertificateFilesFromSecret receive a TLS secret, parses it and communicates
// back to the handler the certificate change event.
func (r *Reconciler) refreshInstanceCertificateFromSecret(
	secret *corev1.Secret,
) error {
	if r.serverCertificateHandler == nil {
		return ErrNoServerCertificateHandler
	}

	certData, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return fmt.Errorf("missing %s field in Secret", corev1.TLSCertKey)
	}

	keyData, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return fmt.Errorf("missing %s field in Secret", corev1.TLSPrivateKeyKey)
	}

	certificate, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return fmt.Errorf("failed decoding Secret: %w", err)
	}

	r.serverCertificateHandler.SetServerCertificate(&certificate)

	return err
}

// refreshCertificateFilesFromSecret receive a secret and rewrite the file
// corresponding to the server certificate.
func (r *Reconciler) refreshCertificateFilesFromSecret(
	ctx context.Context,
	secret *corev1.Secret,
	certificateLocation string,
	privateKeyLocation string,
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	certificate, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return false, fmt.Errorf("missing %s field in Secret", corev1.TLSCertKey)
	}

	privateKey, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return false, fmt.Errorf("missing %s field in Secret", corev1.TLSPrivateKeyKey)
	}

	certificateIsChanged, err := fileutils.WriteFileAtomic(certificateLocation, certificate, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing server certificate: %w", err)
	}

	if certificateIsChanged {
		contextLogger.Info("Refreshed configuration file",
			"filename", certificateLocation,
			"secret", secret.Name)
	}

	privateKeyIsChanged, err := fileutils.WriteFileAtomic(privateKeyLocation, privateKey, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing server private key: %w", err)
	}

	if privateKeyIsChanged {
		contextLogger.Info("Refreshed configuration file",
			"filename", privateKeyLocation,
			"secret", secret.Name)
	}

	return certificateIsChanged || privateKeyIsChanged, nil
}

// refreshCAFromSecret receive a secret and rewrite the `ca.crt` file to the provided location.
func (r *Reconciler) refreshCAFromSecret(
	ctx context.Context,
	secret *corev1.Secret,
	destLocation string,
) (bool, error) {
	caCertificate, ok := secret.Data[certs.CACertKey]
	if !ok {
		return false, fmt.Errorf("missing %s entry in Secret", certs.CACertKey)
	}

	changed, err := fileutils.WriteFileAtomic(destLocation, caCertificate, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing server certificate: %w", err)
	}

	if changed {
		log.FromContext(ctx).Info("Refreshed configuration file",
			"filename", destLocation,
			"secret", secret.Name)
	}

	return changed, nil
}
