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

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
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

	// Is there is an instance to be deleted?
	sacrificialInstanceName := getSacrificialInstanceName(cluster, resources.instances.Items)
	if sacrificialInstanceName == "" {
		contextLogger.Info("There are no instances to be sacrificed. Wait for the next sync loop")
		return nil
	}

	r.Recorder.Eventf(cluster, "Normal", "ScaleDown",
		"Scaling down: removing instance %v", sacrificialInstanceName)

	if err := r.deleteInstance(ctx, cluster, sacrificialInstanceName); err != nil {
		return err
	}

	// Let's drop the PVCs too
	if err := persistentvolumeclaim.DeleteInstancePVCs(
		ctx,
		r.Client,
		cluster,
		sacrificialInstanceName,
		cluster.Namespace,
	); err != nil {
		return err
	}
	// And now also the Job
	for idx := range resources.jobs.Items {
		if strings.HasPrefix(resources.jobs.Items[idx].Name, sacrificialInstanceName+"-") {
			// This job was working against the PVC of this Pod,
			// let's remove it
			foreground := metav1.DeletePropagationForeground
			if err := r.Delete(
				ctx,
				&resources.jobs.Items[idx],
				&client.DeleteOptions{
					PropagationPolicy: &foreground,
				},
			); err != nil {
				// Ignore if NotFound, otherwise report the error
				if !apierrs.IsNotFound(err) {
					return fmt.Errorf("scaling down node (job) %v: %w", sacrificialInstanceName, err)
				}
			}
		}
	}

	return nil
}

func (r *ClusterReconciler) deleteInstance(
	ctx context.Context,
	cluster *apiv1.Cluster,
	sacrificialInstanceName string,
) error {
	contextLogger := log.FromContext(ctx)

	nominatedInstance := &corev1.Pod{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      sacrificialInstanceName,
		Namespace: cluster.Namespace,
	}, nominatedInstance)

	if apierrs.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	contextLogger.Info("Too many nodes for cluster, deleting an instance",
		"pod", nominatedInstance.Name)
	err = r.Delete(ctx, nominatedInstance)
	if apierrs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot kill the Pod to scale down: %w", err)
	}

	return nil
}
