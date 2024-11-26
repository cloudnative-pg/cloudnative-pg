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

package controller

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/controller"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/archiver"
	postgresSpec "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// refreshServerCertificateFiles gets the latest server certificates files from the
// secrets, and may set the instance certificate if it was missing our outdated.
// Returns true if configuration has been changed or the instance has been updated
func (r *InstanceReconciler) refreshServerCertificateFiles(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	contextLogger := log.FromContext(ctx)

	var secret corev1.Secret

	err := retry.OnError(retry.DefaultBackoff, func(error) bool { return true },
		func() error {
			err := r.GetClient().Get(
				ctx,
				client.ObjectKey{Namespace: r.instance.GetNamespaceName(), Name: cluster.Status.Certificates.ServerTLSSecret},
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

	if r.instance.ServerCertificate == nil || changed {
		return changed, r.refreshInstanceCertificateFromSecret(&secret)
	}

	return changed, nil
}

// refreshReplicationUserCertificate gets the latest replication certificates from the
// secrets. Returns true if configuration has been changed
func (r *InstanceReconciler) refreshReplicationUserCertificate(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (bool, error) {
	var secret corev1.Secret
	err := r.GetClient().Get(
		ctx,
		client.ObjectKey{Namespace: r.instance.GetNamespaceName(), Name: cluster.Status.Certificates.ReplicationTLSSecret},
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
func (r *InstanceReconciler) refreshClientCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	var secret corev1.Secret
	err := r.GetClient().Get(
		ctx,
		client.ObjectKey{Namespace: r.instance.GetNamespaceName(), Name: cluster.Status.Certificates.ClientCASecret},
		&secret)
	if err != nil {
		return false, err
	}

	return r.refreshCAFromSecret(ctx, &secret, postgresSpec.ClientCACertificateLocation)
}

// refreshServerCA gets the latest server CA certificates from the secrets.
// It returns true if configuration has been changed
func (r *InstanceReconciler) refreshServerCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	var secret corev1.Secret
	err := r.GetClient().Get(
		ctx,
		client.ObjectKey{Namespace: r.instance.GetNamespaceName(), Name: cluster.Status.Certificates.ServerCASecret},
		&secret)
	if err != nil {
		return false, err
	}

	return r.refreshCAFromSecret(ctx, &secret, postgresSpec.ServerCACertificateLocation)
}

// refreshBarmanEndpointCA gets the latest barman endpoint CA certificates from the secrets.
// It returns true if configuration has been changed
func (r *InstanceReconciler) refreshBarmanEndpointCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
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
		var secret corev1.Secret
		err := r.GetClient().Get(
			ctx,
			client.ObjectKey{Namespace: r.instance.GetNamespaceName(), Name: secretKeySelector.Name},
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

// verifyPgDataCoherenceForPrimary will abort the execution if the current server is a primary
// one from the PGDATA viewpoint, but is not classified as the target nor the
// current primary
func (r *InstanceReconciler) verifyPgDataCoherenceForPrimary(ctx context.Context, cluster *apiv1.Cluster) error {
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}
	if !isPrimary {
		return nil
	}
	contextLogger := log.FromContext(ctx)

	targetPrimary := cluster.Status.TargetPrimary
	currentPrimary := cluster.Status.CurrentPrimary

	contextLogger.Info("Cluster status",
		"currentPrimary", currentPrimary,
		"targetPrimary", targetPrimary,
		"isReplicaCluster", cluster.IsReplica())

	switch {
	case cluster.IsReplica():
		// I'm an old primary, and now I'm inside a replica cluster. This can
		// only happen when a primary cluster is demoted while being hibernated.
		// Otherwise, this would have been caught by the operator, and the operator
		// would have requested a replica cluster transition.
		// In this case, we're demoting the cluster immediately.
		contextLogger.Info("Detected transition to replica cluster after reconciliation " +
			"of the cluster is resumed, demoting immediately")
		return r.instance.Demote(ctx, cluster)

	case targetPrimary == r.instance.GetPodName():
		if currentPrimary == "" {
			// This means that this cluster has been just started up and the
			// current primary still need to be written
			contextLogger.Info("First primary instance bootstrap, marking myself as primary",
				"currentPrimary", currentPrimary,
				"targetPrimary", targetPrimary)

			oldCluster := cluster.DeepCopy()
			cluster.Status.CurrentPrimary = r.instance.GetPodName()
			cluster.Status.CurrentPrimaryTimestamp = pgTime.GetCurrentTimestamp()
			return r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
		}
		return nil

	default:
		// I'm an old primary and not the current one. I need to wait for
		// the switchover procedure to finish, and then I can demote myself
		// and start following the new primary
		contextLogger.Info("This is an old primary instance, waiting for the "+
			"switchover to finish",
			"currentPrimary", currentPrimary,
			"targetPrimary", targetPrimary)

		// Wait for the switchover to be reflected in the cluster metadata
		if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
			contextLogger.Info("Switchover in progress",
				"targetPrimary", cluster.Status.TargetPrimary,
				"currentPrimary", cluster.Status.CurrentPrimary)
			return controller.ErrNextLoop
		}

		contextLogger.Info("Switchover completed",
			"targetPrimary", cluster.Status.TargetPrimary,
			"currentPrimary", cluster.Status.CurrentPrimary)

		// Wait for the new primary to really accept connections
		err := r.instance.WaitForPrimaryAvailable(ctx)
		if err != nil {
			return err
		}

		pgVersion, err := cluster.GetPostgresqlVersion()
		if err != nil {
			return err
		}

		// Clean up any stale pid file before executing pg_rewind
		err = r.instance.CleanUpStalePid()
		if err != nil {
			return err
		}

		// Set permission of postgres.auto.conf to 0600 to allow pg_rewind to write to it
		// the mode will be later reset by the reconciliation again, skip the error as
		// rewind may be not needed
		err = r.instance.SetPostgreSQLAutoConfWritable(true)
		if err != nil {
			contextLogger.Error(
				err, "Error while changing mode of the postgresql.auto.conf file before pg_rewind, skipped")
		}

		// We archive every WAL that have not been archived from the latest postmaster invocation.
		if err := archiver.ArchiveAllReadyWALs(ctx, cluster, r.instance.PgData); err != nil {
			return fmt.Errorf("while ensuring all WAL files are archived: %w", err)
		}

		// pg_rewind could require a clean shutdown of the old primary to
		// work. Unfortunately, if the old primary is already clean starting
		// it up may make it advance in respect to the new one.
		// The only way to check if we really need to start it up before
		// invoking pg_rewind is to try using pg_rewind and, on failures,
		// retrying after having started up the instance.
		err = r.instance.Rewind(ctx, pgVersion)
		if err != nil {
			contextLogger.Info(
				"pg_rewind failed, starting the server to complete the crash recovery",
				"err", err)

			// pg_rewind requires a clean shutdown of the old primary to work.
			// The only way to do that is to start the server again
			// and wait for it to be available again.
			err = r.instance.CompleteCrashRecovery(ctx)
			if err != nil {
				return err
			}

			// Then let's go back to the point of the new primary
			err = r.instance.Rewind(ctx, pgVersion)
			if err != nil {
				return err
			}
		}

		// Now I can demote myself
		return r.instance.Demote(ctx, cluster)
	}
}

// ReconcileWalStorage moves the files from PGDATA/pg_wal to the volume attached, if exists, and
// creates a symlink for it
func (r *InstanceReconciler) ReconcileWalStorage(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	if pgWalExists, err := fileutils.FileExists(specs.PgWalVolumePath); err != nil {
		return err
	} else if !pgWalExists {
		return nil
	}

	pgWalDirInfo, err := os.Lstat(specs.PgWalPath)
	if err != nil {
		return err
	}
	// The pgWalDir it's already a symlink meaning that there's nothing to do
	mode := pgWalDirInfo.Mode() & fs.ModeSymlink
	if !pgWalDirInfo.IsDir() && mode != 0 {
		return nil
	}

	// We discarded every possibility that this has been done, let's move the current file to their
	// new location
	contextLogger.Info("Moving data", "from", specs.PgWalPath, "to", specs.PgWalVolumePgWalPath)
	if err := fileutils.MoveDirectoryContent(specs.PgWalPath, specs.PgWalVolumePgWalPath); err != nil {
		contextLogger.Error(err, "Moving data", "from", specs.PgWalPath, "to",
			specs.PgWalVolumePgWalPath)
		return err
	}

	contextLogger.Debug("Deleting old path", "path", specs.PgWalPath)
	if err := fileutils.RemoveFile(specs.PgWalPath); err != nil {
		contextLogger.Error(err, "Deleting old path", "path", specs.PgWalPath)
		return err
	}

	// We moved all the files now we should create the proper symlink
	contextLogger.Debug("Creating symlink", "from", specs.PgWalPath, "to", specs.PgWalVolumePgWalPath)
	return os.Symlink(specs.PgWalVolumePgWalPath, specs.PgWalPath)
}

// ReconcileTablespaces ensures the mount points created for the tablespaces
// are there, and creates a subdirectory in each of them, which will therefore
// be owned by the `postgres` user (rather than `root` as the mount point),
// as required in order to hold PostgreSQL Tablespaces
func (r *InstanceReconciler) ReconcileTablespaces(
	ctx context.Context,
	cluster *apiv1.Cluster,
) error {
	const dataDir = "data"
	contextLogger := log.FromContext(ctx)

	if !cluster.ContainsTablespaces() {
		return nil
	}

	for _, tbsConfig := range cluster.Spec.Tablespaces {
		tbsName := tbsConfig.Name
		mountPoint := specs.MountForTablespace(tbsName)
		if tbsMount, err := fileutils.FileExists(mountPoint); err != nil {
			contextLogger.Error(err, "while checking for mountpoint", "instance",
				r.instance.GetPodName(), "tablespace", tbsName)
			return err
		} else if !tbsMount {
			contextLogger.Error(fmt.Errorf("mountpoint not found"),
				"mountpoint for tablespaces is missing",
				"instance", r.instance.GetPodName(), "tablespace", tbsName)
			continue
		}

		info, err := os.Lstat(mountPoint)
		if err != nil {
			return fmt.Errorf("while checking for tablespace mount point: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("the tablespace %s mount: %s is not a directory", tbsName, mountPoint)
		}
		err = fileutils.EnsureDirectoryExists(filepath.Join(mountPoint, dataDir))
		if err != nil {
			contextLogger.Error(err,
				"could not create data dir in tablespace mount",
				"instance", r.instance.GetPodName(), "tablespace", tbsName)
			return fmt.Errorf("while creating data dir in tablespace %s: %w", mountPoint, err)
		}
	}
	return nil
}
