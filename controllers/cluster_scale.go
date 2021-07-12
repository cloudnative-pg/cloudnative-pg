/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
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

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// scaleDownCluster handles the scaling down operations of a PostgreSQL cluster.
// the scale up operation is handled by the instances creation code
func (r *ClusterReconciler) scaleDownCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

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
	sacrificialPod := getSacrificialPod(resources.pods.Items)
	if sacrificialPod == nil {
		log.Info("There are no instances to be sacrificed. Wait for the next sync loop")
		return nil
	}

	r.Recorder.Eventf(cluster, "Normal", "ScaleDown",
		"Scaling down: removing instance %v", sacrificialPod.Name)

	log.Info("Too many nodes for cluster, deleting an instance",
		"pod", sacrificialPod.Name)
	if err := r.Delete(ctx, sacrificialPod); err != nil {
		// We cannot observe a deletion if it was not accepted by the server

		// Ignore if NotFound, otherwise report the error
		if !apierrs.IsNotFound(err) {
			log.Error(err, "Cannot kill the Pod to scale down",
				"pod", sacrificialPod.Name)
			return err
		}
	}

	// Let's drop the PVC too
	pvc := v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sacrificialPod.Name,
			Namespace: sacrificialPod.Namespace,
		},
	}

	err := r.Delete(ctx, &pvc)
	if err != nil {
		// We cannot observe a deletion if it was not accepted by the server

		// Ignore if NotFound, otherwise report the error
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("scaling down node (pvc) %v: %v", sacrificialPod.Name, err)
		}
	}

	// And now also the Job
	for idx := range resources.jobs.Items {
		if strings.HasPrefix(resources.jobs.Items[idx].Name, sacrificialPod.Name+"-") {
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
					return fmt.Errorf("scaling down node (job) %v: %v", sacrificialPod.Name, err)
				}
			}
		}
	}

	return nil
}
