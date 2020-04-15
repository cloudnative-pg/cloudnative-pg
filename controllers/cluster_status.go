/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/specs"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/utils"
)

func (r *ClusterReconciler) getManagedPods(
	ctx context.Context,
	cluster v1alpha1.Cluster,
) (corev1.PodList, error) {
	var childPods corev1.PodList
	if err := r.List(ctx, &childPods,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{podOwnerKey: cluster.Name},
	); err != nil {
		r.Log.Error(err, "Unable to list child pods resource",
			"namespace", cluster.Namespace, "name", cluster.Name)
		return corev1.PodList{}, err
	}

	return childPods, nil
}

func (r *ClusterReconciler) getManagedPVCs(
	ctx context.Context,
	cluster v1alpha1.Cluster,
) (corev1.PersistentVolumeClaimList, error) {
	var childPVCs corev1.PersistentVolumeClaimList
	if err := r.List(ctx, &childPVCs,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{pvcOwnerKey: cluster.Name},
	); err != nil {
		r.Log.Error(err, "Unable to list child PVCs",
			"namespace", cluster.Namespace, "name", cluster.Name)
		return corev1.PersistentVolumeClaimList{}, err
	}

	return childPVCs, nil
}

func (r *ClusterReconciler) updateResourceStatus(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	childPods corev1.PodList,
	childPVCs corev1.PersistentVolumeClaimList,
) error {
	// From now on, we'll consider only Active pods: those Pods
	// that will possibly work. Let's forget about the failed ones
	filteredPods := utils.FilterActivePods(childPods.Items)

	// Fill the list of dangling PVCs
	if cluster.IsUsingPersistentStorage() {
		cluster.Status.DanglingPVC = specs.DetectDanglingPVCs(filteredPods, childPVCs.Items)
	}

	// Count pods
	cluster.Status.Instances = int32(len(filteredPods))
	cluster.Status.ReadyInstances = int32(utils.CountReadyPods(filteredPods))

	return r.Status().Update(ctx, cluster)
}

func (r *ClusterReconciler) setPrimaryInstance(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	podName string,
) error {
	cluster.Status.TargetPrimary = podName
	if err := r.Status().Update(ctx, cluster); err != nil {
		return err
	}

	return nil
}
