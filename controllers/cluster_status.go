/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/specs"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
)

func (r *ClusterReconciler) getManagedPods(
	ctx context.Context,
	cluster v1alpha1.Cluster,
) (corev1.PodList, error) {
	log := r.Log.WithName("cloud-native-postgresql").WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	var childPods corev1.PodList
	if err := r.List(ctx, &childPods,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{podOwnerKey: cluster.Name},
	); err != nil {
		log.Error(err, "Unable to list child pods resource")
		return corev1.PodList{}, err
	}

	return childPods, nil
}

func (r *ClusterReconciler) getManagedPVCs(
	ctx context.Context,
	cluster v1alpha1.Cluster,
) (corev1.PersistentVolumeClaimList, error) {
	log := r.Log.WithName("cloud-native-postgresql").WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	var childPVCs corev1.PersistentVolumeClaimList
	if err := r.List(ctx, &childPVCs,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{pvcOwnerKey: cluster.Name},
	); err != nil {
		log.Error(err, "Unable to list child PVCs")
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
	existingClusterStatus := cluster.Status

	// From now on, we'll consider only Active pods: those Pods
	// that will possibly work. Let's forget about the failed ones
	filteredPods := utils.FilterActivePods(childPods.Items)

	// Fill the list of dangling PVCs
	cluster.Status.DanglingPVC = specs.DetectDanglingPVCs(filteredPods, childPVCs.Items)

	// Count pods
	cluster.Status.Instances = int32(len(filteredPods))
	cluster.Status.ReadyInstances = int32(utils.CountReadyPods(filteredPods))

	// Instances status
	cluster.Status.InstancesStatus = utils.ListStatusPods(childPods.Items)

	// Services
	cluster.Status.WriteService = cluster.GetServiceReadWriteName()
	cluster.Status.ReadService = cluster.GetServiceReadName()

	if !reflect.DeepEqual(existingClusterStatus, cluster.Status) {
		return r.Status().Update(ctx, cluster)
	}
	return nil
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

	if err := r.RegisterPhase(ctx, cluster, v1alpha1.PhaseSwitchover,
		fmt.Sprintf("Switching over to %v", podName)); err != nil {
		return err
	}

	return nil
}

// RegisterPhase update phase in the status cluster with the
// proper reason
func (r *ClusterReconciler) RegisterPhase(ctx context.Context,
	cluster *v1alpha1.Cluster,
	phase string,
	reason string,
) error {
	existingClusterStatus := cluster.Status

	cluster.Status.Phase = phase
	cluster.Status.PhaseReason = reason

	if !reflect.DeepEqual(existingClusterStatus, cluster.Status) {
		if err := r.Status().Update(ctx, cluster); err != nil {
			return err
		}
	}

	return nil
}
