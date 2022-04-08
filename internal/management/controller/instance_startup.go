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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/controllers"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	postgresSpec "github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	pkgUtils "github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// refreshServerCertificateFiles gets the latest server certificates files from the
// secrets. Returns true if configuration has been changed
func (r *InstanceReconciler) refreshServerCertificateFiles(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	contextLogger := log.FromContext(ctx)

	var secret corev1.Secret

	err := retry.OnError(retry.DefaultBackoff, func(error) bool { return true },
		func() error {
			err := r.GetClient().Get(
				ctx,
				client.ObjectKey{Namespace: r.instance.Namespace, Name: cluster.Status.Certificates.ServerTLSSecret},
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

	return r.refreshCertificateFilesFromSecret(
		ctx,
		&secret,
		postgresSpec.ServerCertificateLocation,
		postgresSpec.ServerKeyLocation)
}

// refreshReplicationUserCertificate gets the latest replication certificates from the
// secrets. Returns true if configuration has been changed
func (r *InstanceReconciler) refreshReplicationUserCertificate(ctx context.Context,
	cluster *apiv1.Cluster,
) (bool, error) {
	var secret corev1.Secret
	err := r.GetClient().Get(
		ctx,
		client.ObjectKey{Namespace: r.instance.Namespace, Name: cluster.Status.Certificates.ReplicationTLSSecret},
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
		client.ObjectKey{Namespace: r.instance.Namespace, Name: cluster.Status.Certificates.ClientCASecret},
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
		client.ObjectKey{Namespace: r.instance.Namespace, Name: cluster.Status.Certificates.ServerCASecret},
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
			client.ObjectKey{Namespace: r.instance.Namespace, Name: secretKeySelector.Name},
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
func (r *InstanceReconciler) verifyPgDataCoherenceForPrimary(
	ctx context.Context, cluster *apiv1.Cluster,
) error {
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
		"targetPrimary", targetPrimary)

	switch {
	case targetPrimary == r.instance.PodName:
		if currentPrimary == "" {
			// This means that this cluster has been just started up and the
			// current primary still need to be written
			contextLogger.Info("First primary instance bootstrap, marking myself as primary",
				"currentPrimary", currentPrimary,
				"targetPrimary", targetPrimary)

			oldCluster := cluster.DeepCopy()
			cluster.Status.CurrentPrimary = r.instance.PodName
			cluster.Status.CurrentPrimaryTimestamp = pkgUtils.GetCurrentTimestamp()
			return r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
		}
		return nil

	default:
		// I'm an old primary and not the current one. I need to wait for
		// the switchover procedure to finish and then I can demote myself
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
			return controllers.ErrNextLoop
		}

		contextLogger.Info("Switchover completed",
			"targetPrimary", cluster.Status.TargetPrimary,
			"currentPrimary", cluster.Status.CurrentPrimary)

		// Wait for the new primary to really accept connections
		err := r.instance.WaitForPrimaryAvailable()
		if err != nil {
			return err
		}

		tag := pkgUtils.GetImageTag(cluster.GetImageName())
		pgMajorVersion, err := postgresSpec.GetPostgresMajorVersionFromTag(tag)
		if err != nil {
			return err
		}

		// Clean up any stale pid file before executing pg_rewind
		err = r.instance.CleanUpStalePid()
		if err != nil {
			return err
		}

		// pg_rewind could require a clean shutdown of the old primary to
		// work. Unfortunately, if the old primary is already clean starting
		// it up may make it advance in respect to the new one.
		// The only way to check if we really need to start it up before
		// invoking pg_rewind is to try using pg_rewind and, on failures,
		// retrying after having started up the instance.
		err = r.instance.Rewind(pgMajorVersion)
		if err != nil {
			contextLogger.Info(
				"pg_rewind failed, starting the server to complete the crash recovery",
				"err", err)

			// pg_rewind requires a clean shutdown of the old primary to work.
			// The only way to do that is to start the server again
			// and wait for it to be available again.
			err = r.instance.CompleteCrashRecovery()
			if err != nil {
				return err
			}

			// Then let's go back to the point of the new primary
			err = r.instance.Rewind(pgMajorVersion)
			if err != nil {
				return err
			}
		}

		// Now I can demote myself
		return r.instance.Demote()
	}
}
