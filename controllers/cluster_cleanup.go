/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/expectations"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// cleanupCluster remove all the Jobs which are completed
func (r *ClusterReconciler) cleanupCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	jobs batchv1.JobList) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	// Retrieve the cluster key
	key := expectations.KeyFunc(cluster)

	completeJobs := utils.FilterCompleteJobs(jobs.Items)
	if len(completeJobs) == 0 {
		return nil
	}

	if err := r.jobExpectations.ExpectDeletions(key, 0); err != nil {
		log.Error(err, "Unable to initialize jobExpectations", "key", key, "dels", 0)
	}

	for idx := range completeJobs {
		log.V(2).Info("Removing job", "job", jobs.Items[idx].Name)

		// We expect the deletion of the selected Job
		r.jobExpectations.RaiseExpectations(key, 0, 1)

		foreground := metav1.DeletePropagationForeground
		if err := r.Delete(ctx, &jobs.Items[idx], &client.DeleteOptions{
			PropagationPolicy: &foreground,
		}); err != nil {
			// We cannot observe a deletion if it was not accepted by the server
			r.jobExpectations.DeletionObserved(key)

			return fmt.Errorf("cannot delete job: %w", err)
		}
	}

	// Let's remove the stale ConfigMap if we have it
	var configMap corev1.ConfigMap
	err := r.Get(
		ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, &configMap)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return err
	}

	return r.Delete(ctx, &configMap)
}
