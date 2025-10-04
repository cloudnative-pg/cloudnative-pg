/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// scaleDownCluster handles the scaling down operations of a PostgreSQL cluster.
// the scale up operation is handled by the instances creation code
func (r *ClusterReconciler) scaleDownCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) error {
	contextLogger := log.FromContext(ctx)

	if cluster.Spec.MaxSyncReplicas > 0 && cluster.Spec.Instances < (cluster.Spec.MaxSyncReplicas+1) {
		origCluster := cluster.DeepCopy()
		cluster.Spec.Instances = cluster.Status.Instances
		if err := r.Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
			return err
		}

		r.Recorder.Eventf(cluster, "Warning", "NoScaleDown",
			"Can't scale down lower than maxSyncReplicas, going back to %v",
			cluster.Spec.Instances)

		return nil
	}

	// Is there is an instance to be deleted?
	instanceName := findDeletableInstance(cluster, resources.instances.Items)
	if instanceName == "" {
		contextLogger.Info("There are no instances to be sacrificed. Wait for the next sync loop")
		return nil
	}

	message := fmt.Sprintf("Scaling down - removing instance: %v", instanceName)
	r.Recorder.Event(cluster, "Normal", "ScaleDown", message)
	contextLogger.Info(message)

	return r.ensureInstanceIsDeleted(ctx, cluster, instanceName)
}

func (r *ClusterReconciler) ensureInstanceIsDeleted(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instanceName string,
) error {
	if err := persistentvolumeclaim.EnsureInstancePVCGroupIsDeleted(
		ctx,
		r.Client,
		cluster,
		instanceName,
		cluster.Namespace,
	); err != nil {
		return err
	}

	if err := r.ensureInstancePodIsDeleted(ctx, cluster, instanceName); err != nil {
		return err
	}

	return r.ensureInstanceJobAreDeleted(ctx, cluster, instanceName)
}

func (r *ClusterReconciler) ensureInstanceJobAreDeleted(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instanceName string,
) error {
	contextLogger := log.FromContext(ctx)

	var jobList batchv1.JobList
	if err := r.List(
		ctx,
		&jobList,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{jobOwnerKey: cluster.Name},
		client.MatchingLabels{
			utils.InstanceNameLabelName: instanceName,
			utils.ClusterLabelName:      cluster.Name,
		},
		client.HasLabels{
			utils.JobRoleLabelName,
		},
	); err != nil {
		return fmt.Errorf("while looking for stale jobs of instance %s: %w", instanceName, err)
	}

	for i := range jobList.Items {
		job := &jobList.Items[i]

		// This job was working against the PVC of this Pod,
		// let's remove it
		foreground := metav1.DeletePropagationForeground
		if err := r.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &foreground}); err != nil {
			// Ignore if NotFound, otherwise report the error
			if !apierrs.IsNotFound(err) {
				return fmt.Errorf("scaling down node (job) %v: %w", instanceName, err)
			}
			contextLogger.Info("Deleted job", "jobName", job.Name)
		}
	}
	return nil
}

func (r *ClusterReconciler) ensureInstancePodIsDeleted(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instanceName string,
) error {
	contextLogger := log.FromContext(ctx)

	nominatedInstance := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: cluster.Namespace,
		},
	}

	contextLogger.Info("ensuring an instance is deleted", "pod", nominatedInstance.Name)
	err := r.Delete(ctx, nominatedInstance)
	if apierrs.IsNotFound(err) || err == nil {
		return nil
	}
	return fmt.Errorf("cannot delete the instance: %w", err)
}
