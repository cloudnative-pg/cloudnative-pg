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
	"sort"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ErrWalReceiversRunning is raised when a new primary server can't be elected
// because there is a WAL receiver running in our Pod list
var ErrWalReceiversRunning = fmt.Errorf("wal receivers are still running")

// updateTargetPrimaryFromPods sets the name of the target primary from the Pods status if needed
// this function will return the name of the new primary selected for promotion
func (r *ClusterReconciler) updateTargetPrimaryFromPods(
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
	if primary := status.Items[0]; (primary.IsPrimary || (cluster.IsReplica() && primary.IsReady)) &&
		primary.Pod.Name == cluster.Status.CurrentPrimary &&
		cluster.Status.TargetPrimary == cluster.Status.CurrentPrimary {
		isPrimaryOnUnschedulableNode, err := r.isNodeUnschedulable(ctx, primary.Node)
		if err != nil {
			contextLogger.Error(err, "while checking if current primary is on an unschedulable node")
			// in case of error it's better to proceed with the normal target primary reconciliation
		} else if isPrimaryOnUnschedulableNode {
			contextLogger.Info("Primary is running on an unschedulable node, will try switching over",
				"node", primary.Node, "primary", primary.Pod.Name)
			return r.setPrimaryOnSchedulableNode(ctx, cluster, status, &primary)
		}
	}

	// Second step: check if the first element of the sorted list is the primary
	if cluster.IsReplica() {
		return r.updateTargetPrimaryFromPodsReplicaCluster(ctx, cluster, status, resources)
	}

	return r.updateTargetPrimaryFromPodsPrimaryCluster(ctx, cluster, status, resources)
}

// updateTargetPrimaryFromPodsPrimaryCluster sets the name of the target primary from the Pods status if needed
// this function will return the name of the new primary selected for promotion
func (r *ClusterReconciler) updateTargetPrimaryFromPodsPrimaryCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	resources *managedResources,
) (string, error) {
	contextLogger := log.FromContext(ctx)

	// When replica mode is not active, the first instance in the list is the primary one.
	// This means we can just look at the first element of the list to check if the primary
	// is available or not.

	// If the first pod in the sorted list is not the primary we need to execute a failover
	// or wait if the failover has already been triggered

	// If the first pod in the sorted list is already the targetPrimary,
	// we have nothing to do here.
	if cluster.Status.TargetPrimary == status.Items[0].Pod.Name {
		return "", nil
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
		contextLogger.Info("Failing over", "newPrimary", status.Items[0].Pod.Name)
		status.LogStatus(ctx)
		contextLogger.Debug("Cluster status before failover", "instances", resources.instances)
		r.Recorder.Eventf(cluster, "Normal", "FailoverTarget",
			"Failing over from %v to %v",
			cluster.Status.CurrentPrimary, status.Items[0].Pod.Name)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseFailOver,
			fmt.Sprintf("Failing over from %v to %v", cluster.Status.CurrentPrimary, status.Items[0].Pod.Name)); err != nil {
			return "", err
		}
	} else {
		contextLogger.Info("Target primary isn't healthy, switching target",
			"newPrimary", status.Items[0].Pod.Name)
		status.LogStatus(ctx)
		contextLogger.Debug("Cluster status before switching target", "instances", resources.instances)
		r.Recorder.Eventf(cluster, "Normal", "FailingOver",
			"Target primary isn't healthy, switching target from %v to %v",
			cluster.Status.TargetPrimary, status.Items[0].Pod.Name)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseSwitchover,
			fmt.Sprintf("Switching over to %v", status.Items[0].Pod.Name)); err != nil {
			return "", err
		}
	}

	// Set the first pod in the sorted list as the new targetPrimary
	return status.Items[0].Pod.Name, r.setPrimaryInstance(ctx, cluster, status.Items[0].Pod.Name)
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
) (string, error) {
	contextLogger := log.FromContext(ctx)

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
			len(podsOnOtherNodes.Items) < cluster.Spec.Instances-1) {
		contextLogger.Info("Current primary is running on unschedulable node and something is already in progress",
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

		if !utils.IsPodReady(candidate.Pod) {
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

// updateTargetPrimaryFromPodsReplicaCluster sets the name of the target designated
// primary from the Pods status if needed this function will return the name of the
// new primary selected for promotion
func (r *ClusterReconciler) updateTargetPrimaryFromPodsReplicaCluster(
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
	podsOnOtherNodes := postgres.PostgresqlStatusList{}
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

	status := r.extractInstancesStatus(ctx, filteredPods)
	sort.Sort(&status)
	for idx := range status.Items {
		if status.Items[idx].Error != nil {
			log.FromContext(ctx).Info("Cannot extract Pod status",
				"name", status.Items[idx].Pod.Name,
				"error", status.Items[idx].Error.Error())
		}
	}
	return status
}

// updateClusterAnnotationsOnPods we check if we need to add or modify existing annotations specified in the cluster but
// not existing in the pods. We do not support the case of removed annotations from the cluster resource.
func (r *ClusterReconciler) updateClusterAnnotationsOnPods(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pods corev1.PodList,
) error {
	contextLogger := log.FromContext(ctx)

	for i := range pods.Items {
		pod := &pods.Items[i]

		// if all the required annotations are already set and with the correct value,
		// we proceed to the next item
		if utils.IsAnnotationSubset(pod.Annotations, cluster.Annotations, cluster.GetFixedInheritedAnnotations(),
			configuration.Current) &&
			utils.IsAnnotationAppArmorPresentInObject(&pod.ObjectMeta, cluster.Annotations) {
			contextLogger.Debug(
				"Skipping cluster annotations reconciliation, because they are already present on pod",
				"pod", pod.Name,
				"podAnnotations", pod.Annotations,
				"clusterAnnotations", cluster.Annotations,
			)
			continue
		}

		// otherwise, we add the modified/new annotations to the pod
		patch := client.MergeFrom(pod.DeepCopy())
		utils.InheritAnnotations(&pod.ObjectMeta, cluster.Annotations,
			cluster.GetFixedInheritedAnnotations(), configuration.Current)
		if utils.IsAnnotationAppArmorPresent(cluster.Annotations) {
			utils.AnnotateAppArmor(&pod.ObjectMeta, cluster.Annotations)
		}
		contextLogger.Info("Updating cluster annotations on pod", "pod", pod.Name)
		if err := r.Patch(ctx, pod, patch); err != nil {
			return err
		}
		continue
	}

	return nil
}

// updateClusterAnnotationsOnPods we check if we need to add or modify existing labels specified in the cluster but
// not existing in the pods. We do not support the case of removed labels from the cluster resource.
func (r *ClusterReconciler) updateClusterLabelsOnPods(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pods corev1.PodList,
) error {
	contextLogger := log.FromContext(ctx)

	for i := range pods.Items {
		pod := &pods.Items[i]

		// if all the required labels are already set and with the correct value,
		// we proceed to the next item
		if utils.IsLabelSubset(pod.Labels, cluster.Labels, cluster.GetFixedInheritedLabels(),
			configuration.Current) {
			contextLogger.Debug(
				"Skipping cluster label reconciliation, because they are already present on pod",
				"pod", pod.Name,
				"podLabels", pod.Labels,
				"clusterLabels", cluster.Labels,
			)
			continue
		}

		// otherwise, we add the modified/new labels to the pod
		patch := client.MergeFrom(pod.DeepCopy())
		utils.InheritLabels(&pod.ObjectMeta, cluster.Labels, cluster.GetFixedInheritedLabels(), configuration.Current)

		contextLogger.Info("Updating cluster labels on pod", "pod", pod.Name)
		if err := r.Patch(ctx, pod, patch); err != nil {
			return err
		}
	}

	return nil
}

// updateClusterAnnotationsOnPVCs we check if we need to add or modify existing annotations specified in the cluster but
// not existing in the PVCs. We do not support the case of removed annotations from the cluster resource.
func (r *ClusterReconciler) updateClusterAnnotationsOnPVCs(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pvcs corev1.PersistentVolumeClaimList,
) error {
	contextLogger := log.FromContext(ctx)

	for i := range pvcs.Items {
		pvc := &pvcs.Items[i]

		// if all the required annotations are already set and with the correct value,
		// we proceed to the next item
		if utils.IsAnnotationSubset(pvc.Annotations,
			cluster.Annotations,
			cluster.GetFixedInheritedLabels(),
			configuration.Current) &&
			utils.IsAnnotationAppArmorPresentInObject(&pvc.ObjectMeta, cluster.Annotations) {
			contextLogger.Debug(
				"Skipping cluster annotations reconciliation, because they are already present on pvc",
				"pvc", pvc.Name,
				"pvcAnnotations", pvc.Annotations,
				"clusterAnnotations", cluster.Annotations,
			)
			continue
		}

		// otherwise, we add the modified/new annotations to the pvc
		patch := client.MergeFrom(pvc.DeepCopy())
		utils.InheritAnnotations(&pvc.ObjectMeta, cluster.Annotations,
			cluster.GetFixedInheritedAnnotations(), configuration.Current)

		contextLogger.Info("Updating cluster annotations on pvc", "pvc", pvc.Name)
		if err := r.Patch(ctx, pvc, patch); err != nil {
			return err
		}
		continue
	}

	return nil
}

// updateClusterAnnotationsOnPVCs we check if we need to add or modify existing labels specified in the cluster but
// not existing in the PVCs. We do not support the case of removed labels from the cluster resource.
func (r *ClusterReconciler) updateClusterLabelsOnPVCs(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pvcs corev1.PersistentVolumeClaimList,
) error {
	contextLogger := log.FromContext(ctx)

	for i := range pvcs.Items {
		pvc := &pvcs.Items[i]

		// if all the required labels are already set and with the correct value,
		// we proceed to the next item
		if utils.IsLabelSubset(pvc.Labels,
			cluster.Labels,
			cluster.GetFixedInheritedAnnotations(),
			configuration.Current) {
			contextLogger.Debug(
				"Skipping cluster label reconciliation, because they are already present on pvc",
				"pvc", pvc.Name,
				"pvcLabels", pvc.Labels,
				"clusterLabels", cluster.Labels,
			)
			continue
		}

		// otherwise, we add the modified/new labels to the pvc
		patch := client.MergeFrom(pvc.DeepCopy())
		utils.InheritLabels(&pvc.ObjectMeta, cluster.Labels, cluster.GetFixedInheritedLabels(), configuration.Current)

		contextLogger.Debug("Updating cluster labels on pvc", "pvc", pvc.Name)
		if err := r.Patch(ctx, pvc, patch); err != nil {
			return err
		}
		contextLogger.Info("Updated cluster label on pvc", "pvc", pvc.Name)
	}

	return nil
}

// Make sure that only the currentPrimary has the label forward write traffic to him
func (r *ClusterReconciler) updateRoleLabelsOnPods(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pods corev1.PodList,
) error {
	contextLogger := log.FromContext(ctx)

	// No current primary, no work to do
	if cluster.Status.CurrentPrimary == "" {
		return nil
	}

	primaryFound := false
	for idx := range pods.Items {
		pod := &pods.Items[idx]

		if !utils.IsPodActive(*pod) {
			contextLogger.Info("Ignoring not active Pod during label update",
				"pod", pod.Name, "status", pod.Status)
			continue
		}

		podRole, hasRole := pod.ObjectMeta.Labels[specs.ClusterRoleLabelName]

		switch {
		case pod.Name == cluster.Status.CurrentPrimary:
			primaryFound = true

			if !hasRole || podRole != specs.ClusterRoleLabelPrimary {
				contextLogger.Info("Setting primary label", "pod", pod.Name)
				patch := client.MergeFrom(pod.DeepCopy())
				pod.Labels[specs.ClusterRoleLabelName] = specs.ClusterRoleLabelPrimary
				if err := r.Patch(ctx, pod, patch); err != nil {
					return err
				}
			}

		default:
			if !hasRole || podRole != specs.ClusterRoleLabelReplica {
				contextLogger.Info("Setting replica label", "pod", pod.Name)
				patch := client.MergeFrom(pod.DeepCopy())
				pod.Labels[specs.ClusterRoleLabelName] = specs.ClusterRoleLabelReplica
				if err := r.Patch(ctx, pod, patch); err != nil {
					return err
				}
			}
		}
	}

	if !primaryFound {
		contextLogger.Info("No primary instance found for this cluster")
	}

	return nil
}

// updateOperatorLabelsOnInstances ensures that the instances have the correct labels
func (r *ClusterReconciler) updateOperatorLabelsOnInstances(
	ctx context.Context,
	instances corev1.PodList,
) error {
	for i := range instances.Items {
		instance := &instances.Items[i]
		if instance.Labels == nil {
			instance.Labels = map[string]string{}
		}
		var modified bool
		origInstance := instance.DeepCopy()

		if instance.Labels[utils.InstanceNameLabelName] != instance.Name {
			instance.Labels[utils.InstanceNameLabelName] = instance.Name
			modified = true
		}

		if instance.Labels[utils.PodRoleLabelName] != string(utils.PodRoleInstance) {
			instance.Labels[utils.PodRoleLabelName] = string(utils.PodRoleInstance)
			modified = true
		}
		if !modified {
			continue
		}

		if err := r.Client.Patch(ctx, instance, client.MergeFrom(origInstance)); err != nil {
			return err
		}
	}

	return nil
}

// updateOperatorLabelsOnPVC ensures that the PVCs have the correct labels
func (r *ClusterReconciler) updateOperatorLabelsOnPVC(
	ctx context.Context,
	instances corev1.PodList,
	pvcs corev1.PersistentVolumeClaimList,
) error {
	for _, pod := range instances.Items {
		podRole, podHasRole := pod.ObjectMeta.Labels[specs.ClusterRoleLabelName]

		instancePVCs := specs.FilterInstancePVCs(pvcs.Items, pod.Spec)
		for i := range instancePVCs {
			pvc := &instancePVCs[i]
			var modified bool
			// this is needed, because on older versions pvc.labels could be nil
			if pvc.Labels == nil {
				pvc.Labels = map[string]string{}
			}

			origPvc := pvc.DeepCopy()
			if podHasRole && pvc.ObjectMeta.Labels[specs.ClusterRoleLabelName] != podRole {
				pvc.Labels[specs.ClusterRoleLabelName] = podRole
				modified = true
			}
			if pvc.ObjectMeta.Labels[utils.InstanceNameLabelName] != pod.Name {
				pvc.ObjectMeta.Labels[utils.InstanceNameLabelName] = pod.Name
				modified = true
			}
			if !modified {
				continue
			}
			if err := r.Client.Patch(ctx, pvc, client.MergeFrom(origPvc)); err != nil {
				return err
			}
		}
	}

	return nil
}

// getSacrificialPod get the Pod who is supposed to be deleted
// when the cluster is scaled down
func getSacrificialInstance(podList []corev1.Pod) *corev1.Pod {
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
