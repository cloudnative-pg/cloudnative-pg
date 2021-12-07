/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// cleanupCompletedJobs remove all the Jobs which are completed
func (r *ClusterReconciler) cleanupCompletedJobs(
	ctx context.Context,
	jobs batchv1.JobList) {
	contextLogger := log.FromContext(ctx)

	completeJobs := utils.FilterCompleteJobs(jobs.Items)
	if len(completeJobs) == 0 {
		return
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
}
