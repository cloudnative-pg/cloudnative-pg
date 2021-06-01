/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"sort"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/expectations"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

// ErrorInconsistentClusterStatus is raised when the current cluster has no primary nor
// the sufficient number of nodes to issue a switchover
var ErrorInconsistentClusterStatus = errors.New("inconsistent cluster status")

// updateCluster update a Cluster to a new image, if needed
func (r *ClusterReconciler) upgradeCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList v1.PodList,
	clusterStatus postgres.PostgresqlStatusList,
) (string, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	targetImageName := cluster.GetImageName()

	// Sort sortedPodList in reverse order
	sortedPodList := podList.Items
	sort.Slice(sortedPodList, func(i, j int) bool {
		return sortedPodList[i].Name > sortedPodList[j].Name
	})

	// Ensure we really have an upgrade strategy between the involved versions
	upgradePathAvailable, err := r.upgradePathAvailable(ctx, cluster, sortedPodList, targetImageName)
	if err != nil {
		return "", err
	}
	if !upgradePathAvailable {
		return "", nil
	}

	primaryIdx := -1
	standbyIdx := -1
	for idx, pod := range sortedPodList {
		podNeedsRestart, err := isPodNeedingRestart(cluster, clusterStatus, pod)
		if err != nil {
			log.Error(err, "pod", pod.Name)
			continue
		}

		if podNeedsRestart {
			if cluster.Status.CurrentPrimary == pod.Name {
				// This is the primary, and we cannot upgrade it on the fly
				primaryIdx = idx
			} else {
				// Select the standby to upgrade. We can stop here because primaryIdx
				// is only used when all the standbys are already up-to-date
				standbyIdx = idx
				break
			}
		}
	}

	if primaryIdx == -1 && standbyIdx == -1 {
		// Everything is up-to-date
		return "", nil
	}

	if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseUpgrade,
		fmt.Sprintf("Upgrading cluster to image: %v", targetImageName)); err != nil {
		return "", err
	}

	// Update the selected standby
	if standbyIdx != -1 {
		pod := sortedPodList[standbyIdx]
		return pod.Name, r.upgradePod(ctx, cluster, &pod)
	}

	// We still need to upgrade the primary server, let's see
	// if the user prefer to do it manually
	if cluster.GetPrimaryUpdateStrategy() == apiv1.PrimaryUpdateStrategySupervised {
		log.Info(
			"Waiting for the user to request a switchover to complete the rolling update",
			"primaryPod", sortedPodList[primaryIdx].Name)
		return sortedPodList[0].Name, r.RegisterPhase(ctx, cluster, apiv1.PhaseWaitingForUser,
			"User must issue a supervised switchover")
	}

	// Ok, the user want us to automatically update all
	// the servers.
	// If we are working on a single-instance cluster
	// we "just" need to delete the Pod we have, waiting for it to be
	// created again with the same storage.
	if cluster.Spec.Instances == 1 {
		return sortedPodList[0].Name, r.upgradePod(ctx, cluster, &sortedPodList[0])
	}

	// If we have replicas, let's switch over to the most up-to-date and
	// then the procedure will continue with the old primary.
	if len(clusterStatus.Items) < 2 || clusterStatus.Items[1].IsPrimary {
		return "", ErrorInconsistentClusterStatus
	}

	// Let's switch over to this server
	log.Info("Switching over to a replica to complete the rolling update",
		"oldPrimary", cluster.Status.TargetPrimary,
		"newPrimary", clusterStatus.Items[1].PodName,
		"status", clusterStatus)
	r.Recorder.Eventf(cluster, "Normal", "SwitchingOver",
		"Switching over from %v to %v to complete upgrade",
		cluster.Status.TargetPrimary, clusterStatus.Items[1].PodName)
	return sortedPodList[0].Name, r.setPrimaryInstance(ctx, cluster, clusterStatus.Items[1].PodName)
}

// upgradePathAvailable check if we have an available upgrade path to the PostgreSQL version
// whose name in in `targetImageName`
func (r *ClusterReconciler) upgradePathAvailable(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList []v1.Pod,
	targetImageName string) (bool, error) {
	for _, pod := range podList {
		usedImageName, err := specs.GetPostgresImageName(pod)
		if err != nil {
			log.Error(err, "pod", pod.Name)
			continue
		}

		if usedImageName == targetImageName {
			continue
		}

		status, err := postgres.CanUpgrade(usedImageName, targetImageName)
		if err != nil {
			log.Error(
				err, "Error checking image versions", "from", usedImageName, "to", targetImageName)
			_ = r.RegisterPhase(ctx, cluster, apiv1.PhaseUpgradeFailed,
				fmt.Sprintf("Upgrade Failed, wrong image version: %v", err))
			return false, err
		}

		if !status {
			log.Info("Can't upgrade between these PostgreSQL versions",
				"from", usedImageName,
				"to", targetImageName,
				"pod", pod.Name)
			_ = r.RegisterPhase(ctx, cluster,
				apiv1.PhaseUpgradeFailed,
				fmt.Sprintf("Upgrade Failed, can't upgrade from %v to %v",
					usedImageName, targetImageName))
			return false, nil
		}
	}

	return true, nil
}

// isPodNeedingRestart return true if we need to restart the
// Pod to apply a configuration change or a new version of
// PostgreSQL
func isPodNeedingRestart(
	cluster *apiv1.Cluster,
	clusterStatus postgres.PostgresqlStatusList,
	pod v1.Pod) (bool, error) {
	targetImageName := cluster.GetImageName()

	pgCurrentImageName, err := specs.GetPostgresImageName(pod)
	if err != nil {
		return false, err
	}

	opCurrentImageName, err := specs.GetBootstrapControllerImageName(pod)
	if err != nil {
		return false, err
	}

	if pgCurrentImageName != targetImageName {
		// We need to apply a different PostgreSQL version
		return true, nil
	}

	if opCurrentImageName != configuration.Current.OperatorImageName {
		// We need to apply a different version of the instance manager
		return true, nil
	}

	// If the cluster has been restarted and we are working with a Pod
	// which have not been restared yet, or restarted in a different
	// time, let's restart it.
	if clusterRestart, ok := cluster.Annotations[specs.ClusterRestartAnnotationName]; ok {
		podRestart := pod.Annotations[specs.ClusterRestartAnnotationName]
		if clusterRestart != podRestart {
			return true, nil
		}
	}

	for _, entry := range clusterStatus.Items {
		if entry.PodName == pod.Name && entry.PendingRestart {
			// We need to apply a new configuration
			return true, nil
		}
	}

	return false, nil
}

// updatePod update an instance to a newer image version
func (r *ClusterReconciler) upgradePod(ctx context.Context, cluster *apiv1.Cluster, pod *v1.Pod) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	log.Info("Deleting old Pod",
		"pod", pod.Name,
		"to", cluster.Spec.ImageName)

	r.Recorder.Eventf(cluster, "Normal", "UpgradingInstance",
		"Upgrading instance %v", pod.Name)

	// We expect the deletion of the selected Pod
	if err := r.podExpectations.ExpectDeletions(expectations.KeyFunc(cluster), 1); err != nil {
		log.Error(err, "Unable to set podExpectations",
			"key", expectations.KeyFunc(cluster), "dels", 1)
	}

	// Let's wait for this Pod to be recloned or recreated using the same storage
	if err := r.Delete(ctx, pod); err != nil {
		// We cannot observe a deletion if it was not accepted by the server
		r.podExpectations.DeletionObserved(expectations.KeyFunc(cluster))

		// Ignore if NotFound, otherwise report the error
		if !apierrs.IsNotFound(err) {
			return err
		}
	}

	return nil
}
