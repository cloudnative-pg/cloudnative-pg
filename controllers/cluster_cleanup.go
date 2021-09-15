/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// cleanupCluster remove all the Jobs which are completed
func (r *ClusterReconciler) cleanupCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	jobs batchv1.JobList) error {
	contextLogger := log.FromContext(ctx)

	completeJobs := utils.FilterCompleteJobs(jobs.Items)
	if len(completeJobs) == 0 {
		return nil
	}

	for i, job := range completeJobs {
		contextLogger.Debug("Removing job", "job", job.Name)

		foreground := metav1.DeletePropagationForeground
		if err := r.Delete(ctx, &completeJobs[i], &client.DeleteOptions{
			PropagationPolicy: &foreground,
		}); err != nil {
			contextLogger.Error(err, "cannot delete job", "job", job.Name)
			continue
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
