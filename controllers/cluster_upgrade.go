/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"sort"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/expectations"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/postgres"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/specs"
)

var (
	// ErrorInconsistentClusterStatus is raised when the current cluster has no primary nor
	// the sufficient number of nodes to issue a switchover
	ErrorInconsistentClusterStatus = errors.New("inconsistent cluster status")
)

// updateCluster update a Cluster to a new image, if needed
func (r *ClusterReconciler) upgradeCluster(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	podList v1.PodList, clusterStatus postgres.PostgresqlStatusList,
) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	targetImageName := cluster.GetImageName()

	// Sort sortedPodList in reverse order
	sortedPodList := podList.Items
	sort.Slice(sortedPodList, func(i, j int) bool {
		return sortedPodList[i].Name > sortedPodList[j].Name
	})

	// Ensure we really have an upgrade strategy between the involved versions
	for _, pod := range sortedPodList {
		usedImageName, err := specs.GetPostgreSQLImageName(pod)
		if err != nil {
			log.Error(err, "pod", pod.Name)
			continue
		}

		if usedImageName == targetImageName {
			continue
		}

		if err := r.RegisterPhase(ctx, cluster, v1alpha1.PhaseUpgrade,
			fmt.Sprintf("Upgrading cluster to image: %v", targetImageName)); err != nil {
			return err
		}

		status, err := postgres.CanUpgrade(usedImageName, targetImageName)
		if err != nil {
			log.Error(
				err, "Error checking image versions", "from", usedImageName, "to", targetImageName)
			return r.RegisterPhase(ctx, cluster, v1alpha1.PhaseUpgradeFailed,
				fmt.Sprintf("Upgrade Failed, wrong image version: %v", err))
		}

		if !status {
			log.Info("Can't upgrade between these PostgreSQL versions",
				"from", usedImageName,
				"to", targetImageName,
				"pod", pod.Name)
			return r.RegisterPhase(ctx, cluster,
				v1alpha1.PhaseUpgradeFailed,
				fmt.Sprintf("Upgrade Failed, can't upgrade from %v to %v",
					usedImageName, targetImageName))
		}
	}

	primaryIdx := -1
	for idx, pod := range sortedPodList {
		usedImageName, err := specs.GetPostgreSQLImageName(pod)
		if err != nil {
			log.Error(err, "pod", pod.Name)
			continue
		}

		if usedImageName != targetImageName {
			if cluster.Status.CurrentPrimary == pod.Name {
				// This is the primary, and we cannot upgrade it on the fly
				primaryIdx = idx
			} else {
				pod := pod // pin the variable before taking its reference
				return r.upgradePod(ctx, cluster, &pod)
			}
		}
	}

	if primaryIdx == -1 {
		// The primary has been updated too, everything is OK
		return nil
	}

	// We still need to upgrade the primary server, let's see
	// if the user prefer to do it manually
	if cluster.GetPrimaryUpdateStrategy() == v1alpha1.PrimaryUpdateStrategySupervised {
		log.Info(
			"Waiting for the user to request a switchover to complete the rolling update",
			"primaryPod", sortedPodList[primaryIdx].Name)
		return r.RegisterPhase(ctx, cluster, v1alpha1.PhaseWaitingForUser,
			"User must issue a supervised switchover")
	}

	// Ok, the user wants us to automatically update all
	// the server, so let's switch over
	if len(clusterStatus.Items) < 2 || clusterStatus.Items[1].IsPrimary {
		return ErrorInconsistentClusterStatus
	}

	// Let's switch over to this server
	log.Info("Switching over to a replica to complete the rolling update",
		"oldPrimary", cluster.Status.TargetPrimary,
		"newPrimary", clusterStatus.Items[1].PodName,
		"status", clusterStatus)
	return r.setPrimaryInstance(ctx, cluster, clusterStatus.Items[1].PodName)
}

// updatePod update an instance to a newer image version
func (r *ClusterReconciler) upgradePod(ctx context.Context, cluster *v1alpha1.Cluster, pod *v1.Pod) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	log.Info("Deleting old Pod",
		"pod", pod.Name,
		"to", cluster.Spec.ImageName)

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
