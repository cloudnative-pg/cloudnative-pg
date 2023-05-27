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
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/executablehash"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

type rolloutReason = string

func (r *ClusterReconciler) rolloutDueToCondition(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList *postgres.PostgresqlStatusList,
	conditionFunc func(postgres.PostgresqlStatus, *apiv1.Cluster) (bool, bool, string),
) (bool, error) {
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

		if cluster.IsInstanceFenced(postgresqlStatus.Pod.Name) {
			continue
		}

		shouldRestart, _, reason := conditionFunc(postgresqlStatus, cluster)
		if !shouldRestart {
			continue
		}

		restartMessage := fmt.Sprintf("Restarting instance %s, because: %s", postgresqlStatus.Pod.Name, reason)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseUpgrade, restartMessage); err != nil {
			return false, fmt.Errorf("postgresqlStatus pod name: %s, %w", postgresqlStatus.Pod.Name, err)
		}

		return true, r.upgradePod(ctx, cluster, &postgresqlStatus.Pod, restartMessage)
	}

	// report an error if there is no primary. This condition should never happen because
	// `updateTargetPrimaryFromPods()` is executed before this function
	if primaryPostgresqlStatus == nil {
		return false, fmt.Errorf("expected 1 primary PostgreSQL but none found")
	}

	// from now on we know we have a primary instance

	if cluster.IsInstanceFenced(primaryPostgresqlStatus.Pod.Name) {
		return false, nil
	}

	// we first check whether a restart is needed given the provided condition
	shouldRestart, inPlacePossible, reason := conditionFunc(*primaryPostgresqlStatus, cluster)
	if !shouldRestart {
		return false, nil
	}

	// if the primary instance is marked for restart due to hot standby sensitive parameter decrease,
	// it should be restarted by the instance manager itself
	if primaryPostgresqlStatus.PendingRestartForDecrease {
		return false, nil
	}

	return r.updatePrimaryPod(ctx, cluster, podList, primaryPostgresqlStatus.Pod, inPlacePossible, reason)
}

func (r *ClusterReconciler) updatePrimaryPod(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList *postgres.PostgresqlStatusList,
	primaryPod corev1.Pod,
	inPlacePossible bool,
	reason rolloutReason,
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	// we need to check whether a manual switchover is required
	contextLogger = contextLogger.WithValues("primaryPod", primaryPod.Name)
	if cluster.GetPrimaryUpdateStrategy() == apiv1.PrimaryUpdateStrategySupervised {
		contextLogger.Info("Waiting for the user to request a switchover to complete the rolling update",
			"reason", reason)
		err := r.RegisterPhase(ctx, cluster, apiv1.PhaseWaitingForUser, "User must issue a supervised switchover")
		if err != nil {
			return false, err
		}

		return true, nil
	}

	if cluster.GetPrimaryUpdateMethod() == apiv1.PrimaryUpdateMethodRestart {
		if inPlacePossible {
			// In-place restart is possible
			if err := r.updateRestartAnnotation(ctx, cluster, primaryPod); err != nil {
				return false, err
			}
			contextLogger.Info("Restarting primary instance in-place",
				"reason", reason)
			err := r.RegisterPhase(ctx, cluster, apiv1.PhaseInplacePrimaryRestart, reason)
			return err == nil, err
		}
		// The pod needs to be deleted and recreated for the change to be applied
		contextLogger.Info("Restarting primary instance without a switchover first",
			"reason", reason)
		err := r.RegisterPhase(ctx, cluster, apiv1.PhaseInplaceDeletePrimaryRestart, reason)
		if err != nil {
			return false, err
		}
		err = r.upgradePod(ctx, cluster, &primaryPod, reason)
		return err == nil, err
	}

	// if the cluster has more than one instance, we should trigger a switchover before upgrading
	if cluster.Status.Instances > 1 && len(podList.Items) > 1 {
		// If this is not a replica cluster, podList.Items[1] is the first replica,
		// as the pod list is sorted in the same order we use for switchover / failover.
		// This may not be true for replica clusters, where every instance is a replica
		// from the PostgreSQL point-of-view.
		targetPrimary := podList.Items[1].Pod.Name

		// If this is a replica cluster, the target primary we chose may be
		// the one we're trying to upgrade, as the list isn't sorted. In
		// this case, we promote the first instance of the list
		if targetPrimary == primaryPod.Name {
			targetPrimary = podList.Items[0].Pod.Name
		}

		contextLogger.Info("The primary needs to be restarted, we'll trigger a switchover to do that",
			"reason", reason,
			"currentPrimary", primaryPod.Name,
			"targetPrimary", targetPrimary,
			"podList", podList)
		r.Recorder.Eventf(cluster, "Normal", "Switchover",
			"Initiating switchover to %s to upgrade %s", targetPrimary, primaryPod.Name)
		return true, r.setPrimaryInstance(ctx, cluster, targetPrimary)
	}

	// if there is only one instance in the cluster, we should upgrade it even if it's a primary
	if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseUpgrade,
		fmt.Sprintf("The primary instance needs to be restarted: %s, reason: %s",
			primaryPod.Name, reason),
	); err != nil {
		return false, fmt.Errorf("postgresqlStatus for pod %s: %w", primaryPod.Name, err)
	}

	return true, r.upgradePod(ctx, cluster, &primaryPod, reason)
}

func (r *ClusterReconciler) updateRestartAnnotation(
	ctx context.Context,
	cluster *apiv1.Cluster,
	primaryPod corev1.Pod,
) error {
	contextLogger := log.FromContext(ctx)
	if clusterRestart, ok := cluster.Annotations[specs.ClusterRestartAnnotationName]; ok &&
		(primaryPod.Annotations == nil || primaryPod.Annotations[specs.ClusterRestartAnnotationName] != clusterRestart) {
		contextLogger.Info("Setting restart annotation on primary pod as needed", "label", specs.ClusterReloadAnnotationName)
		original := primaryPod.DeepCopy()
		if primaryPod.Annotations == nil {
			primaryPod.Annotations = make(map[string]string)
		}
		primaryPod.Annotations[specs.ClusterRestartAnnotationName] = clusterRestart
		if err := r.Client.Patch(ctx, &primaryPod, client.MergeFrom(original)); err != nil {
			return err
		}
	}
	return nil
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
// - a boolean indicating if an in-place restart is possible
//
// - a string indicating the reason of the rollout.
func IsPodNeedingRollout(status postgres.PostgresqlStatus, cluster *apiv1.Cluster) (
	needsRollout bool,
	inPlacePossible bool,
	reason string,
) {
	if !status.IsPodReady || cluster.IsInstanceFenced(status.Pod.Name) || status.MightBeUnavailable {
		return false, false, ""
	}

	// check if the pod is reporting his instance manager version
	if status.ExecutableHash == "" {
		// This is an old instance manager.
		// We need to replace it with one supporting the online operator upgrade feature
		return true, false, ""
	}

	if configuration.Current.EnableAzurePVCUpdates {
		for _, pvcName := range cluster.Status.ResizingPVC {
			// This code works on the assumption that the PVC begins with the name of the pod using it.
			if persistentvolumeclaim.BelongToInstance(cluster, status.Pod.Name, pvcName) {
				return true, false, fmt.Sprintf("rebooting pod to complete resizing %s", pvcName)
			}
		}
	}

	// Check if there is a change in the projected volume configuration
	if needsUpdate, reason := isPodNeedingUpdateOfProjectedVolume(cluster, status.Pod); needsUpdate {
		return true, false, reason
	}

	// check if the pod requires an image upgrade
	oldImage, newImage, err := isPodNeedingUpgradedImage(cluster, status.Pod)
	if err != nil {
		log.Error(err, "while checking if image could be upgraded")
		return false, false, ""
	}
	if newImage != "" {
		return true, false, fmt.Sprintf("the instance is using an old image: %s -> %s",
			oldImage, newImage)
	}

	if !configuration.Current.EnableInstanceManagerInplaceUpdates {
		oldImage, newImage, err = isPodNeedingUpgradedInitContainerImage(status.Pod)
		if err != nil {
			log.Error(err, "while checking if init container image could be upgraded")
			return false, false, ""
		}

		if newImage != "" {
			return true, false, fmt.Sprintf("the instance is using an old init container image: %s -> %s",
				oldImage, newImage)
		}
	}

	if persistentvolumeclaim.InstanceHasMissingMounts(cluster, &status.Pod) {
		return true, false, string(apiv1.DetachedVolume)
	}

	// Check if there is a change in the environment section
	if restartRequired, reason := isPodNeedingUpdatedEnvironment(*cluster, status.Pod); restartRequired {
		return true, false, reason
	}

	if restartRequired, reason := isPodNeedingUpdatedScheduler(cluster, status.Pod); restartRequired {
		return restartRequired, false, reason
	}

	// Detect changes in the postgres container configuration
	for _, container := range status.Pod.Spec.Containers {
		// we go to the next array element if it isn't the postgres container
		if container.Name != specs.PostgresContainerName {
			continue
		}

		// Check if there is a change in the resource requirements
		if !utils.IsResourceSubset(container.Resources, cluster.Spec.Resources) {
			return true, false, fmt.Sprintf("resources changed, old: %+v, new: %+v",
				cluster.Spec.Resources,
				container.Resources)
		}
	}

	// check if pod needs to be restarted because of some config requiring it
	// or if the cluster have been explicitly restarted
	needingRestart, reason := isPodNeedingRestart(cluster, status)
	return needingRestart, true, reason
}

// isPodNeedingUpdatedScheduler returns a boolean indicating if a restart is required and the relative message
func isPodNeedingUpdatedScheduler(cluster *apiv1.Cluster, pod corev1.Pod) (bool, string) {
	if cluster.Spec.SchedulerName == "" || cluster.Spec.SchedulerName == pod.Spec.SchedulerName {
		return false, ""
	}

	message := fmt.Sprintf(
		"scheduler name changed from: '%s', to '%s'",
		pod.Spec.SchedulerName,
		cluster.Spec.SchedulerName,
	)
	return true, message
}

func isPodNeedingUpdateOfProjectedVolume(cluster *apiv1.Cluster, pod corev1.Pod) (needsUpdate bool, reason string) {
	currentProjectedVolumeConfiguration := getProjectedVolumeConfigurationFromPod(pod)

	desiredProjectedVolumeConfiguration := cluster.Spec.ProjectedVolumeTemplate.DeepCopy()
	if desiredProjectedVolumeConfiguration != nil && desiredProjectedVolumeConfiguration.DefaultMode == nil {
		defaultMode := corev1.ProjectedVolumeSourceDefaultMode
		desiredProjectedVolumeConfiguration.DefaultMode = &defaultMode
	}

	if reflect.DeepEqual(currentProjectedVolumeConfiguration, desiredProjectedVolumeConfiguration) {
		return false, ""
	}

	return true, fmt.Sprintf("projected volume configuration changed, old: %+v, new: %+v",
		currentProjectedVolumeConfiguration,
		desiredProjectedVolumeConfiguration)
}

func getProjectedVolumeConfigurationFromPod(pod corev1.Pod) *corev1.ProjectedVolumeSource {
	for _, volume := range pod.Spec.Volumes {
		if volume.Name != "projected" {
			continue
		}

		return volume.Projected
	}

	return nil
}

// isPodNeedingUpgradedImage checks whether an image in a pod has to be changed
func isPodNeedingUpgradedImage(
	cluster *apiv1.Cluster,
	pod corev1.Pod,
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
	pod corev1.Pod,
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
) (bool, rolloutReason) {
	// If the cluster has been restarted and we are working with a Pod
	// which have not been restarted yet, or restarted in a different
	// time, let's restart it.
	if clusterRestart, ok := cluster.Annotations[specs.ClusterRestartAnnotationName]; ok {
		podRestart := instanceStatus.Pod.Annotations[specs.ClusterRestartAnnotationName]
		if clusterRestart != podRestart {
			return true, "cluster have been explicitly restarted via annotation"
		}
	}

	if instanceStatus.PendingRestart {
		return true, "configuration needs a restart to apply some configuration changes"
	}

	return false, ""
}

func isPodNeedingUpdatedEnvironment(cluster apiv1.Cluster, pod corev1.Pod) (bool, string) {
	envConfig := specs.CreatePodEnvConfig(cluster, pod.Name)

	// Use the hash to detect if the environment needs a refresh
	podEnvHash, hasPodEnvhash := pod.Annotations[utils.PodEnvHashAnnotationName]
	if hasPodEnvhash {
		if podEnvHash != envConfig.Hash {
			return true, "environment variable configuration hash changed"
		}

		return false, ""
	}

	// Fall back to comparing the container environment configuration
	for _, container := range pod.Spec.Containers {
		// we go to the next array element if it isn't the postgres container
		if container.Name != specs.PostgresContainerName {
			continue
		}

		if !envConfig.IsEnvEqual(container) {
			return true, fmt.Sprintf("environment variable configuration changed, "+
				"oldEnv: %+v, oldEnvFrom: %+v, newEnv: %+v, newEnvFrom: %+v",
				container.Env,
				container.EnvFrom,
				envConfig.EnvVars,
				envConfig.EnvFrom,
			)
		}

		break
	}

	return false, ""
}

// upgradePod deletes a Pod to let the operator recreate it using an
// updated definition
func (r *ClusterReconciler) upgradePod(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pod *corev1.Pod,
	reason rolloutReason,
) error {
	log.FromContext(ctx).Info("Deleting old Pod",
		"pod", pod.Name,
		"to", cluster.Spec.ImageName,
		"reason", reason,
	)

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

			if cluster.Status.Phase != apiv1.PhaseOnlineUpgrading {
				err := r.RegisterPhase(ctx, cluster, apiv1.PhaseOnlineUpgrading, "")
				if err != nil {
					return err
				}
			}

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
func upgradeInstanceManagerOnPod(ctx context.Context, pod corev1.Pod) error {
	binaryFileStream, err := executablehash.Stream()
	if err != nil {
		return err
	}
	defer func() {
		err = binaryFileStream.Close()
	}()

	updateURL := url.Build(pod.Status.PodIP, url.PathUpdate, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, updateURL, nil)
	if err != nil {
		return err
	}
	req.Body = binaryFileStream

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
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = resp.Body.Close()
	if err != nil {
		return err
	}

	return fmt.Errorf(string(body))
}
