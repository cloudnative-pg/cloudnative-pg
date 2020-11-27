/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/expectations"
)

// scaleDownCluster handles the scaling down operations of a PostgreSQL cluster.
// the scale up operation is handled by the instances creation code
func (r *ClusterReconciler) scaleDownCluster(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	childPods v1.PodList,
) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	// Is there one pod to be deleted?
	sacrificialPod := getSacrificialPod(childPods.Items)
	if sacrificialPod == nil {
		log.Info("There are no instances to be sacrificed. Wait for the next sync loop")
		return nil
	}

	r.Recorder.Eventf(cluster, "Normal", "DeletingInstance",
		"Scaling down: removing instance %v", sacrificialPod.Name)

	// Retrieve the cluster key
	key := expectations.KeyFunc(cluster)

	// We expect the deletion of the selected Pod
	if err := r.podExpectations.ExpectDeletions(key, 1); err != nil {
		log.Error(err, "Unable to set podExpectations", "key", key, "dels", 1)
	}

	log.Info("Too many nodes for cluster, deleting an instance",
		"pod", sacrificialPod.Name)
	if err := r.Delete(ctx, sacrificialPod); err != nil {
		// We cannot observe a deletion if it was not accepted by the server
		r.podExpectations.DeletionObserved(key)

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

	// We expect the deletion of the selected PVC
	if err := r.pvcExpectations.ExpectDeletions(key, 1); err != nil {
		log.Error(err, "Unable to set pvcExpectations", "key", key, "dels", 1)
	}

	err := r.Delete(ctx, &pvc)
	if err != nil {
		// We cannot observe a deletion if it was not accepted by the server
		r.pvcExpectations.DeletionObserved(key)

		// Ignore if NotFound, otherwise report the error
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("scaling down node (pvc) %v: %v", sacrificialPod.Name, err)
		}
	}

	return nil
}
