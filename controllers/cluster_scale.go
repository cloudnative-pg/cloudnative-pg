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

package controllers

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
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
		cluster.Spec.Instances = cluster.Status.Instances
		if err := r.Update(ctx, cluster); err != nil {
			return err
		}

		r.Recorder.Eventf(cluster, "Warning", "NoScaleDown",
			"Can't scale down lower than maxSyncReplicas, going back to %v",
			cluster.Spec.Instances)

		return nil
	}

	// Is there one pod to be deleted?
	sacrificialInstance := getSacrificialInstance(resources.instances.Items)
	if sacrificialInstance == nil {
		contextLogger.Info("There are no instances to be sacrificed. Wait for the next sync loop")
		return nil
	}

	r.Recorder.Eventf(cluster, "Normal", "ScaleDown",
		"Scaling down: removing instance %v", sacrificialInstance.Name)

	contextLogger.Info("Too many nodes for cluster, deleting an instance",
		"pod", sacrificialInstance.Name)
	if err := r.Delete(ctx, sacrificialInstance); err != nil {
		// Ignore if NotFound, otherwise report the error
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("cannot kill the Pod to scale down: %w", err)
		}
	}

	// Let's drop the PVC too
	pvc := v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sacrificialInstance.Name,
			Namespace: sacrificialInstance.Namespace,
		},
	}

	err := r.Delete(ctx, &pvc)
	if err != nil {
		// Ignore if NotFound, otherwise report the error
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("scaling down node (pvc) %v: %v", sacrificialInstance.Name, err)
		}
	}

	// And now also the Job
	for idx := range resources.jobs.Items {
		if strings.HasPrefix(resources.jobs.Items[idx].Name, sacrificialInstance.Name+"-") {
			// This job was working against the PVC of this Pod,
			// let's remove it
			foreground := metav1.DeletePropagationForeground
			err = r.Delete(
				ctx,
				&resources.jobs.Items[idx],
				&client.DeleteOptions{
					PropagationPolicy: &foreground,
				},
			)
			if err != nil {
				// Ignore if NotFound, otherwise report the error
				if !apierrs.IsNotFound(err) {
					return fmt.Errorf("scaling down node (job) %v: %v", sacrificialInstance.Name, err)
				}
			}
		}
	}

	return nil
}
