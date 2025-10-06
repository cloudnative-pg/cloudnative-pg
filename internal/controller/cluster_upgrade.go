/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// errLogShippingReplicaElected is raised when the pod update process need
// to select a new primary before upgrading the old primary, but the chosen
// instance is not connected via streaming replication
var errLogShippingReplicaElected = errors.New("log shipping replica elected as a new post-switchover primary")

// errRolloutDelayed is raised the a pod rollout have been delayed because
// of the operator configuration
var errRolloutDelayed = errors.New("pod rollout delayed")

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

		podRollout := isInstanceNeedingRollout(ctx, postgresqlStatus, cluster)
		if !podRollout.required {
			continue
		}

		// Check if this is a resource-only change that can be applied in-place
		if podRollout.canUseResourceInPlaceUpdate {
			err := r.updatePodResources(ctx, cluster, postgresqlStatus.Pod)
			if err == nil {
				continue
			}
			log.FromContext(ctx).Info("In-place resource update failed, falling back to pod recreation",
				"pod", postgresqlStatus.Pod.Name,
				"error", err.Error())
		}

		managerResult := r.rolloutManager.CoordinateRollout(client.ObjectKeyFromObject(cluster), postgresqlStatus.Pod.Name)
		if !managerResult.RolloutAllowed {
			r.Recorder.Eventf(
				cluster,
				"Normal",
				"RolloutDelayed",
				"Rollout of pod %s have been delayed for %s",
				postgresqlStatus.Pod.Name,
				managerResult.TimeToWait.String(),
			)
			return false, errRolloutDelayed
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
	podRollout := isInstanceNeedingRollout(ctx, *primaryPostgresqlStatus, cluster)
	if !podRollout.required {
		return false, nil
	}

	// if the primary instance is marked for restart due to hot standby sensitive parameter decrease,
	// it should be restarted by the instance manager itself
	if primaryPostgresqlStatus.PendingRestartForDecrease {
		return false, nil
	}

	managerResult := r.rolloutManager.CoordinateRollout(
		client.ObjectKeyFromObject(cluster),
		primaryPostgresqlStatus.Pod.Name)
	if !managerResult.RolloutAllowed {
		r.Recorder.Eventf(
			cluster,
			"Normal",
			"RolloutDelayed",
			"Rollout of pod %s have been delayed for %s",
			primaryPostgresqlStatus.Pod.Name,
			managerResult.TimeToWait.String(),
		)
		return false, errRolloutDelayed
	}

	return r.updatePrimaryPod(ctx, cluster, podList, *primaryPostgresqlStatus.Pod,
		podRollout.canBeInPlace, podRollout.primaryForceRecreate, podRollout.reason, podRollout.canUseResourceInPlaceUpdate)
}

func (r *ClusterReconciler) updatePrimaryPod(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList *postgres.PostgresqlStatusList,
	primaryPod corev1.Pod,
	inPlacePossible bool,
	forceRecreate bool,
	reason rolloutReason,
	canUseResourceInPlaceUpdate bool,
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

	// Check if this is a resource-only change that can be applied in-place for the primary
	if canUseResourceInPlaceUpdate {
		// Try to apply the resource update in-place
		err := r.updatePodResources(ctx, cluster, &primaryPod)
		if err != nil {
			return false, err
		}
		log.FromContext(ctx).Info("In-place resource update successful for primary pod",
			"pod", primaryPod.Name)
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
		if err := r.Patch(ctx, &primaryPod, client.MergeFrom(original)); err != nil {
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

	needsChangeOperatorImage bool
	needsChangeOperandImage  bool

	// Used for in-place resource updates
	canUseResourceInPlaceUpdate bool

	reason string
}

type rolloutChecker func(
	ctx context.Context,
	pod *corev1.Pod,
	cluster *apiv1.Cluster,
) (rollout, error)

func isInstanceNeedingRollout(
	ctx context.Context,
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) rollout {
	if !status.IsPodReady || status.MightBeUnavailable {
		return rollout{}
	}

	if status.ExecutableHash == "" {
		// This is an old instance manager.
		// We need to replace it with one supporting the online operator upgrade feature
		return rollout{
			required: true,
			reason: fmt.Sprintf("pod '%s' is not reporting the executable hash",
				status.Pod.Name),
			needsChangeOperatorImage: true,
		}
	}

	// Check if the pod needs a rollout (including resource-only changes that might be applied in-place)
	if podRollout := isPodNeedingRollout(ctx, status.Pod, cluster); podRollout.required {
		return podRollout
	}

	if status.PendingRestart {
		return rollout{
			required:     true,
			reason:       "Postgres needs a restart to apply some configuration changes",
			canBeInPlace: true,
		}
	}

	return rollout{}
}

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
	pod *corev1.Pod,
	cluster *apiv1.Cluster,
) rollout {
	contextLogger := log.FromContext(ctx)
	applyCheckers := func(checkers map[string]rolloutChecker) rollout {
		for message, check := range checkers {
			podRollout, err := check(ctx, pod, cluster)
			if err != nil {
				contextLogger.Error(err, "while checking if pod needs rollout")
				continue
			}
			if podRollout.required {
				if podRollout.reason == "" {
					podRollout.reason = message
				}
				contextLogger.Info("Pod rollout required", "podName", pod.Name, "reason", podRollout.reason)
				return podRollout
			}
		}
		return rollout{}
	}

	checkers := map[string]rolloutChecker{
		"resource requirements changed":            checkResourceOnlyChanges,
		"pod has missing PVCs":                     checkHasMissingPVCs,
		"pod projected volume is outdated":         checkProjectedVolumeIsOutdated,
		"pod image is outdated":                    checkPodImageIsOutdated,
		"cluster has different restart annotation": checkClusterHasDifferentRestartAnnotation,
	}

	podRollout := applyCheckers(checkers)
	if podRollout.required {
		return podRollout
	}

	// If the cluster is annotated with `cnpg.io/reconcilePodSpec: disabled`,
	// we avoid checking the PodSpec
	if utils.IsPodSpecReconciliationDisabled(&cluster.ObjectMeta) {
		return rollout{}
	}

	// If the pod has a valid PodSpec annotation, that's the final check.
	// If not, we should perform additional legacy checks
	if hasValidPodSpec(pod) {
		return applyCheckers(map[string]rolloutChecker{
			"PodSpec is outdated": checkPodSpecIsOutdated,
		})
	}

	// These checks are subsumed by the PodSpec checker
	checkers = map[string]rolloutChecker{
		"pod environment is outdated":         checkPodEnvironmentIsOutdated,
		"pod scheduler is outdated":           checkSchedulerIsOutdated,
		"pod needs updated topology":          checkPodNeedsUpdatedTopology,
		"pod bootstrap container is outdated": checkPodBootstrapImage,
	}
	podRollout = applyCheckers(checkers)
	if podRollout.required {
		return podRollout
	}

	return rollout{}
}

// checkResourceOnlyChanges checks if there are only resource requirement changes
// that can be applied in-place according to the container resize policy
func checkResourceOnlyChanges(
	ctx context.Context,
	pod *corev1.Pod,
	cluster *apiv1.Cluster,
) (rollout, error) {
	// Find the PostgreSQL container in the current pod
	var currentPostgresContainer *corev1.Container
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == specs.PostgresContainerName {
			currentPostgresContainer = &pod.Spec.Containers[i]
			break
		}
	}

	if currentPostgresContainer == nil {
		return rollout{}, nil
	}

	// Get the desired resources from the cluster spec
	desiredResources := cluster.Spec.Resources

	// Check if resources are actually different
	if equality.Semantic.DeepEqual(currentPostgresContainer.Resources, desiredResources) {
		// Resources are identical, no changes needed
		return rollout{}, nil
	}

	// Track which resources have changed
	cpuChanged := false
	memoryChanged := false

	// Check CPU requests
	currentCPURequest := currentPostgresContainer.Resources.Requests.Cpu()
	desiredCPURequest := desiredResources.Requests.Cpu()
	if !currentCPURequest.Equal(*desiredCPURequest) {
		cpuChanged = true
	}

	// Check CPU limits
	currentCPULimit := currentPostgresContainer.Resources.Limits.Cpu()
	desiredCPULimit := desiredResources.Limits.Cpu()
	if !currentCPULimit.Equal(*desiredCPULimit) {
		cpuChanged = true
	}

	// Check Memory requests
	currentMemoryRequest := currentPostgresContainer.Resources.Requests.Memory()
	desiredMemoryRequest := desiredResources.Requests.Memory()
	if !currentMemoryRequest.Equal(*desiredMemoryRequest) {
		memoryChanged = true
	}

	// Check Memory limits
	currentMemoryLimit := currentPostgresContainer.Resources.Limits.Memory()
	desiredMemoryLimit := desiredResources.Limits.Memory()
	if !currentMemoryLimit.Equal(*desiredMemoryLimit) {
		memoryChanged = true
	}

	// If nothing changed, no rollout needed
	if !cpuChanged && !memoryChanged {
		return rollout{}, nil
	}

	// Check resize policies to see if we can apply changes in-place
	policies := cluster.Spec.ContainerResizePolicy

	// If no policies are defined, we need to recreate the pod (standard rollout)
	if len(policies) == 0 {
		return rollout{
			required:                    true,
			canUseResourceInPlaceUpdate: false,
			reason:                      "resource requirements changed, pod recreation required (no resize policies defined)",
		}, nil
	}

	// Check if CPU changes require restart
	if cpuChanged {
		cpuPolicy := findResizePolicyForResource(policies, corev1.ResourceCPU)
		if cpuPolicy == nil {
			return rollout{
				required:                    true,
				canUseResourceInPlaceUpdate: false,
				reason:                      "CPU resource changed but no resize policy found for CPU",
			}, nil
		}
		if cpuPolicy.RestartPolicy == corev1.RestartContainer {
			return rollout{
				required:                    true,
				canUseResourceInPlaceUpdate: false,
				reason:                      "CPU resource changed but resize policy requires container restart",
			}, nil
		}
	}

	// Check if Memory changes require restart
	if memoryChanged {
		memoryPolicy := findResizePolicyForResource(policies, corev1.ResourceMemory)
		if memoryPolicy == nil {
			return rollout{
				required:                    true,
				canUseResourceInPlaceUpdate: false,
				reason:                      "Memory resource changed but no resize policy found for memory",
			}, nil
		}
		if memoryPolicy.RestartPolicy == corev1.RestartContainer {
			return rollout{
				required:                    true,
				canUseResourceInPlaceUpdate: false,
				reason:                      "Memory resource changed but resize policy requires container restart",
			}, nil
		}
	}

	// All changed resources can be updated in-place
	return rollout{
		required:                    true,
		canUseResourceInPlaceUpdate: true,
		reason:                      "resource requirements changed, can be updated in-place",
	}, nil
}

// check if the pod has a valid podSpec
func hasValidPodSpec(pod *corev1.Pod) bool {
	podSpecAnnotation, hasStoredPodSpec := pod.Annotations[utils.PodSpecAnnotationName]
	if !hasStoredPodSpec {
		return false
	}
	err := json.Unmarshal([]byte(podSpecAnnotation), &corev1.PodSpec{})
	return err == nil
}

func checkPodNeedsUpdatedTopology(_ context.Context, pod *corev1.Pod, cluster *apiv1.Cluster) (rollout, error) {
	if reflect.DeepEqual(cluster.Spec.TopologySpreadConstraints, pod.Spec.TopologySpreadConstraints) {
		return rollout{}, nil
	}

	return rollout{
		required: true,
		reason: fmt.Sprintf(
			"pod '%s' does not have up-to-date TopologySpreadConstraints. It needs to match the cluster's constraints.",
			pod.Name,
		),
	}, nil
}

func checkSchedulerIsOutdated(_ context.Context, pod *corev1.Pod, cluster *apiv1.Cluster) (rollout, error) {
	if cluster.Spec.SchedulerName == "" || cluster.Spec.SchedulerName == pod.Spec.SchedulerName {
		return rollout{}, nil
	}

	return rollout{
		required: true,
		reason: fmt.Sprintf(
			"scheduler name changed from: '%s', to '%s'",
			pod.Spec.SchedulerName,
			cluster.Spec.SchedulerName,
		),
	}, nil
}

func checkProjectedVolumeIsOutdated(_ context.Context, pod *corev1.Pod, cluster *apiv1.Cluster) (rollout, error) {
	isNilOrZero := func(vs *corev1.ProjectedVolumeSource) bool {
		return vs == nil || len(vs.Sources) == 0
	}

	// Check if there is a change in the projected volume configuration
	currentProjectedVolumeConfiguration := getProjectedVolumeConfigurationFromPod(*pod)
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

func checkPodImageIsOutdated(_ context.Context, pod *corev1.Pod, cluster *apiv1.Cluster) (rollout, error) {
	targetImageName := cluster.Status.Image

	pgCurrentImageName, err := specs.GetPostgresImageName(*pod)
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
		needsChangeOperandImage: true,
	}, nil
}

func checkPodBootstrapImage(_ context.Context, pod *corev1.Pod, _ *apiv1.Cluster) (rollout, error) {
	if configuration.Current.EnableInstanceManagerInplaceUpdates {
		return rollout{}, nil
	}

	opCurrentImageName, err := specs.GetBootstrapControllerImageName(*pod)
	if err != nil {
		return rollout{}, err
	}

	if opCurrentImageName == configuration.Current.OperatorImageName {
		return rollout{}, nil
	}

	// We need to apply a different version of the instance manager
	return rollout{
		required: true,
		reason: fmt.Sprintf("the instance is using an old bootstrap container image: %s -> %s",
			opCurrentImageName, configuration.Current.OperatorImageName),
		needsChangeOperatorImage: true,
	}, nil
}

func checkHasMissingPVCs(_ context.Context, pod *corev1.Pod, cluster *apiv1.Cluster) (rollout, error) {
	if persistentvolumeclaim.InstanceHasMissingMounts(cluster, pod) {
		return rollout{
			required:             true,
			primaryForceRecreate: true,
			reason:               "attaching a new PVC to the instance Pod",
		}, nil
	}
	return rollout{}, nil
}

func checkClusterHasDifferentRestartAnnotation(
	_ context.Context,
	pod *corev1.Pod,
	cluster *apiv1.Cluster,
) (rollout, error) {
	// If the pod restart value doesn't match with the one contained in the cluster, restart the pod.
	if clusterRestart, ok := cluster.Annotations[utils.ClusterRestartAnnotationName]; ok {
		podRestart := pod.Annotations[utils.ClusterRestartAnnotationName]
		if clusterRestart != podRestart {
			return rollout{
				required:     true,
				reason:       "cluster has been explicitly restarted via annotation",
				canBeInPlace: false,
			}, nil
		}
	}

	return rollout{}, nil
}

// checkPodEnvironmentIsOutdated checks if the environment variables in the pod have changed.
//
// Deprecated: this function doesn't take into account plugin changes, use PodSpec annotation.
func checkPodEnvironmentIsOutdated(_ context.Context, pod *corev1.Pod, cluster *apiv1.Cluster) (rollout, error) {
	// Check if there is a change in the environment section
	envConfig := specs.CreatePodEnvConfig(*cluster, pod.Name)

	// Use the hash to detect if the environment needs a refresh
	// Deprecated: the PodEnvHashAnnotationName is marked deprecated. When it is
	// eliminated, the fallback code below can still be useful
	podEnvHash, hasPodEnvhash := pod.Annotations[utils.PodEnvHashAnnotationName]
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
	for _, container := range pod.Spec.Containers {
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
	ctx context.Context,
	pod *corev1.Pod,
	cluster *apiv1.Cluster,
) (rollout, error) {
	podSpecAnnotation, ok := pod.Annotations[utils.PodSpecAnnotationName]
	if !ok {
		return rollout{}, nil
	}

	var storedPodSpec corev1.PodSpec
	err := json.Unmarshal([]byte(podSpecAnnotation), &storedPodSpec)
	if err != nil {
		return rollout{}, fmt.Errorf("while unmarshaling the pod resources annotation: %w", err)
	}

	tlsEnabled := remote.GetStatusSchemeFromPod(pod).IsHTTPS()

	serial, err := utils.GetClusterSerialValue(pod.Annotations)
	if err != nil {
		return rollout{}, fmt.Errorf("while getting the pod serial value: %w", err)
	}

	targetPod, err := specs.NewInstance(ctx, *cluster, serial, tlsEnabled)
	if err != nil {
		return rollout{}, fmt.Errorf("while creating a new pod to check podSpec: %w", err)
	}

	// the bootstrap init-container could change image after an operator upgrade.
	// If in-place upgrades of the instance manager are enabled, we don't need rollout.
	opCurrentImageName, err := specs.GetBootstrapControllerImageName(*pod)
	if err != nil {
		return rollout{}, err
	}
	if opCurrentImageName != configuration.Current.OperatorImageName &&
		!configuration.Current.EnableInstanceManagerInplaceUpdates {
		return rollout{
			required: true,
			reason: fmt.Sprintf("the instance is using an old bootstrap container image: %s -> %s",
				opCurrentImageName, configuration.Current.OperatorImageName),
			needsChangeOperatorImage: true,
		}, nil
	}

	match, diff := specs.ComparePodSpecs(storedPodSpec, targetPod.Spec)
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
		"to", cluster.Status.Image,
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

// findResizePolicyForResource finds the resize policy for a specific resource
func findResizePolicyForResource(policies []corev1.ContainerResizePolicy, resourceName corev1.ResourceName) *corev1.ContainerResizePolicy {
	for i, policy := range policies {
		if policy.ResourceName == resourceName {
			return &policies[i]
		}
	}
	return nil
}

// updatePodResources applies in-place resource updates to a Pod when the resize policy allows
// it, avoiding the need to delete and recreate the pod. This is used specifically for resource
// changes (CPU/memory) when the container resize policy permits in-place updates.
func (r *ClusterReconciler) updatePodResources(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pod *corev1.Pod,
) error {

	// Create a patch to update only the container resources
	patchedPod := pod.DeepCopy()

	// Update resources for the PostgreSQL container with the desired resources from cluster spec
	for i, container := range patchedPod.Spec.Containers {
		if container.Name == specs.PostgresContainerName {
			patchedPod.Spec.Containers[i].Resources = cluster.Spec.Resources
			break
		}
	}

	// Use the resize subresource to patch pod resources, similar to:
	// kubectl patch pod <name> --subresource resize --patch '{"spec":{"containers":[{"name":"<name>", "resources":{"requests":{"cpu":"800m"}}}]}}'
	err := r.SubResource("resize").Patch(ctx, patchedPod, client.MergeFrom(pod))
	if err != nil {
		return err
	}

	// IMPORTANT: After a successful in-place resource update, we need to update the pod's
	// PodSpec annotation to reflect the new resources. This prevents the next reconciliation
	// cycle from detecting a mismatch and triggering pod recreation.

	// Update the pod's PodSpec annotation to reflect the new resources
	if err := r.updatePodSpecAnnotationAfterResourceUpdate(ctx, pod, cluster); err != nil {
		// Don't return error here as the resource update was successful; log and continue
		log.FromContext(ctx).Info(
			"failed to update PodSpec annotation after in-place resource update (will retry on next reconciliation)",
			"pod", pod.Name,
			"error", err.Error(),
		)
	}

	return nil
}

// updatePodSpecAnnotationAfterResourceUpdate updates the pod's PodSpec annotation to reflect
// the new resources after a successful in-place resource update. This prevents the next
// reconciliation cycle from detecting a mismatch and triggering pod recreation.
func (r *ClusterReconciler) updatePodSpecAnnotationAfterResourceUpdate(
	ctx context.Context,
	pod *corev1.Pod,
	cluster *apiv1.Cluster,
) error {

	// Get the current pod to ensure we have the latest version
	var currentPod corev1.Pod
	if err := r.Get(ctx, client.ObjectKeyFromObject(pod), &currentPod); err != nil {
		return fmt.Errorf("failed to get current pod: %w", err)
	}

	// Check if the pod has a PodSpec annotation
	podSpecAnnotation, hasAnnotation := currentPod.Annotations[utils.PodSpecAnnotationName]
	if !hasAnnotation {
		return nil
	}

	// Parse the current PodSpec annotation
	var currentPodSpec corev1.PodSpec
	if err := json.Unmarshal([]byte(podSpecAnnotation), &currentPodSpec); err != nil {
		return fmt.Errorf("failed to unmarshal current pod spec annotation: %w", err)
	}

	// Update the resources in the PodSpec for the PostgreSQL container
	updated := false
	for i, container := range currentPodSpec.Containers {
		if container.Name == specs.PostgresContainerName {
			// Update the resources to match the cluster spec
			currentPodSpec.Containers[i].Resources = cluster.Spec.Resources
			updated = true
			break
		}
	}

	if !updated {
		return nil
	}

	// Marshal the updated PodSpec back to JSON
	updatedPodSpecJSON, err := json.Marshal(currentPodSpec)
	if err != nil {
		return fmt.Errorf("failed to marshal updated pod spec: %w", err)
	}

	// Update the pod's annotation
	originalPod := currentPod.DeepCopy()
	if currentPod.Annotations == nil {
		currentPod.Annotations = make(map[string]string)
	}
	currentPod.Annotations[utils.PodSpecAnnotationName] = string(updatedPodSpecJSON)

	// Apply the annotation update
	if err := r.Patch(ctx, &currentPod, client.MergeFrom(originalPod)); err != nil {
		return fmt.Errorf("failed to update pod spec annotation: %w", err)
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

		if instanceManagerIsUpgrading || instanceManagerHash == "" || instanceManagerHash == operatorHash {
			message := fmt.Sprintf("Instance manager will skip upgrade on %s (upgrading: %t) "+
				"(operator hash: %s — instance manager hash: %s)",
				postgresqlStatus.Pod.Name,
				instanceManagerIsUpgrading,
				operatorHash[:6],
				instanceManagerHash[:6])
			contextLogger.Trace(message)
			continue
		}

		// We need to upgrade this Pod
		contextLogger.Info("Upgrading instance manager",
			"pod", postgresqlStatus.Pod.Name,
			"oldHash", instanceManagerHash,
			"newHash", operatorHash)

		if cluster.Status.Phase != apiv1.PhaseOnlineUpgrading {
			err := r.RegisterPhase(ctx, cluster, apiv1.PhaseOnlineUpgrading, "")
			if err != nil {
				return err
			}
		}

		err = r.InstanceClient.UpgradeInstanceManager(ctx, postgresqlStatus.Pod, targetManager)
		if err != nil {
			enrichedError := fmt.Errorf("while upgrading instance manager on %s (hash: %s): %w",
				postgresqlStatus.Pod.Name,
				operatorHash[:6],
				err)

			r.Recorder.Event(cluster, "Warning", "InstanceManagerUpgradeFailed",
				fmt.Sprintf("Error %s", enrichedError))
			return enrichedError
		}

		message := fmt.Sprintf("Instance manager has been upgraded on %s "+
			"(oldHash: %s — newHash: %s)",
			postgresqlStatus.Pod.Name,
			instanceManagerHash[:6],
			operatorHash[:6])

		r.Recorder.Event(cluster, "Normal", "InstanceManagerUpgraded", message)
		contextLogger.Info(message)
	}

	return nil
}
