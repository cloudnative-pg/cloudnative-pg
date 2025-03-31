package certificate

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	v1 "k8s.io/api/core/v1"
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

// refreshServerCertificateFiles gets the latest server certificates files from the
// secrets, and may set the instance certificate if it was missing our outdated.
// Returns true if configuration has been changed or the instance has been updated
func (r *Reconciler) refreshServerCertificateFiles(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	contextLogger := log.FromContext(ctx)

	var secret v1.Secret

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

// refreshReplicationUserCertificate gets the latest replication certificates from the
// secrets. Returns true if configuration has been changed
func (r *Reconciler) refreshReplicationUserCertificate(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (bool, error) {
	var secret v1.Secret
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

// refreshClientCA gets the latest client CA certificates from the secrets.
// It returns true if configuration has been changed
func (r *Reconciler) refreshClientCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	var secret v1.Secret
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
// It returns true if configuration has been changed
func (r *Reconciler) refreshServerCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	var secret v1.Secret
	err := r.cli.Get(
		ctx,
		client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Status.Certificates.ServerCASecret},
		&secret)
	if err != nil {
		return false, err
	}

	return r.refreshCAFromSecret(ctx, &secret, postgresSpec.ServerCACertificateLocation)
}

// refreshBarmanEndpointCA gets the latest barman endpoint CA certificates from the secrets.
// It returns true if configuration has been changed
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
		var secret v1.Secret
		err := r.cli.Get(
			ctx,
			client.ObjectKey{Namespace: cluster.Namespace, Name: secretKeySelector.Name},
			&secret)
		if err != nil {
			return false, err
		}
		c, err := r.refreshFileFromSecret(ctx, &secret, secretKeySelector.Key, target)
		changed = changed || c
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// refreshCertificateFilesFromSecret receive a secret and rewrite the file
// corresponding to the server certificate
func (r *Reconciler) refreshInstanceCertificateFromSecret(
	secret *v1.Secret,
) error {
	certData, ok := secret.Data[v1.TLSCertKey]
	if !ok {
		return fmt.Errorf("missing %s field in Secret", v1.TLSCertKey)
	}

	keyData, ok := secret.Data[v1.TLSPrivateKeyKey]
	if !ok {
		return fmt.Errorf("missing %s field in Secret", v1.TLSPrivateKeyKey)
	}

	certificate, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return fmt.Errorf("failed decoding Secret: %w", err)
	}

	r.serverCertificateHandler.SetServerCertificate(&certificate)

	return err
}

// refreshCertificateFilesFromSecret receive a secret and rewrite the file
// corresponding to the server certificate
func (r *Reconciler) refreshCertificateFilesFromSecret(
	ctx context.Context,
	secret *v1.Secret,
	certificateLocation string,
	privateKeyLocation string,
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	certificate, ok := secret.Data[v1.TLSCertKey]
	if !ok {
		return false, fmt.Errorf("missing %s field in Secret", v1.TLSCertKey)
	}

	privateKey, ok := secret.Data[v1.TLSPrivateKeyKey]
	if !ok {
		return false, fmt.Errorf("missing %s field in Secret", v1.TLSPrivateKeyKey)
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

// refreshCAFromSecret receive a secret and rewrite the ca.crt file to the provided location
func (r *Reconciler) refreshCAFromSecret(
	ctx context.Context,
	secret *v1.Secret,
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

// refreshFileFromSecret receive a secret and rewrite the file corresponding to the key to the provided location
func (r *Reconciler) refreshFileFromSecret(
	ctx context.Context,
	secret *v1.Secret,
	key, destLocation string,
) (bool, error) {
	contextLogger := log.FromContext(ctx)
	data, ok := secret.Data[key]
	if !ok {
		return false, fmt.Errorf("missing %s entry in Secret", key)
	}

	changed, err := fileutils.WriteFileAtomic(destLocation, data, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing file: %w", err)
	}

	if changed {
		contextLogger.Info("Refreshed configuration file",
			"filename", destLocation,
			"secret", secret.Name,
			"key", key)
	}

	return changed, nil
}
