/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apiv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	postgresSpec "github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// RefreshServerCertificateFiles get the latest certificates from the
// secrets
func (r *InstanceReconciler) RefreshServerCertificateFiles() error {
	unstructuredObject, err := r.client.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}).
		Namespace(r.instance.Namespace).
		Get(r.instance.ClusterName+apiv1alpha1.ServerSecretSuffix, metav1.GetOptions{})
	if err != nil {
		return err
	}

	return r.refreshCertificateFilesFromObject(
		unstructuredObject,
		postgresSpec.ServerCertificateLocation,
		postgresSpec.ServerKeyLocation)
}

// RefreshPostgresUserCertificate get the latest certificates from the
// secrets
func (r *InstanceReconciler) RefreshPostgresUserCertificate() error {
	unstructuredObject, err := r.client.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}).
		Namespace(r.instance.Namespace).
		Get(r.instance.ClusterName+apiv1alpha1.ReplicationSecretSuffix, metav1.GetOptions{})
	if err != nil {
		return err
	}

	return r.refreshCertificateFilesFromObject(
		unstructuredObject,
		postgresSpec.StreamingReplicaCertificateLocation,
		postgresSpec.StreamingReplicaKeyLocation)
}

// RefreshCA get the latest certificates from the
// secrets
func (r *InstanceReconciler) RefreshCA() error {
	unstructuredObject, err := r.client.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}).
		Namespace(r.instance.Namespace).
		Get(r.instance.ClusterName+apiv1alpha1.CaSecretSuffix, metav1.GetOptions{})
	if err != nil {
		return err
	}

	return r.refreshCAFromObject(unstructuredObject)
}

// VerifyPgDataCoherence check if this cluster exist in k8s and panic if this
// pod belongs to a primary but the cluster status is not coherent with that
func (r *InstanceReconciler) VerifyPgDataCoherence(ctx context.Context) error {
	r.log.Info("Checking PGDATA coherence")

	cluster, err := r.client.
		Resource(apiv1alpha1.ClusterGVK).
		Namespace(r.instance.Namespace).
		Get(r.instance.ClusterName, metav1.GetOptions{})
	if err != nil {
		return err
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
		return r.verifyPgDataCoherenceForPrimary(cluster)
	}

	return nil
}

// This function will abort the execution if the current server is a primary
// one from the PGDATA viewpoint, but is not classified as the target nor the
// current primary
func (r *InstanceReconciler) verifyPgDataCoherenceForPrimary(cluster *unstructured.Unstructured) error {
	targetPrimary, err := utils.GetTargetPrimary(cluster)
	if err != nil {
		return err
	}

	// When a new cluster has just started, we have still
	// no current primary. It's not a real issue, we just need
	// to put our name there
	currentPrimary, err := utils.GetCurrentPrimary(cluster)
	if err != nil && err != utils.ErrCurrentPrimaryNotFound {
		return err
	}

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
			err = utils.SetCurrentPrimary(cluster, r.instance.PodName)
			if err != nil {
				return err
			}

			_, err = r.client.
				Resource(apiv1alpha1.ClusterGVK).
				Namespace(r.instance.Namespace).
				UpdateStatus(cluster, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
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

		err = r.waitForSwitchoverToBeCompleted()
		if err != nil {
			return err
		}

		// Then let's go back to the point of the new master
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
func (r *InstanceReconciler) waitForSwitchoverToBeCompleted() error {
	switchoverWatch, err := r.client.
		Resource(apiv1alpha1.ClusterGVK).
		Namespace(r.instance.Namespace).
		Watch(metav1.ListOptions{
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

		object, err := objectToUnstructured(event.Object)
		if err != nil {
			return fmt.Errorf("error while decoding runtime.Object data from watch: %w", err)
		}

		targetPrimary, err := utils.GetTargetPrimary(object)
		if err != nil {
			return fmt.Errorf("error while extracting the targetPrimary from the cluster status %v: %w",
				object, err)
		}
		currentPrimary, err := utils.GetCurrentPrimary(object)
		if err != nil {
			return fmt.Errorf("error while extracting the currentPrimary from the cluster status %v: %w",
				object, err)
		}

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
