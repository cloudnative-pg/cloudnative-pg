/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
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

	// find the primary both real or designated (for replica clusters)
	primaryPod := status.Items[0]
	for i, pod := range status.Items {
		if pod.IsPrimary ||
			(pod.PodName == cluster.Status.TargetPrimary &&
				cluster.Status.TargetPrimary == cluster.Status.CurrentPrimary) {
			primaryPod = status.Items[i]
			break
		}
	}

	// If the first instance in the sorted list is already the primary, we check whether we need
	// a switchover because it's running on an unschedulable node, e.g. due to a node being drained
	primaryUnschedulable, err := r.isNodeUnschedulable(ctx, primaryPod.Node)
	if err != nil {
		return "", err
	}

	if primaryUnschedulable {
		log.Info("Primary is running on an unschedulable node, will try switching over",
			"node", status.Items[0].Node, "primary", primaryPod.PodName)
		return r.setPrimaryOnSchedulableNode(ctx, cluster, status, &primaryPod, log)
	}

	if cluster.IsReplica() {
		return r.updateTargetPrimaryFromPodsReplicaCluster(ctx, cluster, status, resources, &primaryPod)
	}

	return r.updateTargetPrimaryFromPodsPrimaryCluster(ctx, cluster, status, resources, &primaryPod)
}

// updateTargetPrimaryFromPodsPrimaryCluster sets the name of the target primary from the Pods status if needed
// this function will returns the name of the new primary selected for promotion
func (r *ClusterReconciler) updateTargetPrimaryFromPodsPrimaryCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	resources *managedResources,
	primaryPod *postgres.PostgresqlStatus,
) (string, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	// When replica mode is not active, the first instance in the list is the primary one.
	// This means we can just look at the first element of the list to check if the primary
	// if available or not.

	// If the first pod in the sorted list is not the primary we need to execute a failover
	// or wait if the failover has already been triggered

	// If the first pod in the sorted list is already the targetPrimary,
	// we have nothing to do here.
	if cluster.Status.TargetPrimary == primaryPod.PodName {
		return "", nil
	}

	// We can select a new primary only if all the alive pods agrees
	// that the old one isn't streaming anymore.
	if !status.AreWalReceiversDown() {
		return "", ErrWalReceiversRunning
	}

	// Set the first pod in the sorted list as the new targetPrimary.
	// This may trigger a failover if previous primary disappeared
	// or change the target primary if the current one is not valid anymore.
	if cluster.Status.TargetPrimary == cluster.Status.CurrentPrimary {
		log.Info("Current primary isn't healthy, failing over",
			"newPrimary", primaryPod.PodName,
			"clusterStatus", status)
		log.V(1).Info("Cluster status before failover", "pods", resources.pods)
		r.Recorder.Eventf(cluster, "Normal", "FailingOver",
			"Current primary isn't healthy, failing over from %v to %v",
			cluster.Status.TargetPrimary, primaryPod.PodName)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseFailOver,
			fmt.Sprintf("Failing over to %v", status.Items[0].PodName)); err != nil {
			return "", err
		}
	} else {
		log.Info("Target primary isn't healthy, switching target",
			"newPrimary", status.Items[0].PodName,
			"clusterStatus", status)
		log.V(1).Info("Cluster status before switching target", "pods", resources.pods)
		r.Recorder.Eventf(cluster, "Normal", "FailingOver",
			"Target primary isn't healthy, switching target from %v to %v",
			cluster.Status.TargetPrimary, primaryPod.PodName)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseSwitchover,
			fmt.Sprintf("Switching over to %v", primaryPod.PodName)); err != nil {
			return "", err
		}
	}

	// No primary, no party. Failover please!
	return primaryPod.PodName, r.setPrimaryInstance(ctx, cluster, primaryPod.PodName)
}

// isNodeUnschedulable checks whether a node is set to unschedulable
func (r *ClusterReconciler) isNodeUnschedulable(ctx context.Context, nodeName string) (bool, error) {
	var node corev1.Node
	err := r.Get(ctx, client.ObjectKey{Name: nodeName}, &node)
	if err != nil {
		return false, err
	}
	return node.Spec.Unschedulable, nil
}

// Pick the next primary on a schedulable node, if the current is running on an unschedulable one,
// e.g. in case a drain is in progress
func (r *ClusterReconciler) setPrimaryOnSchedulableNode(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	primaryPod *postgres.PostgresqlStatus,
	log logr.Logger,
) (string, error) {
	// Checking failed pods, e.g. pending pods due to missing PVCs
	_, hasFailedPods := cluster.Status.InstancesStatus[utils.PodFailed]

	// Checking whether there are pods on other nodes
	podsOnOtherNodes := GetPodsNotOnPrimaryNode(status, primaryPod)

	// If no failed pods are found, but not all instances are ready or not all replicas have been moved to a
	// schedulable instance, wait, because something is in progress
	if !hasFailedPods &&
		// e.g an instance is being joined
		(cluster.Spec.Instances != cluster.Status.ReadyInstances ||
			// e.g. we want all instances to be moved to a schedulable node before triggering the switchover
			len(podsOnOtherNodes.Items) < int(cluster.Spec.Instances)-1) {
		log.Info("Current primary is running on unschedulable node and something is already in progress",
			"currentPrimary", primaryPod,
			"podsOnOtherNodes", len(podsOnOtherNodes.Items),
			"instances", cluster.Spec.Instances,
			"readyInstances", cluster.Status.ReadyInstances,
			"primaryNode", primaryPod.Node)
		return "", nil
	}

	// In case we have failed pods, we try to do a switchover, because pods could be in this state
	// (e.g. Pending) because something is preventing pods to be scheduled successfully, e.g. draining the primary node
	// while a maintenance window is in progress and reusePVC is set to false, in this case a replica would be terminated
	// and the operator would be waiting for it to be rescheduled to a different node indefinitely if the PVC used can not
	// be moved between nodes, e.g. local-path-provisioner on Kind.

	// Start looking for the next primary among the pods
	for _, candidate := range podsOnOtherNodes.Items {
		// If candidate on an unschedulable node too, skip it
		if unschedulable, _ := r.isNodeUnschedulable(ctx, candidate.Node); unschedulable {
			continue
		}

		// Set the current candidate as targetPrimary
		log.Info("Current primary is running on unschedulable node, triggering a switchover",
			"currentPrimary", status.Items[0].PodName, "currentPrimaryNode", status.Items[0].Node,
			"targetPrimary", candidate.PodName, "targetPrimaryNode", candidate.Node)
		r.Recorder.Eventf(cluster, "Normal", "SwitchingOver",
			"Current primary is running on unschedulable node %v, switching over from %v to %v",
			status.Items[0].Node, cluster.Status.TargetPrimary, candidate.PodName)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseSwitchover,
			fmt.Sprintf("Switching over to %v, because primary instance "+
				"was running on unschedulable node %v",
				candidate.PodName,
				status.Items[0].Node)); err != nil {
			return "", err
		}
		return candidate.PodName, r.setPrimaryInstance(ctx, cluster, candidate.PodName)
	}

	// if we are here this means no new primary has been chosen
	log.Info("Current primary is running on unschedulable node, but there are no valid candidates",
		"currentPrimary", status.Items[0].PodName,
		"primaryNode", status.Items[0].Node,
		"pods", status.Items)
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
	primaryPod *postgres.PostgresqlStatus,
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

	return primaryPod.PodName, r.setPrimaryInstance(ctx, cluster, primaryPod.PodName)
}

// GetPodsNotOnPrimaryNode filters out only pods that are not on the same node as the primary one
func GetPodsNotOnPrimaryNode(
	status postgres.PostgresqlStatusList,
	primaryPod *postgres.PostgresqlStatus,
) postgres.PostgresqlStatusList {
	podsOnOtherNodes := postgres.PostgresqlStatusList{}
	if primaryPod == nil {
		return podsOnOtherNodes
	}
	for _, candidate := range status.Items {
		if candidate.PodName != primaryPod.PodName && candidate.Node != primaryPod.Node {
			podsOnOtherNodes.Items = append(podsOnOtherNodes.Items, candidate)
		}
	}
	return podsOnOtherNodes
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
