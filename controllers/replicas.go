/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/postgres"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/specs"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/utils"
)

// updateTargetPrimaryFromPods set the name of the target primary from the Pods status if needed
func (r *ClusterReconciler) updateTargetPrimaryFromPods(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	pods corev1.PodList) error {
	// Only work on Pods which can still become active in the future
	filteredPods := utils.FilterActivePods(pods.Items)
	if len(filteredPods) == 0 {
		// No instances to control
		return nil
	}

	status, err := r.extractInstancesStatus(ctx, filteredPods)
	if err != nil {
		return err
	}

	if len(status.Items) == 0 {
		// Still no ready instances
		return nil
	}

	sort.Sort(&status)

	// Set targetPrimary to do a failover if needed
	if !status.Items[0].IsPrimary {
		r.Log.Info("Current master isn't valid, failing over",
			"newPrimary", status.Items[0].PodName)
		// No primary, no party. Failover please!
		return r.setPrimaryInstance(ctx, cluster, status.Items[0].PodName)
	}

	return nil
}

// Make sure that only the currentPrimary has the label forward write traffic to him
func (r *ClusterReconciler) updateLabelsOnPods(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	pods corev1.PodList) error {
	// No current primary, no work to do
	if cluster.Status.CurrentPrimary == "" {
		return nil
	}

	for idx := range pods.Items {
		pod := &pods.Items[idx]

		if pod.Name == cluster.Status.CurrentPrimary && !specs.IsPodPrimary(pods.Items[idx]) {
			patch := client.MergeFrom(pod.DeepCopy())
			pod.Labels[specs.ClusterRoleLabelName] = specs.ClusterRoleLabelPrimary
			if err := r.Patch(ctx, pod, patch); err != nil {
				return err
			}
		}

		if pod.Name != cluster.Status.CurrentPrimary && specs.IsPodPrimary(pods.Items[idx]) {
			patch := client.MergeFrom(pod.DeepCopy())
			delete(pod.Labels, specs.ClusterRoleLabelName)
			if err := r.Patch(ctx, pod, patch); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ClusterReconciler) getReplicaStatusFromPod(
	ctx context.Context,
	pod corev1.Pod) (postgres.PostgresqlStatus, error) {
	var result postgres.PostgresqlStatus

	twoSeconds := time.Second * 2
	stdout, _, err := utils.ExecCommand(ctx, pod, specs.PostgresContainerName, &twoSeconds, "/pgk", "status")
	if err != nil {
		return result, err
	}

	err = json.Unmarshal([]byte(stdout), &result)
	if err != nil {
		return result, err
	}

	result.PodName = pod.Name
	return result, nil
}

func (r *ClusterReconciler) extractInstancesStatus(
	ctx context.Context,
	filteredPods []corev1.Pod) (postgres.PostgresqlStatusList, error) {
	var result postgres.PostgresqlStatusList

	for idx := range filteredPods {
		if utils.IsPodReady(filteredPods[idx]) {
			instanceStatus, err := r.getReplicaStatusFromPod(ctx, filteredPods[idx])
			if err != nil {
				r.Log.Error(err, "Error while extracting instance status",
					"name", filteredPods[idx].Name,
					"namespace", filteredPods[idx].Namespace)
				return result, err
			}

			result.Items = append(result.Items, instanceStatus)
		}
	}

	return result, nil
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

		podSerial, err := specs.GetNodeSerial(pod)

		// This isn't one of our Pods, since I can't get the node serial
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

// getPrimaryPod get the Pod which is supposed to be the primary of this cluster
func getPrimaryPod(podList []corev1.Pod) *corev1.Pod {
	for idx, pod := range podList {
		if !specs.IsPodPrimary(pod) {
			continue
		}

		if !utils.IsPodReady(pod) || !utils.IsPodActive(pod) {
			continue
		}

		_, err := specs.GetNodeSerial(pod)

		// This isn't one of our Pods, since I can't get the node serial
		if err != nil {
			continue
		}

		return &podList[idx]
	}

	return nil
}
