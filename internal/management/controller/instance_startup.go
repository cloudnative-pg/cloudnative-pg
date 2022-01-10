/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	postgresSpec "github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	pkgUtils "github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// RefreshServerCertificateFiles gets the latest server certificates files from the
// secrets. Returns true if configuration has been changed
func (r *InstanceReconciler) RefreshServerCertificateFiles(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
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

// RefreshReplicationUserCertificate gets the latest replication certificates from the
// secrets. Returns true if configuration has been changed
func (r *InstanceReconciler) RefreshReplicationUserCertificate(ctx context.Context,
	cluster *apiv1.Cluster) (bool, error) {
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

// RefreshClientCA gets the latest client CA certificates from the secrets.
// It returns true if configuration has been changed
func (r *InstanceReconciler) RefreshClientCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
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

// RefreshServerCA gets the latest server CA certificates from the secrets.
// It returns true if configuration has been changed
func (r *InstanceReconciler) RefreshServerCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
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

// RefreshBarmanEndpointCA gets the latest barman endpoint CA certificates from the secrets.
// It returns true if configuration has been changed
func (r *InstanceReconciler) RefreshBarmanEndpointCA(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
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

// VerifyPgDataCoherence checks if this cluster exists in K8s. It panics if this
// pod belongs to a primary but the cluster status is not coherent with that
func (r *InstanceReconciler) VerifyPgDataCoherence(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	contextLogger.Info("Checking PGDATA coherence")

	if err := fileutils.EnsurePgDataPerms(r.instance.PgData); err != nil {
		return err
	}

	if err := postgres.WritePostgresUserMaps(r.instance.PgData); err != nil {
		return err
	}

	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}

	contextLogger.Info("Instance type", "isPrimary", isPrimary)

	if isPrimary {
		return r.verifyPgDataCoherenceForPrimary(ctx, cluster)
	}

	return nil
}

// This function will abort the execution if the current server is a primary
// one from the PGDATA viewpoint, but is not classified as the target nor the
// current primary
func (r *InstanceReconciler) verifyPgDataCoherenceForPrimary(
	ctx context.Context, cluster *apiv1.Cluster) error {
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
		err := r.waitForSwitchoverToBeCompleted(ctx)
		if err != nil {
			return err
		}

		// Wait for the new primary to really accept connections
		err = r.instance.WaitForPrimaryAvailable()
		if err != nil {
			return err
		}

		tag := pkgUtils.GetImageTag(cluster.GetImageName())
		pgMajorVersion, err := postgresSpec.GetPostgresMajorVersionFromTag(tag)
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

// waitForSwitchoverToBeCompleted is supposed to be called when `targetPrimary`
// is different from `currentPrimary`, meaning that a switchover procedure is in
// progress. The function will create a watch on the cluster resource and wait
// until the switchover is completed
func (r *InstanceReconciler) waitForSwitchoverToBeCompleted(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	switchoverWatch, err := r.client.Watch(ctx, &apiv1.ClusterList{}, &client.ListOptions{
		Namespace:     r.instance.Namespace,
		FieldSelector: fields.OneTermEqualSelector("metadata.name", r.instance.ClusterName),
	})
	if err != nil {
		return err
	}

	channel := switchoverWatch.ResultChan()
	for {
		// TODO Retry with exponential back-off

		event, ok := <-channel
		if !ok {
			return fmt.Errorf("watch expired while waiting for switchover to complete")
		}

		var cluster *apiv1.Cluster
		cluster, ok = event.Object.(*apiv1.Cluster)
		if !ok {
			return fmt.Errorf("error while decoding runtime.Object data from watch")
		}

		targetPrimary := cluster.Status.TargetPrimary
		currentPrimary := cluster.Status.CurrentPrimary

		if targetPrimary == currentPrimary {
			contextLogger.Info("Switchover completed",
				"targetPrimary", targetPrimary,
				"currentPrimary", currentPrimary)
			switchoverWatch.Stop()
			break
		} else {
			contextLogger.Info("Switchover in progress",
				"targetPrimary", targetPrimary,
				"currentPrimary", currentPrimary)
		}
	}

	return nil
}
