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

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// errLogShippingReplicaElected is raised when the pod update process need
// to select a new primary before upgrading the old primary, but the chosen
// instance is not connected via streaming replication
var errLogShippingReplicaElected = errors.New("log shipping replica elected as a new post-switchover primary")

type rolloutReason = string

func (r *ClusterReconciler) rolloutRequiredInstances(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList *postgres.PostgresqlStatusList,
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

		podRollout := isPodNeedingRollout(ctx, postgresqlStatus, cluster)
		if !podRollout.required {
			continue
		}

		restartMessage := fmt.Sprintf("Restarting instance %s, because: %s",
			postgresqlStatus.Pod.Name, podRollout.reason)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseUpgrade, restartMessage); err != nil {
			return false, fmt.Errorf("postgresqlStatus pod name: %s, %w", postgresqlStatus.Pod.Name, err)
		}

		return true, r.upgradePod(ctx, cluster, postgresqlStatus.Pod, restartMessage)
	}

	// report an error if there is no primary. This condition should never happen because
	// `reconcileTargetPrimaryFromPods()` is executed before this function
	if primaryPostgresqlStatus == nil {
		return false, fmt.Errorf("expected 1 primary PostgreSQL but none found")
	}

	// from now on we know we have a primary instance

	if cluster.IsInstanceFenced(primaryPostgresqlStatus.Pod.Name) {
		return false, nil
	}

	// we first check whether a restart is needed given the provided condition
	podRollout := isPodNeedingRollout(ctx, *primaryPostgresqlStatus, cluster)
	if !podRollout.required {
		return false, nil
	}

	// if the primary instance is marked for restart due to hot standby sensitive parameter decrease,
	// it should be restarted by the instance manager itself
	if primaryPostgresqlStatus.PendingRestartForDecrease {
		return false, nil
	}

	return r.updatePrimaryPod(ctx, cluster, podList, *primaryPostgresqlStatus.Pod,
		podRollout.canBeInPlace, podRollout.primaryForceRecreate, podRollout.reason)
}

func (r *ClusterReconciler) updatePrimaryPod(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList *postgres.PostgresqlStatusList,
	primaryPod corev1.Pod,
	inPlacePossible bool,
	forceRecreate bool,
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

	if cluster.GetPrimaryUpdateMethod() == apiv1.PrimaryUpdateMethodRestart || forceRecreate {
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
		targetInstance := podList.Items[1]

		// If this is a replica cluster, the target primary we chose may be
		// the one we're trying to upgrade, as the list isn't sorted. In
		// this case, we promote the first instance of the list
		if targetInstance.Pod.Name == primaryPod.Name {
			targetInstance = podList.Items[0]
		}

		// Before promoting a replica, the instance manager will wait for the WAL receiver
		// process to be down. We're doing that to avoid losing data written on the primary.
		// This protection can work only when the streaming connection is active.
		// Given that, we refuse to promote a replica when the streaming connection
		// is not active.
		if !targetInstance.IsWalReceiverActive {
			contextLogger.Info(
				"chosen new primary is still not connected via streaming replication, "+
					"interrupting the primaryPodUpdate",
				"updateReason", reason,
				"currentPrimary", primaryPod.Name,
				"targetPrimary", targetInstance.Pod.Name,
			)
			return false, errLogShippingReplicaElected
		}

		contextLogger.Info("The primary needs to be restarted, we'll trigger a switchover to do that",
			"reason", reason,
			"currentPrimary", primaryPod.Name,
			"targetPrimary", targetInstance.Pod.Name)
		podList.LogStatus(ctx)
		r.Recorder.Eventf(cluster, "Normal", "Switchover",
			"Initiating switchover to %s to upgrade %s", targetInstance.Pod.Name, primaryPod.Name)
		return true, r.setPrimaryInstance(ctx, cluster, targetInstance.Pod.Name)
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
	if clusterRestart, ok := cluster.Annotations[utils.ClusterRestartAnnotationName]; ok &&
		(primaryPod.Annotations == nil || primaryPod.Annotations[utils.ClusterRestartAnnotationName] != clusterRestart) {
		contextLogger.Info(
			"Setting restart annotation on primary pod as needed",
			"label", utils.ClusterRestartAnnotationName)
		original := primaryPod.DeepCopy()
		if primaryPod.Annotations == nil {
			primaryPod.Annotations = make(map[string]string)
		}
		primaryPod.Annotations[utils.ClusterRestartAnnotationName] = clusterRestart
		if err := r.Client.Patch(ctx, &primaryPod, client.MergeFrom(original)); err != nil {
			return err
		}
	}
	return nil
}

// rollout describes whether a rollout should happen, and if so whether it can
// be done in-place, and what the reason for the rollout is
type rollout struct {
	required             bool
	canBeInPlace         bool
	primaryForceRecreate bool
	reason               string
}

type rolloutChecker func(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (rollout, error)

// isPodNeedingRollout checks if a given cluster instance needs a rollout by comparing its current state
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
// - a rollout object including whether a restart is required, and the reason
func isPodNeedingRollout(
	ctx context.Context,
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) rollout {
	contextLogger := log.FromContext(ctx)
	if !status.IsPodReady || cluster.IsInstanceFenced(status.Pod.Name) || status.MightBeUnavailable {
		return rollout{}
	}
	applyCheckers := func(checkers map[string]rolloutChecker) rollout {
		for message, check := range checkers {
			podRollout, err := check(status, cluster)
			if err != nil {
				contextLogger.Error(err, "while checking if pod needs rollout")
				continue
			}
			if podRollout.required {
				if podRollout.reason == "" {
					podRollout.reason = message
				}
				contextLogger.Info("Pod rollout required", "pod", status.Pod.Name, "reason", podRollout.reason)
				return podRollout
			}
		}
		return rollout{}
	}

	checkers := map[string]rolloutChecker{
		"instance is missing executable hash":  checkHasExecutableHash,
		"pod has missing PVCs":                 checkHasMissingPVCs,
		"pod has PVC requiring resizing":       checkHasResizingPVC,
		"pod projected volume is outdated":     checkProjectedVolumeIsOutdated,
		"pod image is outdated":                checkPodImageIsOutdated,
		"postgres restart required":            checkPostgresPendingRestart,
		"cluster has newer restart annotation": checkClusterHasNewerRestartAnnotation,
	}

	podRollout := applyCheckers(checkers)
	if podRollout.required {
		return podRollout
	}

	// If the pod has a valid PodSpec annotation, that's the final check.
	// If not, we should perform additional legacy checks
	if hasValidPodSpec(status) {
		return applyCheckers(map[string]rolloutChecker{
			"PodSpec is outdated": checkPodSpecIsOutdated,
		})
	}

	// These checks are subsumed by the PodSpec checker
	checkers = map[string]rolloutChecker{
		"pod environment is outdated":    checkPodEnvironmentIsOutdated,
		"pod scheduler is outdated":      checkSchedulerIsOutdated,
		"pod needs updated topology":     checkPodNeedsUpdatedTopology,
		"pod init container is outdated": checkPodInitContainerIsOutdated,
	}
	podRollout = applyCheckers(checkers)
	if podRollout.required {
		return podRollout
	}

	return rollout{}
}

// check if the pod has a valid podSpec
func hasValidPodSpec(status postgres.PostgresqlStatus) bool {
	podSpecAnnotation, hasStoredPodSpec := status.Pod.ObjectMeta.Annotations[utils.PodSpecAnnotationName]
	if !hasStoredPodSpec {
		return false
	}
	err := json.Unmarshal([]byte(podSpecAnnotation), &corev1.PodSpec{})
	return err == nil
}

func checkHasExecutableHash(
	status postgres.PostgresqlStatus,
	_ *apiv1.Cluster,
) (rollout, error) {
	if status.ExecutableHash == "" {
		// This is an old instance manager.
		// We need to replace it with one supporting the online operator upgrade feature
		return rollout{
			required: true,
			reason: fmt.Sprintf("pod '%s' is not reporting the executable hash",
				status.Pod.Name),
		}, nil
	}
	return rollout{}, nil
}

func checkHasResizingPVC(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (rollout, error) {
	if configuration.Current.EnableAzurePVCUpdates {
		for _, pvcName := range cluster.Status.ResizingPVC {
			// This code works on the assumption that the PVC begins with the name of the pod using it.
			if persistentvolumeclaim.BelongToInstance(cluster, status.Pod.Name, pvcName) {
				return rollout{
					required: true,
					reason:   fmt.Sprintf("rebooting pod to complete resizing %s", pvcName),
				}, nil
			}
		}
	}
	return rollout{}, nil
}

func checkPodNeedsUpdatedTopology(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (rollout, error) {
	if reflect.DeepEqual(cluster.Spec.TopologySpreadConstraints, status.Pod.Spec.TopologySpreadConstraints) {
		return rollout{}, nil
	}

	return rollout{
		required: true,
		reason: fmt.Sprintf(
			"pod '%s' does not have up-to-date TopologySpreadConstraints. It needs to match the cluster's constraints.",
			status.Pod.Name,
		),
	}, nil
}

func checkSchedulerIsOutdated(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (rollout, error) {
	if cluster.Spec.SchedulerName == "" || cluster.Spec.SchedulerName == status.Pod.Spec.SchedulerName {
		return rollout{}, nil
	}

	return rollout{
		required: true,
		reason: fmt.Sprintf(
			"scheduler name changed from: '%s', to '%s'",
			status.Pod.Spec.SchedulerName,
			cluster.Spec.SchedulerName,
		),
	}, nil
}

func checkProjectedVolumeIsOutdated(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (rollout, error) {
	isNilOrZero := func(vs *corev1.ProjectedVolumeSource) bool {
		return vs == nil || len(vs.Sources) == 0
	}

	// Check if there is a change in the projected volume configuration
	currentProjectedVolumeConfiguration := getProjectedVolumeConfigurationFromPod(*status.Pod)
	desiredProjectedVolumeConfiguration := cluster.Spec.ProjectedVolumeTemplate.DeepCopy()

	// we do not need to raise a rollout if the desired and current projected volume source equal to zero-length or nil
	if isNilOrZero(desiredProjectedVolumeConfiguration) && isNilOrZero(currentProjectedVolumeConfiguration) {
		return rollout{}, nil
	}

	if desiredProjectedVolumeConfiguration != nil && desiredProjectedVolumeConfiguration.DefaultMode == nil {
		defaultMode := corev1.ProjectedVolumeSourceDefaultMode
		desiredProjectedVolumeConfiguration.DefaultMode = &defaultMode
	}

	if reflect.DeepEqual(currentProjectedVolumeConfiguration, desiredProjectedVolumeConfiguration) {
		return rollout{}, nil
	}

	return rollout{
		required: true,
		reason: fmt.Sprintf("projected volume configuration changed, old: %+v, new: %+v",
			currentProjectedVolumeConfiguration,
			desiredProjectedVolumeConfiguration),
	}, nil
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

func checkPodImageIsOutdated(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (rollout, error) {
	targetImageName := cluster.GetImageName()

	pgCurrentImageName, err := specs.GetPostgresImageName(*status.Pod)
	if err != nil {
		return rollout{}, err
	}

	if pgCurrentImageName == targetImageName {
		return rollout{}, nil
	}

	return rollout{
		required: true,
		reason: fmt.Sprintf("the instance is using a different image: %s -> %s",
			pgCurrentImageName, targetImageName),
	}, nil
}

func checkPodInitContainerIsOutdated(
	status postgres.PostgresqlStatus,
	_ *apiv1.Cluster,
) (rollout, error) {
	if configuration.Current.EnableInstanceManagerInplaceUpdates {
		return rollout{}, nil
	}

	opCurrentImageName, err := specs.GetBootstrapControllerImageName(*status.Pod)
	if err != nil {
		return rollout{}, err
	}

	if opCurrentImageName == configuration.Current.OperatorImageName {
		return rollout{}, nil
	}

	// We need to apply a different version of the instance manager
	return rollout{
		required: true,
		reason: fmt.Sprintf("the instance is using an old init container image: %s -> %s",
			opCurrentImageName, configuration.Current.OperatorImageName),
	}, nil
}

func checkHasMissingPVCs(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (rollout, error) {
	if persistentvolumeclaim.InstanceHasMissingMounts(cluster, status.Pod) {
		return rollout{
			required:             true,
			primaryForceRecreate: true,
			reason:               "attaching a new PVC to the instance Pod",
		}, nil
	}
	return rollout{}, nil
}

func checkClusterHasNewerRestartAnnotation(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (rollout, error) {
	// check if pod needs to be restarted because of some config requiring it
	// or if the cluster have been explicitly restarted
	// If the cluster has been restarted and we are working with a Pod
	// which has not been restarted yet, or restarted at a different
	// time, let's restart it.
	if clusterRestart, ok := cluster.Annotations[utils.ClusterRestartAnnotationName]; ok {
		podRestart := status.Pod.Annotations[utils.ClusterRestartAnnotationName]
		if clusterRestart != podRestart {
			return rollout{
				required:     true,
				reason:       "cluster has been explicitly restarted via annotation",
				canBeInPlace: true,
			}, nil
		}
	}

	return rollout{}, nil
}

func checkPostgresPendingRestart(
	status postgres.PostgresqlStatus,
	_ *apiv1.Cluster,
) (rollout, error) {
	if status.PendingRestart {
		return rollout{
			required:     true,
			reason:       "Postgres needs a restart to apply some configuration changes",
			canBeInPlace: true,
		}, nil
	}

	return rollout{}, nil
}

func checkPodEnvironmentIsOutdated(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (rollout, error) {
	// Check if there is a change in the environment section
	envConfig := specs.CreatePodEnvConfig(*cluster, status.Pod.Name)

	// Use the hash to detect if the environment needs a refresh
	// Deprecated: the PodEnvHashAnnotationName is marked deprecated. When it is
	// eliminated, the fallback code below can still be useful
	podEnvHash, hasPodEnvhash := status.Pod.Annotations[utils.PodEnvHashAnnotationName]
	if hasPodEnvhash {
		if podEnvHash != envConfig.Hash {
			return rollout{
				required: true,
				reason:   "environment variable configuration hash changed",
			}, nil
		}

		return rollout{}, nil
	}

	// Fall back to comparing the container environment configuration
	for _, container := range status.Pod.Spec.Containers {
		// we go to the next array element if it isn't the postgres container
		if container.Name != specs.PostgresContainerName {
			continue
		}

		if !envConfig.IsEnvEqual(container) {
			return rollout{
				required: true,
				reason: fmt.Sprintf("environment variable configuration changed, "+
					"oldEnv: %+v, oldEnvFrom: %+v, newEnv: %+v, newEnvFrom: %+v",
					container.Env,
					container.EnvFrom,
					envConfig.EnvVars,
					envConfig.EnvFrom,
				),
			}, nil
		}

		break
	}

	return rollout{}, nil
}

func checkPodSpecIsOutdated(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (rollout, error) {
	podSpecAnnotation, ok := status.Pod.ObjectMeta.Annotations[utils.PodSpecAnnotationName]
	if !ok {
		return rollout{}, nil
	}

	var storedPodSpec corev1.PodSpec
	err := json.Unmarshal([]byte(podSpecAnnotation), &storedPodSpec)
	if err != nil {
		return rollout{}, fmt.Errorf("while unmarshaling the pod resources annotation: %w", err)
	}
	envConfig := specs.CreatePodEnvConfig(*cluster, status.Pod.Name)
	gracePeriod := int64(cluster.GetMaxStopDelay())
	targetPodSpec := specs.CreateClusterPodSpec(status.Pod.Name, *cluster, envConfig, gracePeriod,
		instance.GetStatusSchemeFromPod(status.Pod) == "https")

	// the bootstrap init-container could change image after an operator upgrade.
	// If in-place upgrades of the instance manager are enabled, we don't need rollout.
	opCurrentImageName, err := specs.GetBootstrapControllerImageName(*status.Pod)
	if err != nil {
		return rollout{}, err
	}
	if opCurrentImageName != configuration.Current.OperatorImageName &&
		!configuration.Current.EnableInstanceManagerInplaceUpdates {
		return rollout{
			required: true,
			reason: fmt.Sprintf("the instance is using an old init container image: %s -> %s",
				opCurrentImageName, configuration.Current.OperatorImageName),
		}, nil
	}

	// from here we don't care about drift in the init containers: avoid checking them
	storedPodSpec.InitContainers = nil
	targetPodSpec.InitContainers = nil

	match, diff := specs.ComparePodSpecs(storedPodSpec, targetPodSpec)
	if !match {
		return rollout{
			required: true,
			reason:   "original and target PodSpec differ in " + diff,
		}, nil
	}

	return rollout{}, nil
}

// upgradePod deletes a Pod to let the operator recreate it using an
// updated definition
func (r *ClusterReconciler) upgradePod(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pod *corev1.Pod,
	reason rolloutReason,
) error {
	log.FromContext(ctx).Info("Recreating instance pod",
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

// upgradeInstanceManager upgrades the instance managers of each Pod running in this cluster
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
	// In both ways, we are skipping this automatic update and relying
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

	// We start upgrading the instance managers we have
	for i := len(podList.Items) - 1; i >= 0; i-- {
		postgresqlStatus := podList.Items[i]
		instanceManagerHash := postgresqlStatus.ExecutableHash
		instanceManagerIsUpgrading := postgresqlStatus.IsInstanceManagerUpgrading

		// Gather the hash of the operator's manager using the current pod architecture.
		// If one of the pods is requesting an architecture that's not present in the
		// operator, that means we've upgraded to an image which doesn't support all
		// the architectures currently being used by this cluster.
		// In this case we force the reconciliation loop to stop, requiring manual
		// intervention.
		targetManager, err := utils.GetAvailableArchitecture(postgresqlStatus.InstanceArch)
		if err != nil {
			contextLogger.Error(err, "encountered an error while upgrading the instance manager")
			if regErr := r.RegisterPhase(
				ctx,
				cluster,
				apiv1.PhaseArchitectureBinaryMissing,
				fmt.Sprintf("encountered an error while upgrading the instance manager: %s", err.Error()),
			); regErr != nil {
				return regErr
			}

			return utils.ErrTerminateLoop
		}
		operatorHash := targetManager.GetHash()

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

			err = r.StatusClient.UpgradeInstanceManager(ctx, postgresqlStatus.Pod, targetManager)
			if err != nil {
				enrichedError := fmt.Errorf("while upgrading instance manager on %s (hash: %s): %w",
					postgresqlStatus.Pod.Name,
					operatorHash[:6],
					err)

				r.Recorder.Event(cluster, "Warning", "InstanceManagerUpgradeFailed",
					fmt.Sprintf("Error %s", enrichedError))
				return enrichedError
			}

			message := fmt.Sprintf("Instance manager has been upgraded on %s (hash: %s — previous hash: %s)",
				postgresqlStatus.Pod.Name,
				operatorHash[:6],
				instanceManagerHash[:6])

			r.Recorder.Event(cluster, "Normal", "InstanceManagerUpgraded", message)
			contextLogger.Info(message)
		}
	}

	return nil
}
