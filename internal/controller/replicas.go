/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ErrWalReceiversRunning is raised when a new primary server can't be elected
// because there is a WAL receiver running in our Pod list
var ErrWalReceiversRunning = fmt.Errorf("wal receivers are still running")

// ErrWaitingOnFailOverDelay is raised when the primary server can't be elected because the .spec.failoverDelay hasn't
// elapsed yet
var ErrWaitingOnFailOverDelay = fmt.Errorf("current primary isn't healthy, waiting for the delay before triggering a failover") //nolint: lll

// reconcileTargetPrimaryFromPods sets the name of the target primary from the Pods status if needed
// this function will return the name of the new primary selected for promotion.
// Returns the name of the primary if any changes was made and any error encountered.
// TODO: move to a reconciler package
func (r *ClusterReconciler) reconcileTargetPrimaryFromPods(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	resources *managedResources,
) (string, error) {
	contextLogger := log.FromContext(ctx)

	if len(status.Items) == 0 {
		// We have no status to check and we can't make a
		// switchover under those conditions
		return "", nil
	}

	// First step: check if the current primary is running in an unschedulable node
	// and issue a switchover if that's the case
	// Can not check node settings in namespaced deployment
	if !configuration.Current.Namespaced {
		if primary := status.Items[0]; (primary.IsPrimary || (cluster.IsReplica() && primary.IsPodReady)) &&
			primary.Pod.Name == cluster.Status.CurrentPrimary &&
			cluster.Status.TargetPrimary == cluster.Status.CurrentPrimary {
			isPrimaryOnUnschedulableNode, err := r.isNodeUnschedulableOrBeingDrained(ctx, primary.Node)
			if err != nil {
				contextLogger.Error(err, "while checking if current primary is on an unschedulable node")
				// in case of error it's better to proceed with the normal target primary reconciliation
			} else if isPrimaryOnUnschedulableNode {
				contextLogger.Info("Primary is running on an unschedulable node, will try switching over",
					"node", primary.Node, "primary", primary.Pod.Name)
				return r.setPrimaryOnSchedulableNode(ctx, cluster, status, &primary)
			}
		}
	}
	// Second step: check if the first element of the sorted list is the primary
	if cluster.IsReplica() {
		return r.reconcileTargetPrimaryForReplicaCluster(ctx, cluster, status, resources)
	}

	return r.reconcileTargetPrimaryForNonReplicaCluster(ctx, cluster, status, resources)
}

// reconcileTargetPrimaryForNonReplicaCluster sets the name of the target primary from the Pods status if needed
// this function will return the name of the new primary selected for promotion
func (r *ClusterReconciler) reconcileTargetPrimaryForNonReplicaCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	resources *managedResources,
) (string, error) {
	contextLogger := log.FromContext(ctx)

	mostAdvancedInstance := status.Items[0]
	if cluster.Status.TargetPrimary == mostAdvancedInstance.Pod.Name {
		return "", nil
	}

	// If the first pod of the list has no reported status we can't evaluate the failover logic.
	if !mostAdvancedInstance.HasHTTPStatus() {
		return "", nil
	}

	if err := r.enforceFailoverDelay(ctx, cluster); err != nil {
		return "", err
	}

	// If quorum check is active, ensure we don't failover in unsafe scenarios.
	if cluster.Status.TargetPrimary == cluster.Status.CurrentPrimary &&
		cluster.IsFailoverQuorumActive() {
		if status, err := r.evaluateQuorumCheck(ctx, cluster, status); err != nil {
			return "", err
		} else if !status {
			// Prevent a failover from happening
			return "", nil
		}
	}

	// The current primary is not correctly working, and we need to elect a new one
	// but before doing that we need to wait for all the WAL receivers to be
	// terminated. To make sure they eventually terminate we signal the old primary
	// (if is still alive) to shut down by setting the apiv1.PendingFailoverMarker as
	// target primary.
	if cluster.Status.TargetPrimary == cluster.Status.CurrentPrimary {
		contextLogger.Info("Current primary isn't healthy, initiating a failover")
		status.LogStatus(ctx)
		contextLogger.Debug("Cluster status before initiating the failover", "instances", resources.instances)
		r.Recorder.Eventf(cluster, "Normal", "FailingOver",
			"Current primary isn't healthy, initiating a failover from %v", cluster.Status.CurrentPrimary)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseFailOver,
			fmt.Sprintf("Initiating a failover from %v", cluster.Status.CurrentPrimary)); err != nil {
			return "", err
		}
		err := r.setPrimaryInstance(ctx, cluster, apiv1.PendingFailoverMarker)
		if err != nil {
			return "", err
		}
	}

	// Wait until all the WAL receivers are down. This is needed to avoid losing the WAL
	// data that is being received (think about a switchover).
	if !status.AreWalReceiversDown(cluster.Status.CurrentPrimary) {
		return "", ErrWalReceiversRunning
	}

	// This may be tha last step of a failover if target primary is set to apiv1.PendingFailoverMarker
	// or change the target primary if the current one is not valid anymore.
	if cluster.Status.TargetPrimary == apiv1.PendingFailoverMarker {
		contextLogger.Info("Failing over", "newPrimary", mostAdvancedInstance.Pod.Name)
		status.LogStatus(ctx)
		contextLogger.Debug("Cluster status before failover", "instances", resources.instances)
		r.Recorder.Eventf(cluster, "Normal", "FailoverTarget",
			"Failing over from %v to %v",
			cluster.Status.CurrentPrimary, mostAdvancedInstance.Pod.Name)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseFailOver,
			fmt.Sprintf("Failing over from %v to %v", cluster.Status.CurrentPrimary, mostAdvancedInstance.Pod.Name),
		); err != nil {
			return "", err
		}
	} else {
		contextLogger.Info("Target primary isn't healthy, switching target",
			"newPrimary", mostAdvancedInstance.Pod.Name)
		status.LogStatus(ctx)
		contextLogger.Debug("Cluster status before switching target", "instances", resources.instances)
		r.Recorder.Eventf(cluster, "Normal", "FailingOver",
			"Target primary isn't healthy, switching target from %v to %v",
			cluster.Status.TargetPrimary, mostAdvancedInstance.Pod.Name)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseSwitchover,
			fmt.Sprintf("Switching over to %v", mostAdvancedInstance.Pod.Name)); err != nil {
			return "", err
		}
	}

	// Set the first pod in the sorted list as the new targetPrimary
	return mostAdvancedInstance.Pod.Name, r.setPrimaryInstance(ctx, cluster, mostAdvancedInstance.Pod.Name)
}

// isNodeUnschedulableOrBeingDrained checks if a node is currently being drained.
// nolint: lll
// Copied from https://github.com/kubernetes-sigs/aws-ebs-csi-driver/blob/7bacf2d36f397bd098b3388403e8759c480be7e5/cmd/hooks/prestop.go#L91
func isNodeUnschedulableOrBeingDrained(node *corev1.Node, drainTaints []string) bool {
	for _, taint := range node.Spec.Taints {
		if slices.Contains(drainTaints, taint.Key) {
			return true
		}
	}

	return node.Spec.Unschedulable
}

// isNodeUnschedulableOrBeingDrained checks whether a node is set to unschedulable
func (r *ClusterReconciler) isNodeUnschedulableOrBeingDrained(
	ctx context.Context,
	nodeName string,
) (bool, error) {
	var node corev1.Node
	err := r.Get(ctx, client.ObjectKey{Name: nodeName}, &node)
	if err != nil {
		return false, err
	}

	return isNodeUnschedulableOrBeingDrained(&node, r.drainTaints), nil
}

// Pick the next primary on a schedulable node, if the current is running on an unschedulable one,
// e.g. in case a drain is in progress
func (r *ClusterReconciler) setPrimaryOnSchedulableNode(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	primaryPod *postgres.PostgresqlStatus,
) (string, error) {
	contextLogger := log.FromContext(ctx)

	// Checking failed pods, e.g. pending pods due to missing PVCs
	_, hasFailedPods := cluster.Status.InstancesStatus[apiv1.PodFailed]

	// Checking whether there are pods on other nodes
	podsOnOtherNodes := GetPodsNotOnPrimaryNode(status, primaryPod)

	// If no failed pods are found, but not all instances are ready or not all replicas have been moved to a
	// schedulable instance, wait, because something is in progress
	if !hasFailedPods &&
		// e.g an instance is being joined
		(cluster.Spec.Instances != cluster.Status.ReadyInstances ||
			// e.g. we want all instances to be moved to a schedulable node before triggering the switchover
			len(podsOnOtherNodes.Items) < cluster.Spec.Instances-1) {
		contextLogger.Info("Current primary is running on unschedulable node and something is already in progress",
			"currentPrimary", primaryPod.Pod.Name,
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
		if status, _ := r.isNodeUnschedulableOrBeingDrained(ctx, candidate.Node); status {
			continue
		}

		if !utils.IsPodReady(*candidate.Pod) {
			continue
		}

		// If the candidate has not established a connection to the current primary, skip it
		if !candidate.IsWalReceiverActive {
			continue
		}

		// Set the current candidate as targetPrimary
		contextLogger.Info("Current primary is running on unschedulable node, triggering a switchover",
			"currentPrimary", primaryPod.Pod.Name, "currentPrimaryNode", primaryPod.Node,
			"targetPrimary", candidate.Pod.Name, "targetPrimaryNode", candidate.Node)
		status.LogStatus(ctx)
		r.Recorder.Eventf(cluster, "Normal", "SwitchingOver",
			"Current primary is running on unschedulable node %v, switching over from %v to %v",
			primaryPod.Node, cluster.Status.TargetPrimary, candidate.Pod.Name)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseSwitchover,
			fmt.Sprintf("Switching over to %v, because primary instance "+
				"was running on unschedulable node %v",
				candidate.Pod.Name,
				primaryPod.Node)); err != nil {
			return "", err
		}
		return candidate.Pod.Name, r.setPrimaryInstance(ctx, cluster, candidate.Pod.Name)
	}

	// if we are here this means no new primary has been chosen
	contextLogger.Info("Current primary is running on unschedulable node, but there are no valid candidates",
		"currentPrimary", status.Items[0].Pod.Name,
		"primaryNode", status.Items[0].Node,
		"instances", status.Items)
	status.LogStatus(ctx)
	return "", nil
}

// reconcileTargetPrimaryForReplicaCluster sets the name of the target designated
// primary from the Pods status if needed this function will return the name of the
// new primary selected for promotion
func (r *ClusterReconciler) reconcileTargetPrimaryForReplicaCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	resources *managedResources,
) (string, error) {
	contextLogger := log.FromContext(ctx)

	// When replica mode is active, the designated primary may not be the first element
	// in this list, since from the PostgreSQL point-of-view it's not the real primary.

	for _, statusRecord := range status.Items {
		if statusRecord.Pod.Name == cluster.Status.TargetPrimary {
			// Everything fine, the current designated primary is active
			return "", nil
		}
	}

	if err := r.enforceFailoverDelay(ctx, cluster); err != nil {
		return "", err
	}

	// The designated primary is not correctly working, and we need to elect a new one
	// but before doing that we need to wait for all the WAL receivers to be
	// terminated. This is needed to avoid losing the WAL data that is being received
	// (think about a switchover).
	if !status.AreWalReceiversDown(cluster.Status.CurrentPrimary) {
		return "", ErrWalReceiversRunning
	}

	contextLogger.Info("Current target primary isn't healthy, failing over",
		"newPrimary", status.Items[0].Pod.Name)
	status.LogStatus(ctx)
	contextLogger.Debug("Cluster status before failover", "instances", resources.instances)
	r.Recorder.Eventf(cluster, "Normal", "FailingOver",
		"Current target primary isn't healthy, failing over from %v to %v",
		cluster.Status.TargetPrimary, status.Items[0].Pod.Name)
	if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseFailOver,
		fmt.Sprintf("Failing over to %v", status.Items[0].Pod.Name)); err != nil {
		return "", err
	}

	return status.Items[0].Pod.Name, r.setPrimaryInstance(ctx, cluster, status.Items[0].Pod.Name)
}

// GetPodsNotOnPrimaryNode filters out only pods that are not on the same node as the primary one
func GetPodsNotOnPrimaryNode(
	status postgres.PostgresqlStatusList,
	primaryPod *postgres.PostgresqlStatus,
) postgres.PostgresqlStatusList {
	podsOnOtherNodes := postgres.PostgresqlStatusList{
		IsReplicaCluster: status.IsReplicaCluster,
		CurrentPrimary:   status.CurrentPrimary,
	}
	if primaryPod == nil {
		return podsOnOtherNodes
	}
	for _, candidate := range status.Items {
		if candidate.Pod.Name != primaryPod.Pod.Name && candidate.Node != primaryPod.Node {
			podsOnOtherNodes.Items = append(podsOnOtherNodes.Items, candidate)
		}
	}
	return podsOnOtherNodes
}

// If the cluster is not in the online upgrading phase, enforceFailoverDelay will evaluate the failover delay specified
// in the cluster's specification.
// If the user has set a custom failoverDelay value and the cluster is in the OnlineUpgrading phase, the function will
// wait for the remaining time of the custom delay, as long as it is greater than the fixed delay of
// 30 seconds for online upgrades.
// enforceFailoverDelay checks if the cluster is in the online upgrading phase and enforces a failover delay of
// 30 seconds if it is. enforceFailoverDelay will return an error if there is an issue with evaluating the failover
// delay.
func (r *ClusterReconciler) enforceFailoverDelay(ctx context.Context, cluster *apiv1.Cluster) error {
	if cluster.Status.Phase == apiv1.PhaseOnlineUpgrading {
		const onlineUpgradeFailOverDelay = 30
		if err := r.evaluateFailoverDelay(ctx, cluster, onlineUpgradeFailOverDelay); err != nil {
			return err
		}
	}

	return r.evaluateFailoverDelay(ctx, cluster, cluster.Spec.FailoverDelay)
}

func (r *ClusterReconciler) evaluateFailoverDelay(
	ctx context.Context,
	cluster *apiv1.Cluster,
	failOverDelay int32,
) error {
	if failOverDelay == 0 {
		return nil
	}

	if cluster.Status.CurrentPrimaryFailingSinceTimestamp == "" {
		cluster.Status.CurrentPrimaryFailingSinceTimestamp = pgTime.GetCurrentTimestamp()
		if err := r.Status().Update(ctx, cluster); err != nil {
			return err
		}
	}
	primaryFailingSince, err := pgTime.DifferenceBetweenTimestamps(
		pgTime.GetCurrentTimestamp(),
		cluster.Status.CurrentPrimaryFailingSinceTimestamp,
	)
	if err != nil {
		return err
	}
	delay := time.Duration(failOverDelay) * time.Second
	if delay > primaryFailingSince {
		return ErrWaitingOnFailOverDelay
	}

	return nil
}

// findDeletableInstance get the Pod who is supposed to be deleted when the cluster is scaled down
func findDeletableInstance(cluster *apiv1.Cluster, instances []corev1.Pod) string {
	resultIdx := -1
	var lastFoundSerial int

	instancesNotRunning := cluster.Status.InstanceNames

	for idx, pod := range instances {
		if nameIndex := slices.Index(instancesNotRunning, pod.Name); nameIndex != -1 {
			instancesNotRunning = slices.Delete(instancesNotRunning, nameIndex, nameIndex+1)
		}

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

	if len(instancesNotRunning) > 0 {
		return instancesNotRunning[len(instancesNotRunning)-1]
	}

	if resultIdx == -1 {
		return ""
	}

	return instances[resultIdx].Name
}
