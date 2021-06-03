/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	postgresSpec "github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// RefreshServerCertificateFiles gets the latest server certificates files from the
// secrets. Returns true if configuration has been changed
func (r *InstanceReconciler) RefreshServerCertificateFiles(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	var secret corev1.Secret

	err := retry.OnError(retry.DefaultBackoff, func(error) bool { return true },
		func() error {
			err := r.GetClient().Get(
				ctx,
				client.ObjectKey{Namespace: r.instance.Namespace, Name: cluster.Status.Certificates.ServerTLSSecret},
				&secret)
			if err != nil {
				r.log.Info("Error accessing server TLS Certificate. Retrying with exponential backoff.",
					"secret", cluster.Status.Certificates.ServerTLSSecret)
				return err
			}
			return nil
		})
	if err != nil {
		return false, err
	}

	return r.refreshCertificateFilesFromSecret(
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

	return r.refreshCAFromSecret(&secret, postgresSpec.ClientCACertificateLocation)
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

	return r.refreshCAFromSecret(&secret, postgresSpec.ServerCACertificateLocation)
}

// VerifyPgDataCoherence checks if this cluster exists in K8s. It panics if this
// pod belongs to a primary but the cluster status is not coherent with that
func (r *InstanceReconciler) VerifyPgDataCoherence(ctx context.Context) error {
	r.log.Info("Checking PGDATA coherence")

	var cluster apiv1.Cluster
	err := r.GetClient().Get(
		ctx,
		client.ObjectKey{Namespace: r.instance.Namespace, Name: r.instance.ClusterName},
		&cluster)
	if err != nil {
		return fmt.Errorf("error while decoding runtime.Object data from watch: %w", err)
	}

	if err := fileutils.EnsurePgDataPerms(r.instance.PgData); err != nil {
		return err
	}

	// Delete any stale Postgres PID file
	if err := r.instance.CleanupStalePidFile(); err != nil {
		return err
	}

	if err := postgres.WritePostgresUserMaps(r.instance.PgData); err != nil {
		return err
	}

	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}

	r.log.Info("Instance type", "isPrimary", isPrimary)

	if isPrimary {
		return r.verifyPgDataCoherenceForPrimary(ctx, &cluster)
	}

	return nil
}

// This function will abort the execution if the current server is a primary
// one from the PGDATA viewpoint, but is not classified as the target nor the
// current primary
func (r *InstanceReconciler) verifyPgDataCoherenceForPrimary(
	ctx context.Context, cluster *apiv1.Cluster) error {
	targetPrimary := cluster.Status.TargetPrimary
	currentPrimary := cluster.Status.CurrentPrimary

	r.log.Info("Cluster status",
		"currentPrimary", currentPrimary,
		"targetPrimary", targetPrimary)

	switch {
	case targetPrimary == r.instance.PodName:
		if currentPrimary == "" {
			// This means that this cluster has been just started up and the
			// current primary still need to be written
			r.log.Info("First primary instance bootstrap, marking myself as primary",
				"currentPrimary", currentPrimary,
				"targetPrimary", targetPrimary)

			_, err := utils.UpdateClusterStatusAndRetry(
				ctx, r.client, cluster, func(cluster *apiv1.Cluster) error {
					cluster.Status.CurrentPrimary = r.instance.PodName
					return nil
				})

			return err
		}
		return nil

	default:
		// I'm an old primary and not the current one. I need to wait for
		// the switchover procedure to finish and then I can demote myself
		// and start following the new primary
		r.log.Info("This is an old primary instance, waiting for the "+
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

		// pg_rewind could require a clean shutdown of the old primary to
		// work. Unfortunately, if the old primary is already clean starting
		// it up may make it advance in respect to the new one.
		// The only way to check if we really need to start it up before
		// invoking pg_rewind is to try using pg_rewind and, on failures,
		// retrying after having started up the instance.
		err = r.instance.Rewind()
		if err != nil {
			r.log.Info(
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
			err = r.instance.Rewind()
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
	switchoverWatch, err := r.dynamicClient.
		Resource(apiv1.ClusterGVK).
		Namespace(r.instance.Namespace).
		Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("metadata.name", r.instance.ClusterName).String(),
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

		cluster, err := utils.ObjectToCluster(event.Object)
		if err != nil {
			return fmt.Errorf("error while decoding runtime.Object data from watch: %w", err)
		}

		targetPrimary := cluster.Status.TargetPrimary
		currentPrimary := cluster.Status.CurrentPrimary

		if targetPrimary == currentPrimary {
			r.log.Info("Switchover completed",
				"targetPrimary", targetPrimary,
				"currentPrimary", currentPrimary)
			switchoverWatch.Stop()
			break
		} else {
			r.log.Info("Switchover in progress",
				"targetPrimary", targetPrimary,
				"currentPrimary", currentPrimary)
		}
	}

	return nil
}
