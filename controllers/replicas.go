/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/postgres"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/specs"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
)

// updateTargetPrimaryFromPods set the name of the target primary from the Pods status if needed
func (r *ClusterReconciler) updateTargetPrimaryFromPods(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	status postgres.PostgresqlStatusList,
) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)
	// TODO: what if I delete the master with only 2 instances
	if len(status.Items) <= 1 && cluster.Status.Instances <= 1 {
		// Can't make a switchover of failover if we have
		// less than two instances
		return nil
	}

	// Set targetPrimary to do a failover if needed
	if !status.Items[0].IsPrimary {
		log.Info("Current primary isn't healthy, failing over",
			"newPrimary", status.Items[0].PodName,
			"clusterStatus", status)
		r.Recorder.Eventf(cluster, "Normal", "FailingOver",
			"Current primary isn't healthy, failing over from %v to %v",
			cluster.Status.TargetPrimary, status.Items[0].PodName)
		if err := r.RegisterPhase(ctx, cluster, v1alpha1.PhaseFailOver,
			fmt.Sprintf("Failing over to %v", status.Items[0].PodName)); err != nil {
			return err
		}
		// No primary, no party. Failover please!
		return r.setPrimaryInstance(ctx, cluster, status.Items[0].PodName)
	}

	// If our primary instance need a restart and all the replicas
	// already are restarted and ready, let's just switchover
	// to a replica to finish the configuration changes
	instancesNeedingRestart := 0
	for _, status := range status.Items {
		if status.PendingRestart {
			instancesNeedingRestart++
		}
	}

	primaryPendingRestart := status.Items[0].PendingRestart
	allInstancesReady := cluster.Status.ReadyInstances == cluster.Spec.Instances
	if instancesNeedingRestart == 1 && primaryPendingRestart && allInstancesReady {
		log.Info("current primary is needing a restart and the replicas "+
			"are ready, switching over to complete configuration apply",
			"newPrimary", status.Items[1].PodName,
			"clusterStatus", status)
		r.Recorder.Eventf(cluster, "Normal", "SwitchingOver",
			"Current primary %v is needing a restart and the replicas "+
				"are ready, switching over to %v to complete configuration apply",
			cluster.Status.TargetPrimary, status.Items[1].PodName)
		if err := r.RegisterPhase(ctx, cluster, v1alpha1.PhaseFailOver,
			fmt.Sprintf("Switching over to %v to complete configuration apply",
				status.Items[1].PodName)); err != nil {
			return err
		}
		return r.setPrimaryInstance(ctx, cluster, status.Items[1].PodName)
	}

	return nil
}

// getStatusFromInstances get the replication status from the PostgreSQL instances,
// the returned list is sorted in order to have the primary as the first element
// and the other instances in their election order
func (r *ClusterReconciler) getStatusFromInstances(
	ctx context.Context,
	pods corev1.PodList,
) (postgres.PostgresqlStatusList, error) {
	// Only work on Pods which can still become active in the future
	filteredPods := utils.FilterActivePods(pods.Items)
	if len(filteredPods) == 0 {
		// No instances to control
		return postgres.PostgresqlStatusList{}, nil
	}

	status, err := r.extractInstancesStatus(ctx, filteredPods)
	if err != nil {
		return postgres.PostgresqlStatusList{}, err
	}

	sort.Sort(&status)
	return status, nil
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

	timeout := time.Second * 2
	config := controllerruntime.GetConfigOrDie()
	clientInterface := kubernetes.NewForConfigOrDie(config)
	stdout, _, err := utils.ExecCommand(
		ctx,
		clientInterface,
		config,
		pod,
		specs.PostgresContainerName,
		&timeout,
		"/controller/manager", "instance", "status")

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
	filteredPods []corev1.Pod,
) (postgres.PostgresqlStatusList, error) {
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

		podSerial, err := specs.GetNodeSerial(pod.ObjectMeta)

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

		_, err := specs.GetNodeSerial(pod.ObjectMeta)

		// This isn't one of our Pods, since I can't get the node serial
		if err != nil {
			continue
		}

		return &podList[idx]
	}

	return nil
}
