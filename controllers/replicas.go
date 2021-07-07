/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// ErrWalReceiversRunning is raised when a new primary server can't be elected
// because there is a WAL receiver running in our Pod list
var ErrWalReceiversRunning = fmt.Errorf("wal receivers are still running")

// updateTargetPrimaryFromPods sets the name of the target primary from the Pods status if needed
// this function will returns the name of the new primary selected for promotion
func (r *ClusterReconciler) updateTargetPrimaryFromPods(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	resources *managedResources,
) (string, error) {
	if len(status.Items) == 0 {
		// We have no status to check and we can't make a
		// switchover under those conditions
		return "", nil
	}
	if cluster.IsReplica() {
		return r.updateTargetPrimaryFromPodsReplicaCluster(ctx, cluster, status, resources)
	}

	return r.updateTargetPrimaryFromPodsPrimaryCluster(ctx, cluster, status, resources)
}

// updateTargetPrimaryFromPodsPrimaryCluster sets the name of the target primary from the Pods status if needed
// this function will returns the name of the new primary selected for promotion
func (r *ClusterReconciler) updateTargetPrimaryFromPodsPrimaryCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	resources *managedResources,
) (string, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	// When replica mode is not active, the first instance in the list is the primary one.
	// This means we can just look at the first element of the list to check if the primary
	// if available or not.

	if !status.Items[0].IsPrimary && cluster.Status.TargetPrimary != status.Items[0].PodName {
		if !status.AreWalReceiversDown() {
			return "", ErrWalReceiversRunning
		}

		log.Info("Current primary isn't healthy, failing over",
			"newPrimary", status.Items[0].PodName,
			"clusterStatus", status)
		log.V(1).Info("Cluster status before failover", "pods", resources.pods)
		r.Recorder.Eventf(cluster, "Normal", "FailingOver",
			"Current primary isn't healthy, failing over from %v to %v",
			cluster.Status.TargetPrimary, status.Items[0].PodName)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseFailOver,
			fmt.Sprintf("Failing over to %v", status.Items[0].PodName)); err != nil {
			return "", err
		}
		// No primary, no party. Failover please!
		return status.Items[0].PodName, r.setPrimaryInstance(ctx, cluster, status.Items[0].PodName)
	}

	return "", nil
}

// updateTargetPrimaryFromPodsReplicaCluster sets the name of the target designated
// primary from the Pods status if needed this function will returns the name of the
// new primary selected for promotion
func (r *ClusterReconciler) updateTargetPrimaryFromPodsReplicaCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	resources *managedResources,
) (string, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	// When replica mode is active, the designated primary may not be the first element
	// in this list, since from the PostgreSQL point-of-view it's not the real primary.

	for _, statusRecord := range status.Items {
		if statusRecord.PodName == cluster.Status.TargetPrimary {
			// Everything fine, the current designated primary is active
			return "", nil
		}
	}

	// The designated primary is not correctly working and we need to elect a new one
	if !status.AreWalReceiversDown() {
		return "", ErrWalReceiversRunning
	}

	log.Info("Current target primary isn't healthy, failing over",
		"newPrimary", status.Items[0].PodName,
		"clusterStatus", status)
	log.V(1).Info("Cluster status before failover", "pods", resources.pods)
	r.Recorder.Eventf(cluster, "Normal", "FailingOver",
		"Current target primary isn't healthy, failing over from %v to %v",
		cluster.Status.TargetPrimary, status.Items[0].PodName)
	if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseFailOver,
		fmt.Sprintf("Failing over to %v", status.Items[0].PodName)); err != nil {
		return "", err
	}

	return status.Items[0].PodName, r.setPrimaryInstance(ctx, cluster, status.Items[0].PodName)
}

// getStatusFromInstances gets the replication status from the PostgreSQL instances,
// the returned list is sorted in order to have the primary as the first element
// and the other instances in their election order
func (r *ClusterReconciler) getStatusFromInstances(
	ctx context.Context,
	pods corev1.PodList,
) postgres.PostgresqlStatusList {
	// Only work on Pods which can still become active in the future
	filteredPods := utils.FilterActivePods(pods.Items)
	if len(filteredPods) == 0 {
		// No instances to control
		return postgres.PostgresqlStatusList{}
	}

	status := ExtractInstancesStatus(ctx, filteredPods)
	sort.Sort(&status)
	for idx := range status.Items {
		if status.Items[idx].ExecError != nil {
			r.Log.Info("Cannot extract Pod status",
				"name", status.Items[idx].PodName,
				"error", status.Items[idx].ExecError.Error())
		}
	}
	return status
}

// Make sure that only the currentPrimary has the label forward write traffic to him
func (r *ClusterReconciler) updateLabelsOnPods(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pods corev1.PodList,
) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	// No current primary, no work to do
	if cluster.Status.CurrentPrimary == "" {
		return nil
	}

	primaryFound := false
	for idx := range pods.Items {
		pod := &pods.Items[idx]

		if !utils.IsPodActive(*pod) {
			log.Info("Ignoring not active Pod during label update",
				"pod", pod.Name, "status", pod.Status)
			continue
		}

		podRole, hasRole := pod.ObjectMeta.Labels[specs.ClusterRoleLabelName]

		switch {
		case pod.Name == cluster.Status.CurrentPrimary:
			primaryFound = true

			if !hasRole || podRole != specs.ClusterRoleLabelPrimary {
				log.Info("Setting primary label", "pod", pod.Name)
				patch := client.MergeFrom(pod.DeepCopy())
				pod.Labels[specs.ClusterRoleLabelName] = specs.ClusterRoleLabelPrimary
				if err := r.Patch(ctx, pod, patch); err != nil {
					return err
				}
			}

		default:
			if !hasRole || podRole != specs.ClusterRoleLabelReplica {
				log.Info("Setting replica label", "pod", pod.Name)
				patch := client.MergeFrom(pod.DeepCopy())
				pod.Labels[specs.ClusterRoleLabelName] = specs.ClusterRoleLabelReplica
				if err := r.Patch(ctx, pod, patch); err != nil {
					return err
				}
			}
		}
	}

	if !primaryFound {
		log.Info("No primary instance found for this cluster")
	}

	return nil
}

// getSacrificialPod get the Pod who is supposed to be deleted
// when the cluster is scaled down
func getSacrificialPod(podList []corev1.Pod) *corev1.Pod {
	resultIdx := -1
	var lastFoundSerial int

	for idx, pod := range podList {
		// Avoid parting non ready nodes, non active nodes, or primary nodes
		if !utils.IsPodReady(pod) || !utils.IsPodActive(pod) || specs.IsPodPrimary(pod) {
			continue
		}

		// If we cannot get the node serial this is not one of our Pods
		podSerial, err := specs.GetNodeSerial(pod.ObjectMeta)
		if err != nil {
			continue
		}

		if lastFoundSerial == 0 || lastFoundSerial < podSerial {
			resultIdx = idx
			lastFoundSerial = podSerial
		}
	}

	if resultIdx == -1 {
		return nil
	}
	return &podList[resultIdx]
}
