/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

var (
	// ErrWalReceiversRunning is raised when a new primary server can't be elected
	// because there is a WAL receiver running in our Pod list
	ErrWalReceiversRunning = fmt.Errorf("wal receivers are still running")
)

// updateTargetPrimaryFromPods set the name of the target primary from the Pods status if needed
// this function will returns the name of the new primary selected for promotion
func (r *ClusterReconciler) updateTargetPrimaryFromPods(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status postgres.PostgresqlStatusList,
	resources *managedResources,
) (string, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	// TODO: what if I delete the master with only 2 instances
	if len(status.Items) <= 1 && cluster.Status.Instances <= 1 {
		// Can't make a switchover of failover if we have
		// less than two instances
		return "", nil
	}

	if len(status.Items) == 0 {
		// We have no status to check and we can't make a
		// switchover under those conditions
		return "", nil
	}

	// Set targetPrimary to do a failover if needed
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

func (r *ClusterReconciler) getReplicaStatusFromPod(
	ctx context.Context,
	pod corev1.Pod) (postgres.PostgresqlStatus, error) {
	var result postgres.PostgresqlStatus

	timeout := time.Second * 2
	config := ctrl.GetConfigOrDie()
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
