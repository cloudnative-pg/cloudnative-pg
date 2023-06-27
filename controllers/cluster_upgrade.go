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

		rollout := IsPodNeedingRollout(ctx, postgresqlStatus, cluster)
		if !rollout.Required {
			continue
		}

		restartMessage := fmt.Sprintf("Restarting instance %s, because: %s",
			postgresqlStatus.Pod.Name, rollout.Reason)
		if err := r.RegisterPhase(ctx, cluster, apiv1.PhaseUpgrade, restartMessage); err != nil {
			return false, fmt.Errorf("postgresqlStatus pod name: %s, %w", postgresqlStatus.Pod.Name, err)
		}

		return true, r.upgradePod(ctx, cluster, postgresqlStatus.Pod, restartMessage)
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
	rollout := IsPodNeedingRollout(ctx, *primaryPostgresqlStatus, cluster)
	if !rollout.Required {
		return false, nil
	}

	// if the primary instance is marked for restart due to hot standby sensitive parameter decrease,
	// it should be restarted by the instance manager itself
	if primaryPostgresqlStatus.PendingRestartForDecrease {
		return false, nil
	}

	return r.updatePrimaryPod(ctx, cluster, podList, *primaryPostgresqlStatus.Pod,
		rollout.CanBeInPlace, rollout.Reason)
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

// Rollout describes whether a rollout should happen, and if so whether it can
// be done in-place, and what the reason for the rollout is
type Rollout struct {
	Required     bool
	CanBeInPlace bool
	Reason       string
}

type rolloutChecker func(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (Rollout, error)

// IsPodNeedingRollout checks if a given cluster instance needs a rollout by comparing its current state
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
// - a Rollout object including whether a restart is required, and the reason
func IsPodNeedingRollout(
	ctx context.Context,
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) Rollout {
	contextLogger := log.FromContext(ctx)
	if !status.IsPodReady || cluster.IsInstanceFenced(status.Pod.Name) || status.MightBeUnavailable {
		return Rollout{}
	}

	checkers := map[string]rolloutChecker{
		"missing executable hash":              checkHasExecutableHash,
		"has PVC requiring resizing":           checkHasResizingPVC,
		"projected volume is outdated":         checkProjectedVolumeIsOutdated,
		"resources are outdated":               checkResourcesAreOutdated,
		"pod image is outdated":                checkPodImageIsOutdated,
		"pod init container is outdated":       checkPodInitContainerIsOutdated,
		"has missing PVCs":                     checkHasMissingPVCs,
		"pod environment is outdated":          checkPodEnvironmentIsOutdated,
		"scheduler is outdated":                checkSchedulerIsOutdated,
		"cluster has newer restart annotation": checkClusterHasNewerRestartAnnotation,
		"pod needs updated topology":           checkPodNeedsUpdatedTopology,
	}
	for message, check := range checkers {
		rollout, err := check(status, cluster)
		if err != nil {
			contextLogger.Error(err, "while checking if pod needs rollout")
			continue
		}
		if rollout.Required {
			if rollout.Reason == "" {
				rollout.Reason = message
			}
			return rollout
		}
	}
	return Rollout{}
}

func checkHasExecutableHash(
	status postgres.PostgresqlStatus,
	_ *apiv1.Cluster,
) (Rollout, error) {
	if status.ExecutableHash == "" {
		// This is an old instance manager.
		// We need to replace it with one supporting the online operator upgrade feature
		return Rollout{Required: true}, nil
	}
	return Rollout{}, nil
}

func checkHasResizingPVC(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (Rollout, error) {
	if configuration.Current.EnableAzurePVCUpdates {
		for _, pvcName := range cluster.Status.ResizingPVC {
			// This code works on the assumption that the PVC begins with the name of the pod using it.
			if persistentvolumeclaim.BelongToInstance(cluster, status.Pod.Name, pvcName) {
				return Rollout{
					Required: true,
					Reason:   fmt.Sprintf("rebooting pod to complete resizing %s", pvcName),
				}, nil
			}
		}
	}
	return Rollout{}, nil
}

func checkProjectedVolumeIsOutdated(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (Rollout, error) {
	// Check if there is a change in the projected volume configuration
	currentProjectedVolumeConfiguration := getProjectedVolumeConfigurationFromPod(*status.Pod)

	desiredProjectedVolumeConfiguration := cluster.Spec.ProjectedVolumeTemplate.DeepCopy()
	if desiredProjectedVolumeConfiguration != nil && desiredProjectedVolumeConfiguration.DefaultMode == nil {
		defaultMode := corev1.ProjectedVolumeSourceDefaultMode
		desiredProjectedVolumeConfiguration.DefaultMode = &defaultMode
	}

	if reflect.DeepEqual(currentProjectedVolumeConfiguration, desiredProjectedVolumeConfiguration) {
		return Rollout{}, nil
	}

	return Rollout{
		Required: true,
		Reason: fmt.Sprintf("projected volume configuration changed, old: %+v, new: %+v",
			currentProjectedVolumeConfiguration,
			desiredProjectedVolumeConfiguration),
	}, nil
}

func checkResourcesAreOutdated(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (Rollout, error) {
	res, ok := status.Pod.ObjectMeta.Annotations[utils.PodResourcesAnnotationName]
	if !ok {
		return Rollout{}, nil
	}

	var resources corev1.ResourceRequirements
	err := (&resources).Unmarshal([]byte(res))
	if err != nil {
		return Rollout{}, fmt.Errorf("while unmarshaling the pod resources annotation: %w", err)
	}
	if !reflect.DeepEqual(resources, cluster.Spec.Resources) {
		return Rollout{
			Required: true,
			Reason:   "the instance resources don't match the current cluster spec",
		}, nil
	}

	return Rollout{}, nil
}

func checkPodImageIsOutdated(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (Rollout, error) {
	targetImageName := cluster.GetImageName()

	pgCurrentImageName, err := specs.GetPostgresImageName(*status.Pod)
	if err != nil {
		return Rollout{}, err
	}

	if pgCurrentImageName != targetImageName {
		// We need to apply a different PostgreSQL version
		return Rollout{
			Required: true,
			Reason: fmt.Sprintf("the instance is using an old image: %s -> %s",
				pgCurrentImageName, targetImageName),
		}, nil
	}

	canUpgradeImage, err := postgres.CanUpgrade(pgCurrentImageName, targetImageName)
	if err != nil {
		return Rollout{}, err
	}

	if !canUpgradeImage {
		return Rollout{}, nil
	}

	return Rollout{}, nil
}

func checkPodInitContainerIsOutdated(
	status postgres.PostgresqlStatus,
	_ *apiv1.Cluster,
) (Rollout, error) {
	if !configuration.Current.EnableInstanceManagerInplaceUpdates {
		opCurrentImageName, err := specs.GetBootstrapControllerImageName(*status.Pod)
		if err != nil {
			return Rollout{}, err
		}

		if opCurrentImageName != configuration.Current.OperatorImageName {
			// We need to apply a different version of the instance manager
			return Rollout{
				Required: true,
				Reason: fmt.Sprintf("the instance is using an old init container image: %s -> %s",
					opCurrentImageName, configuration.Current.OperatorImageName),
			}, nil
		}
	}

	return Rollout{}, nil
}

func checkHasMissingPVCs(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (Rollout, error) {
	if persistentvolumeclaim.InstanceHasMissingMounts(cluster, status.Pod) {
		return Rollout{Required: true, Reason: string(apiv1.DetachedVolume)}, nil
	}
	return Rollout{}, nil
}

func checkPodEnvironmentIsOutdated(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (Rollout, error) {
	// Check if there is a change in the environment section
	envConfig := specs.CreatePodEnvConfig(*cluster, status.Pod.Name)

	// Use the hash to detect if the environment needs a refresh
	podEnvHash, hasPodEnvhash := status.Pod.Annotations[utils.PodEnvHashAnnotationName]
	if hasPodEnvhash {
		if podEnvHash != envConfig.Hash {
			return Rollout{
				Required: true,
				Reason:   "environment variable configuration hash changed",
			}, nil
		}

		return Rollout{}, nil
	}

	// Fall back to comparing the container environment configuration
	for _, container := range status.Pod.Spec.Containers {
		// we go to the next array element if it isn't the postgres container
		if container.Name != specs.PostgresContainerName {
			continue
		}

		if !envConfig.IsEnvEqual(container) {
			return Rollout{
				Required: true,
				Reason: fmt.Sprintf("environment variable configuration changed, "+
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

	return Rollout{}, nil
}

func checkSchedulerIsOutdated(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (Rollout, error) {
	if cluster.Spec.SchedulerName == "" || cluster.Spec.SchedulerName == status.Pod.Spec.SchedulerName {
		return Rollout{}, nil
	}

	message := fmt.Sprintf(
		"scheduler name changed from: '%s', to '%s'",
		status.Pod.Spec.SchedulerName,
		cluster.Spec.SchedulerName,
	)
	return Rollout{
		Required: true,
		Reason:   message,
	}, nil
}

func checkClusterHasNewerRestartAnnotation(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (Rollout, error) {
	// check if pod needs to be restarted because of some config requiring it
	// or if the cluster have been explicitly restarted
	// If the cluster has been restarted and we are working with a Pod
	// which has not been restarted yet, or restarted at a different
	// time, let's restart it.
	if clusterRestart, ok := cluster.Annotations[specs.ClusterRestartAnnotationName]; ok {
		podRestart := status.Pod.Annotations[specs.ClusterRestartAnnotationName]
		if clusterRestart != podRestart {
			return Rollout{
				Required:     true,
				Reason:       "cluster has been explicitly restarted via annotation",
				CanBeInPlace: true,
			}, nil
		}
	}

	if status.PendingRestart {
		return Rollout{
			Required:     true,
			Reason:       "configuration needs a restart to apply some configuration changes",
			CanBeInPlace: true,
		}, nil
	}

	return Rollout{}, nil
}

func checkPodNeedsUpdatedTopology(
	status postgres.PostgresqlStatus,
	cluster *apiv1.Cluster,
) (Rollout, error) {
	if reflect.DeepEqual(cluster.Spec.TopologySpreadConstraints, status.Pod.Spec.TopologySpreadConstraints) {
		return Rollout{}, nil
	}
	reason := fmt.Sprintf(
		"Pod '%s' does not have up-to-date TopologySpreadConstraints. It needs to match the cluster's constraints.",
		status.Pod.Name,
	)

	return Rollout{
		Required: true,
		Reason:   reason,
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

			err = upgradeInstanceManagerOnPod(ctx, *postgresqlStatus.Pod)
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
