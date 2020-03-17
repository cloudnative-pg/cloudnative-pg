/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"

	v1 "k8s.io/api/core/v1"

	"github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
)

// scaleDownCluster handles the scaling down operations of a PostgreSQL cluster.
// the scale up operation is handled by the instances creation code
func (r *ClusterReconciler) scaleDownCluster(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	childPods v1.PodList,
) error {
	// Is there one pod to be deleted?
	sacrificialPod := getSacrificialPod(childPods.Items)
	if sacrificialPod == nil {
		r.Log.Info("There are no instances to be sacrificed. Wait for the next sync loop")
		return nil
	}

	r.Log.Info("Too many nodes for cluster, deleting an instance",
		"cluster", cluster.Name,
		"namespace", cluster.Namespace,
		"pod", sacrificialPod.Name)
	err := r.Delete(ctx, sacrificialPod)
	if err != nil {
		r.Log.Error(err, "Cannot kill the Pod to scale down",
			"clusterName", cluster.Name,
			"namespace", cluster.Namespace,
			"pod", sacrificialPod.Name)
		return err
	}

	return nil
}
