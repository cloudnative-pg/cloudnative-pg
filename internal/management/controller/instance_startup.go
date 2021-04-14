/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	postgresSpec "github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// RefreshServerCertificateFiles get the latest certificates from the
// secrets. Returns true if configuration has been changed
func (r *InstanceReconciler) RefreshServerCertificateFiles(ctx context.Context) (bool, error) {
	unstructuredObject, err := r.client.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}).
		Namespace(r.instance.Namespace).
		Get(ctx, r.instance.ClusterName+apiv1.ServerSecretSuffix, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	secret, err := utils.ObjectToSecret(unstructuredObject)
	if err != nil {
		return false, err
	}

	return r.refreshCertificateFilesFromSecret(
		secret,
		postgresSpec.ServerCertificateLocation,
		postgresSpec.ServerKeyLocation)
}

// RefreshReplicationUserCertificate get the latest certificates from the
// secrets. Returns true if configuration has been changed
func (r *InstanceReconciler) RefreshReplicationUserCertificate(ctx context.Context) (bool, error) {
	unstructuredObject, err := r.client.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}).
		Namespace(r.instance.Namespace).
		Get(ctx, r.instance.ClusterName+apiv1.ReplicationSecretSuffix, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	secret, err := utils.ObjectToSecret(unstructuredObject)
	if err != nil {
		return false, err
	}

	return r.refreshCertificateFilesFromSecret(
		secret,
		postgresSpec.StreamingReplicaCertificateLocation,
		postgresSpec.StreamingReplicaKeyLocation)
}

// RefreshCA get the latest certificates from the
// secrets. Returns true if configuration has been changed
func (r *InstanceReconciler) RefreshCA(ctx context.Context) (bool, error) {
	unstructuredObject, err := r.client.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}).
		Namespace(r.instance.Namespace).
		Get(ctx, r.instance.ClusterName+apiv1.CaSecretSuffix, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	secret, err := utils.ObjectToSecret(unstructuredObject)
	if err != nil {
		return false, err
	}

	return r.refreshCAFromSecret(secret)
}

// VerifyPgDataCoherence check if this cluster exist in k8s and panic if this
// pod belongs to a primary but the cluster status is not coherent with that
func (r *InstanceReconciler) VerifyPgDataCoherence(ctx context.Context) error {
	r.log.Info("Checking PGDATA coherence")

	cluster, err := utils.GetCluster(ctx, r.client, r.instance.Namespace, r.instance.ClusterName)
	if err != nil {
		return fmt.Errorf("error while decoding runtime.Object data from watch: %w", err)
	}

	if err := fileutils.EnsurePgDataPerms(r.instance.PgData); err != nil {
		return err
	}

	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}

	r.log.Info("Instance type", "isPrimary", isPrimary)

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

		// Now I can demote myself
		return r.instance.Demote()
	}
}

// waitForSwitchoverToBeCompleted is supposed to be called when `targetPrimary`
// is different from `currentPrimary`, meaning that a switchover procedure is in
// progress. The function will create a watch on the cluster resource and wait
// until the switchover is completed
func (r *InstanceReconciler) waitForSwitchoverToBeCompleted(ctx context.Context) error {
	switchoverWatch, err := r.client.
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
