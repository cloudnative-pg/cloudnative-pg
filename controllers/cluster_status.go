/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"fmt"
	"reflect"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/expectations"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// managedResources contains the resources that are created a cluster
// and need to be managed by the controller
type managedResources struct {
	pods corev1.PodList
	pvcs corev1.PersistentVolumeClaimList
	jobs batchv1.JobList
}

// Count the number of jobs that are still running
func (resources managedResources) countRunningJobs() int {
	jobCount := len(resources.jobs.Items)
	completeJobs := utils.CountCompleteJobs(resources.jobs.Items)
	return jobCount - completeJobs
}

// getManagedResources get the managed resources of various types
func (r *ClusterReconciler) getManagedResources(ctx context.Context,
	cluster v1alpha1.Cluster) (*managedResources, error) {
	// Update the status of this resource
	childPods, err := r.getManagedPods(ctx, cluster)
	if err != nil {
		return nil, err
	}

	childPVCs, err := r.getManagedPVCs(ctx, cluster)
	if err != nil {
		return nil, err
	}

	childJobs, err := r.getManagedJobs(ctx, cluster)
	if err != nil {
		return nil, err
	}

	return &managedResources{
		pods: childPods,
		pvcs: childPVCs,
		jobs: childJobs,
	}, nil
}

func (r *ClusterReconciler) getManagedPods(
	ctx context.Context,
	cluster v1alpha1.Cluster,
) (corev1.PodList, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

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
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

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

// getManagedJobs extract the list of jobs which are being created
// by this cluster
func (r *ClusterReconciler) getManagedJobs(
	ctx context.Context,
	cluster v1alpha1.Cluster,
) (batchv1.JobList, error) {
	var childJobs batchv1.JobList
	if err := r.List(ctx, &childJobs,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{jobOwnerKey: cluster.Name},
	); err != nil {
		return batchv1.JobList{}, err
	}

	return childJobs, nil
}

func (r *ClusterReconciler) updateResourceStatus(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	resources *managedResources,
) error {
	// Retrieve the cluster key
	key := expectations.KeyFunc(cluster)

	existingClusterStatus := cluster.Status

	// Fill the list of dangling PVCs
	oldPVCCount := cluster.Status.PVCCount
	newPVCCount := int32(len(resources.pvcs.Items))
	cluster.Status.PVCCount = newPVCCount
	cluster.Status.DanglingPVC = specs.DetectDanglingPVCs(resources.pods.Items, resources.pvcs.Items)

	// From now on, we'll consider only Active pods: those Pods
	// that will possibly work. Let's forget about the failed ones
	filteredPods := utils.FilterActivePods(resources.pods.Items)

	// Update the pvcExpectations for the cluster
	r.pvcExpectations.LowerExpectationsDelta(key, int(newPVCCount-oldPVCCount))

	// Count pods
	oldInstances := cluster.Status.Instances
	newInstances := int32(len(filteredPods))
	cluster.Status.Instances = newInstances
	cluster.Status.ReadyInstances = int32(utils.CountReadyPods(filteredPods))

	// Update the podExpectations for the cluster
	r.podExpectations.LowerExpectationsDelta(key, int(newInstances-oldInstances))

	// Count jobs
	oldJobs := cluster.Status.JobCount
	newJobs := int32(len(resources.jobs.Items))
	cluster.Status.JobCount = newJobs

	// Update the jobExpectations for the cluster
	r.jobExpectations.LowerExpectationsDelta(key, int(newJobs-oldJobs))

	// Instances status
	cluster.Status.InstancesStatus = utils.ListStatusPods(resources.pods.Items)

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
