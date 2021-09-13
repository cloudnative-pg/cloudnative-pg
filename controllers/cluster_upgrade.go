/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

func (r *ClusterReconciler) rolloutDueToCondition(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList *postgres.PostgresqlStatusList,
	conditionFunc func(postgres.PostgresqlStatus, *apiv1.Cluster) (bool, string),
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	// The following code works under the assumption that the latest element
	// is the primary and this assumption should be enforced by the
	// `updateTargetPrimaryFromPods` function, which is executed before this

	for i := len(podList.Items) - 1; i >= 0; i-- {
		postgresqlStatus := podList.Items[i]

		shouldRestart, reason := conditionFunc(postgresqlStatus, cluster)
		if !shouldRestart {
			continue
		}

		// if it is not the current primary, we can just upgrade it
		if cluster.Status.CurrentPrimary != postgresqlStatus.Pod.Name {
			if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseUpgrade,
				fmt.Sprintf("Restarting instance, beacause: %s", reason)); err != nil {
				contextLogger.Error(err, "postgresqlStatus", postgresqlStatus.Pod.Name)
				return false, err
			}
			return true, r.upgradePod(ctx, cluster, &postgresqlStatus.Pod)
		}

		// if it's a primary instance, we need to check whether a manual switchover is required
		if cluster.GetPrimaryUpdateStrategy() == apiv1.PrimaryUpdateStrategySupervised {
			contextLogger.Info("Waiting for the user to request a switchover to complete the rolling update",
				"reason", reason, "primaryPod", postgresqlStatus.Pod.Name)
			return true, r.RegisterPhase(ctx, cluster, apiv1.PhaseWaitingForUser,
				"User must issue a supervised switchover")
		}

		// if the cluster has more than one instance, we should trigger a switchover before upgrading the
		if cluster.Status.Instances > 1 && len(podList.Items) > 1 {
			// podList.Items[1] is the first replica, as the pod list
			// is sorted in the same order we use for switchover / failovers
			targetPrimary := podList.Items[1].Pod.Name
			contextLogger.Info("The primary needs to be restarted, we'll trigger a switchover first",
				"reason", reason,
				"currentPrimary", postgresqlStatus.Pod.Name,
				"targetPrimary", targetPrimary)
			return true, r.setPrimaryInstance(ctx, cluster, targetPrimary)
		}

		// if there is only one instance in the cluster, we should upgrade it even if it's a primary
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseUpgrade,
			fmt.Sprintf("The primary instance needs to be restarted: %s, reason: %s",
				postgresqlStatus.Pod.Name, reason)); err != nil {
			contextLogger.Error(err, "postgresqlStatus", postgresqlStatus.Pod.Name)
			return false, err
		}

		return true, r.upgradePod(ctx, cluster, &postgresqlStatus.Pod)
	}

	return false, nil
}

// IsPodNeedingRollout checks whether a given postgres instance has to be rolled out for any reason,
// returning if it does need to and a human readable string explaining the reason for it
func IsPodNeedingRollout(status postgres.PostgresqlStatus, cluster *apiv1.Cluster) (bool, string) {
	if !status.IsReady {
		return false, ""
	}
	// check if the pod requires an image upgrade
	oldImage, newImage, err := isPodNeedingUpgradedImage(cluster, status.Pod)
	if err != nil {
		log.Error(err, "while checking if image could be upgraded")
		return false, ""
	}
	if newImage != "" {
		return true, fmt.Sprintf("the instance is using an old image: %s -> %s",
			oldImage, newImage)
	}

	// check if pod needs to be restarted because of some config requiring it
	return isPodNeedingRestart(cluster, status),
		"configuration needs a restart to apply some configuration changes"
}

// isPodNeedingUpgradedImage checks whether an image in a pod has to be changed
func isPodNeedingUpgradedImage(
	cluster *apiv1.Cluster,
	pod v1.Pod,
) (oldImage string, targetImage string, err error) {
	targetImageName := cluster.GetImageName()

	pgCurrentImageName, err := specs.GetPostgresImageName(pod)
	if err != nil {
		return "", "", err
	}

	opCurrentImageName, err := specs.GetBootstrapControllerImageName(pod)
	if err != nil {
		return "", "", err
	}

	if pgCurrentImageName != targetImageName {
		// We need to apply a different PostgreSQL version
		return pgCurrentImageName, targetImageName, nil
	}

	if opCurrentImageName != configuration.Current.OperatorImageName {
		// We need to apply a different version of the instance manager
		return opCurrentImageName, configuration.Current.OperatorImageName, nil
	}

	canUpgradeImage, err := postgres.CanUpgrade(pgCurrentImageName, targetImageName)
	if err != nil {
		return "", "", err
	}

	if !canUpgradeImage {
		return "", "", nil
	}

	return "", "", nil
}

// isPodNeedingRestart returns true if we need to restart the
// Pod to apply a configuration change or there is a request of restart for the cluster
func isPodNeedingRestart(
	cluster *apiv1.Cluster,
	instanceStatus postgres.PostgresqlStatus,
) bool {
	// If the cluster has been restarted and we are working with a Pod
	// which have not been restarted yet, or restarted in a different
	// time, let's restart it.
	if clusterRestart, ok := cluster.Annotations[specs.ClusterRestartAnnotationName]; ok {
		podRestart := instanceStatus.Pod.Annotations[specs.ClusterRestartAnnotationName]
		if clusterRestart != podRestart {
			return true
		}
	}

	return instanceStatus.PendingRestart
}

// upgradePod updates an instance to a newer image version
func (r *ClusterReconciler) upgradePod(ctx context.Context, cluster *apiv1.Cluster, pod *v1.Pod) error {
	log.FromContext(ctx).Info("Deleting old Pod",
		"pod", pod.Name,
		"to", cluster.Spec.ImageName)

	r.Recorder.Eventf(cluster, "Normal", "UpgradingInstance",
		"Upgrading instance %v", pod.Name)

	// let's wait for this Pod to be recloned or recreated, using the same storage
	if err := r.Delete(ctx, pod); err != nil {
		// ignore if NotFound, otherwise report the error
		if !apierrs.IsNotFound(err) {
			return err
		}
	}

	return nil
}
