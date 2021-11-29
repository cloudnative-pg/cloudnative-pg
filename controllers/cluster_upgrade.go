/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	neturl "net/url"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/executablehash"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

func (r *ClusterReconciler) rolloutDueToCondition(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList *postgres.PostgresqlStatusList,
	conditionFunc func(postgres.PostgresqlStatus, *apiv1.Cluster) (bool, string),
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	// The following code works under the assumption that podList.Items list is ordered
	// by lag (primary first)

	// upgrade all the replicas starting from the more lagged
	var primaryPostgresqlStatus *postgres.PostgresqlStatus
	for i := len(podList.Items) - 1; i >= 0; i-- {
		postgresqlStatus := podList.Items[i]

		// If this pod is the current primary, we upgrade it in the last step
		if cluster.Status.CurrentPrimary == postgresqlStatus.Pod.Name {
			primaryPostgresqlStatus = &podList.Items[i]
			continue
		}

		shouldRestart, reason := conditionFunc(postgresqlStatus, cluster)
		if !shouldRestart {
			continue
		}

		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseUpgrade,
			fmt.Sprintf("Restarting instance %s, because: %s", postgresqlStatus.Pod.Name, reason),
		); err != nil {
			return false, fmt.Errorf("postgresqlStatus pod name: %s, %w", postgresqlStatus.Pod.Name, err)
		}

		return true, r.upgradePod(ctx, cluster, &postgresqlStatus.Pod)
	}

	// report an error if there is no primary. This condition should never happen because
	// `updateTargetPrimaryFromPods()` is executed before this function
	if primaryPostgresqlStatus == nil {
		return false, fmt.Errorf("expected 1 primary PostgreSQL but none found")
	}

	shouldRestart, reason := conditionFunc(*primaryPostgresqlStatus, cluster)
	if !shouldRestart {
		return false, nil
	}

	// we need to check whether a manual switchover is required
	if cluster.GetPrimaryUpdateStrategy() == apiv1.PrimaryUpdateStrategySupervised {
		contextLogger.Info("Waiting for the user to request a switchover to complete the rolling update",
			"reason", reason, "primaryPod", primaryPostgresqlStatus.Pod.Name)
		err := r.RegisterPhase(ctx, cluster, apiv1.PhaseWaitingForUser, "User must issue a supervised switchover")
		if err != nil {
			return false, err
		}

		return true, nil
	}

	// if the cluster has more than one instance, we should trigger a switchover before upgrading
	if cluster.Status.Instances > 1 && len(podList.Items) > 1 {
		// podList.Items[1] is the first replica, as the pod list
		// is sorted in the same order we use for switchover / failover
		targetPrimary := podList.Items[1].Pod.Name
		contextLogger.Info("The primary needs to be restarted, we'll trigger a switchover to do that",
			"reason", reason,
			"currentPrimary", primaryPostgresqlStatus.Pod.Name,
			"targetPrimary", targetPrimary)
		r.Recorder.Eventf(cluster, "Normal", "SwitchOver",
			"Initiating switchover to %s to upgrade %s", targetPrimary, primaryPostgresqlStatus.Pod.Name)
		return true, r.setPrimaryInstance(ctx, cluster, targetPrimary)
	}

	// if there is only one instance in the cluster, we should upgrade it even if it's a primary
	if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseUpgrade,
		fmt.Sprintf("The primary instance needs to be restarted: %s, reason: %s",
			primaryPostgresqlStatus.Pod.Name, reason),
	); err != nil {
		return false, fmt.Errorf("postgresqlStatus pod name: %s, %w", primaryPostgresqlStatus.Pod.Name, err)
	}

	return true, r.upgradePod(ctx, cluster, &primaryPostgresqlStatus.Pod)
}

// IsPodNeedingRollout checks if a given cluster instance needs a rollout by comparing its actual state
// with its expected state defined inside the cluster struct.
//
// Arguments:
//
// - The status of a postgres cluster instance.
//
// - The cluster struct containing the desired state.
//
// Returns:
//
// - a boolean indicating if a rollout is needed.
//
// - a string indicating the reason of the rollout.
func IsPodNeedingRollout(status postgres.PostgresqlStatus, cluster *apiv1.Cluster) (bool, string) {
	if !status.IsReady {
		return false, ""
	}

	// check if the pod is reporting his instance manager version
	if status.ExecutableHash == "" {
		// This is an old instance manager.
		// We need to replace it with one supporting the online operator upgrade feature
		return true, ""
	}

	if configuration.Current.EnableAzurePVCUpdates {
		for _, pvc := range cluster.Status.ResizingPVC {
			// This code works on the assumption that the PVC have the same name as the pod using it.
			if status.Pod.Name == pvc {
				return true, fmt.Sprintf("rebooting pod to complete resizing %s", pvc)
			}
		}
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

	if !configuration.Current.EnableInstanceManagerInplaceUpdates {
		oldImage, newImage, err = isPodNeedingUpgradedInitContainerImage(status.Pod)
		if err != nil {
			log.Error(err, "while checking if init container image could be upgraded")
			return false, ""
		}

		if newImage != "" {
			return true, fmt.Sprintf("the instance is using an old init container image: %s -> %s",
				oldImage, newImage)
		}
	}

	// Detect changes in the postgres container configuration
	for _, container := range status.Pod.Spec.Containers {
		// we go to the next array element if it isn't the postgres container
		if container.Name != specs.PostgresContainerName {
			continue
		}

		// Check if there is a change in the resource requirements
		if !utils.IsResourceSubset(container.Resources, cluster.Spec.Resources) {
			return true, fmt.Sprintf("resources changed, old: %+v, new: %+v",
				cluster.Spec.Resources,
				container.Resources)
		}
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

	if pgCurrentImageName != targetImageName {
		// We need to apply a different PostgreSQL version
		return pgCurrentImageName, targetImageName, nil
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

// isPodNeedingUpgradedInitContainerImage checks whether an image in init container has to be changed
func isPodNeedingUpgradedInitContainerImage(
	pod v1.Pod,
) (oldImage string, targetImage string, err error) {
	opCurrentImageName, err := specs.GetBootstrapControllerImageName(pod)
	if err != nil {
		return "", "", err
	}

	if opCurrentImageName != configuration.Current.OperatorImageName {
		// We need to apply a different version of the instance manager
		return opCurrentImageName, configuration.Current.OperatorImageName, nil
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

// upgradeInstanceManager upgrades the instance managers of the Pod running in this cluster
func (r *ClusterReconciler) upgradeInstanceManager(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList *postgres.PostgresqlStatusList,
) error {
	contextLogger := log.FromContext(ctx)

	// If we have an instance manager which is not reporting its hash code
	// we could have:
	//
	// 1. an instance manager which doesn't support automatic update
	// 2. an instance manager which isn't working
	//
	// In both ways, we are skipping this automatic update and we rely
	// on the rollout strategy
	for i := len(podList.Items) - 1; i >= 0; i-- {
		postgresqlStatus := podList.Items[i]
		instanceManagerHash := postgresqlStatus.ExecutableHash

		if instanceManagerHash == "" {
			contextLogger.Debug("Detected a non reporting instance manager, proceeding with rolling update",
				"pod", postgresqlStatus.Pod.Name)
			// We continue in the synchronization loop, leading
			// to a rollout of the new instance manager
			return nil
		}
	}

	operatorHash, err := executablehash.Get()
	if err != nil {
		return err
	}

	// We start upgrading the instance managers we have
	for i := len(podList.Items) - 1; i >= 0; i-- {
		postgresqlStatus := podList.Items[i]
		instanceManagerHash := postgresqlStatus.ExecutableHash
		instanceManagerIsUpgrading := postgresqlStatus.IsInstanceManagerUpgrading
		if instanceManagerHash != "" && instanceManagerHash != operatorHash && !instanceManagerIsUpgrading {
			// We need to upgrade this Pod
			contextLogger.Info("Upgrading instance manager",
				"pod", postgresqlStatus.Pod.Name,
				"oldVersion", postgresqlStatus.ExecutableHash)
			err = upgradeInstanceManagerOnPod(ctx, postgresqlStatus.Pod)
			if err != nil {
				enrichedError := fmt.Errorf("while upgrading instance manager on %s (hash: %s): %w",
					postgresqlStatus.Pod.Name,
					operatorHash[:6],
					err)

				r.Recorder.Event(cluster, "Warning", "InstanceManagerUpgradeFailed",
					fmt.Sprintf("Error %s", enrichedError))
				return enrichedError
			}

			message := fmt.Sprintf("Instance manager has been upgraded on %s (hash: %s)",
				postgresqlStatus.Pod.Name,
				operatorHash[:6])

			r.Recorder.Event(cluster, "Normal", "InstanceManagerUpgraded", message)
			contextLogger.Info(message)
		}
	}

	return nil
}

// upgradeInstanceManagerOnPod upgrades an instance manager of a Pod via an HTTP PUT request.
func upgradeInstanceManagerOnPod(ctx context.Context, pod v1.Pod) error {
	binaryFileStream, err := executablehash.Stream()
	if err != nil {
		return err
	}
	defer func() {
		err = binaryFileStream.Close()
	}()

	updateURL := url.Build(pod.Status.PodIP, url.PathUpdate, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, updateURL, nil)
	req.Body = binaryFileStream
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err.(*neturl.Error).Err, io.EOF) {
			// This is perfectly fine as the instance manager will
			// synchronously update and this call won't return.
			return nil
		}

		return err
	}

	if resp.StatusCode == http.StatusOK {
		// This should not happen. See previous block.
		return nil
	}

	var body []byte
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = resp.Body.Close()
	if err != nil {
		return err
	}

	return fmt.Errorf(string(body))
}
