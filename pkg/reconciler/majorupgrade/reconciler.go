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

package majorupgrade

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/extensions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/imagecatalog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// ErrIncoherentMajorUpgradeJob is raised when the major upgrade job
// is missing the target image
var ErrIncoherentMajorUpgradeJob = fmt.Errorf("major upgrade job is missing the target image")

// ErrNoPrimaryPVCFound is raised when the list of PVCs doesn't
// include any primary instance.
var ErrNoPrimaryPVCFound = fmt.Errorf("no primary PVC found")

// Reconcile implements the major version upgrade logic.
func Reconcile(
	ctx context.Context,
	c client.Client,
	recorder record.EventRecorder,
	cluster *apiv1.Cluster,
	instances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
	jobs []batchv1.Job,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if majorUpgradeJob := getMajorUpdateJob(jobs); majorUpgradeJob != nil {
		if utils.JobHasOneCompletion(*majorUpgradeJob) {
			return majorVersionUpgradeHandleCompletion(ctx, c, cluster, majorUpgradeJob, pvcs)
		}

		if result, err := handleRollbackIfNeeded(ctx, c, recorder, cluster, majorUpgradeJob); result != nil || err != nil {
			return result, err
		}

		contextLogger.Info("Major upgrade job not completed.")
		return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	requestedMajor, err := cluster.GetPostgresqlMajorVersion()
	if err != nil {
		contextLogger.Error(err, "Unable to retrieve the requested PostgreSQL version")
		return nil, err
	}
	if cluster.Status.PGDataImageInfo == nil || requestedMajor <= cluster.Status.PGDataImageInfo.MajorVersion {
		return nil, clearStaleUpgradeTarget(ctx, c, cluster)
	}

	primaryNodeSerial, err := getPrimarySerial(pvcs)
	if err != nil || primaryNodeSerial == 0 {
		contextLogger.Error(err, "Unable to retrieve the primary node serial")
		return nil, err
	}

	contextLogger.Info("Reconciling in-place major version upgrades",
		"primaryNodeSerial", primaryNodeSerial, "requestedMajor", requestedMajor)

	// Resolve the target-major extensions upfront so we can fail before
	// touching pods if the catalog is missing an entry, and so we can
	// persist the resolved list atomically with the phase transition.
	extensions, err := resolveExtensionsForMajorVersion(ctx, c, cluster, requestedMajor)
	if err != nil {
		contextLogger.Error(err, "Unable to resolve extensions for new major version",
			"requestedMajor", requestedMajor)

		if regErr := registerPhase(
			ctx,
			c,
			cluster,
			apiv1.PhaseImageCatalogError,
			fmt.Sprintf("Cannot resolve extensions for major upgrade to version %d: %v", requestedMajor, err),
		); regErr != nil {
			contextLogger.Error(regErr, "Unable to register phase after extension resolution failure")
		}

		return nil, fmt.Errorf("cannot resolve extensions for major upgrade to version %d: %w",
			requestedMajor, err)
	}

	if err := status.PatchWithOptimisticLock(
		ctx,
		c,
		cluster,
		status.SetPhase(apiv1.PhaseMajorUpgrade,
			fmt.Sprintf("Upgrading cluster to major version %v", requestedMajor)),
		status.SetClusterReadyCondition,
		status.SetTargetPGDataImageInfo(&apiv1.ImageInfo{
			Image:        cluster.Status.Image,
			MajorVersion: requestedMajor,
			Extensions:   extensions,
		}),
	); err != nil {
		return nil, err
	}

	if result, err := deleteAllPodsInMajorUpgradePreparation(ctx, c, instances, jobs); err != nil {
		contextLogger.Error(err, "Unable to delete pods and jobs in preparation for major upgrade")
		return nil, err
	} else if result != nil {
		return result, err
	}

	if result, err := createMajorUpgradeJob(ctx, c, cluster, primaryNodeSerial, extensions); err != nil {
		contextLogger.Error(err, "Unable to create major upgrade job")
		return nil, err
	} else if result != nil {
		return result, err
	}

	return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// clearStaleUpgradeTarget reverts any artifacts left from a major-upgrade
// attempt that the user has rolled back before the upgrade Job was created.
// reconcileImage Case 3 sets Status.Image to the upgrade target without
// touching PGDataImageInfo; on revert reconcileImage compares the spec
// against PGDataImageInfo (now matching) and short-circuits at Case 2,
// leaving Status.Image stale. This mirrors the reset that
// handleRollbackIfNeeded performs once the Job exists.
//
// The function issues no API call when there is nothing to clear.
func clearStaleUpgradeTarget(ctx context.Context, c client.Client, cluster *apiv1.Cluster) error {
	var transactions []status.Transaction
	if cluster.Status.TargetPGDataImageInfo != nil {
		transactions = append(transactions, status.SetTargetPGDataImageInfo(nil))
	}
	if cluster.Status.PGDataImageInfo != nil &&
		cluster.Status.Image != cluster.Status.PGDataImageInfo.Image {
		transactions = append(transactions, status.SetImage(cluster.Status.PGDataImageInfo.Image))
	}
	if len(transactions) == 0 {
		return nil
	}
	return status.PatchWithOptimisticLock(ctx, c, cluster, transactions...)
}

func getMajorUpdateJob(items []batchv1.Job) *batchv1.Job {
	for _, job := range items {
		if isMajorUpgradeJob(&job) {
			return &job
		}
	}

	return nil
}

func deleteAllPodsInMajorUpgradePreparation(
	ctx context.Context,
	c client.Client,
	instances []corev1.Pod,
	jobs []batchv1.Job,
) (*ctrl.Result, error) {
	foundSomethingToDelete := false
	waitingForDeletion := false

	for _, pod := range instances {
		if pod.GetDeletionTimestamp() != nil {
			waitingForDeletion = true
			continue
		}

		foundSomethingToDelete = true
		if err := c.Delete(ctx, &pod); err != nil {
			if !apierrs.IsNotFound(err) {
				return nil, err
			}
		}
	}

	for _, job := range jobs {
		if job.GetDeletionTimestamp() != nil {
			waitingForDeletion = true
			continue
		}

		foundSomethingToDelete = true
		if err := c.Delete(ctx, &job, &client.DeleteOptions{
			PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
		}); err != nil {
			if !apierrs.IsNotFound(err) {
				return nil, err
			}
		}
	}

	if foundSomethingToDelete || waitingForDeletion {
		return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return nil, nil
}

func createMajorUpgradeJob(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	primaryNodeSerial int,
	extensions []apiv1.ExtensionConfiguration,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	job := createMajorUpgradeJobDefinition(cluster, primaryNodeSerial, extensions)

	if err := ctrl.SetControllerReference(cluster, job, c.Scheme()); err != nil {
		contextLogger.Error(err, "Unable to set the owner reference for major upgrade job")
		return nil, err
	}

	utils.SetOperatorVersion(&job.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&job.ObjectMeta, cluster.Annotations,
		cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritAnnotations(&job.Spec.Template.ObjectMeta, cluster.Annotations,
		cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritLabels(&job.ObjectMeta, cluster.Labels,
		cluster.GetFixedInheritedLabels(), configuration.Current)
	utils.InheritLabels(&job.Spec.Template.ObjectMeta, cluster.Labels,
		cluster.GetFixedInheritedLabels(), configuration.Current)
	utils.SetInstanceRole(&job.Spec.Template.ObjectMeta, specs.ClusterRoleLabelPrimary)

	contextLogger.Info("Creating new major upgrade Job",
		"jobName", job.Name,
		"primary", true)

	if err := c.Create(ctx, job); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// This Job was already created, maybe the cache is stale.
			return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return nil, err
	}

	return nil, nil
}

func majorVersionUpgradeHandleCompletion(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	job *batchv1.Job,
	pvcs []corev1.PersistentVolumeClaim,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	for _, pvc := range pvcs {
		if pvc.GetDeletionTimestamp() != nil {
			continue
		}

		if specs.IsPrimary(pvc.ObjectMeta) {
			continue
		}

		if err := c.Delete(ctx, &pvc); err != nil {
			// Ignore if NotFound, otherwise report the error
			if !apierrs.IsNotFound(err) {
				return nil, err
			}
		}
	}

	jobImage, ok := getTargetImageFromMajorUpgradeJob(job)
	if !ok {
		return nil, ErrIncoherentMajorUpgradeJob
	}

	requestedMajor, err := cluster.GetPostgresqlMajorVersion()
	if err != nil {
		contextLogger.Error(err, "Unable to retrieve the requested PostgreSQL version")
		return nil, err
	}

	// Resolve extensions for the new major version.
	// This ensures that extension images match the upgraded PostgreSQL version.
	// If extension resolution fails (e.g., catalog doesn't have extensions for new version),
	// we fail the major upgrade completion to avoid leaving the cluster in an inconsistent state.
	exts, err := resolveExtensionsForMajorVersion(ctx, c, cluster, requestedMajor)
	if err != nil {
		contextLogger.Error(err, "Unable to resolve extensions for upgraded PostgreSQL version",
			"requestedMajor", requestedMajor)

		// Set the cluster phase to indicate image catalog error
		if regErr := registerPhase(
			ctx,
			c,
			cluster,
			apiv1.PhaseImageCatalogError,
			fmt.Sprintf("Cannot resolve extensions after major upgrade to version %d: %v", requestedMajor, err),
		); regErr != nil {
			contextLogger.Error(regErr, "Unable to register phase after extension resolution failure")
		}

		return nil, fmt.Errorf("cannot resolve extensions after major upgrade to version %d: %w",
			requestedMajor, err)
	}

	// Reset timeline ID to 1 after major upgrade to match pg_upgrade behavior.
	// This prevents replicas from restoring incompatible timeline history files
	// from the pre-upgrade cluster in object storage.
	if err := status.PatchWithOptimisticLock(
		ctx,
		c,
		cluster,
		status.SetPGDataImageInfo(&apiv1.ImageInfo{
			Image:        jobImage,
			MajorVersion: requestedMajor,
			Extensions:   exts,
		}),
		status.SetTargetPGDataImageInfo(nil),
		status.SetTimelineID(1),
	); err != nil {
		contextLogger.Error(err, "Unable to update cluster status after major upgrade completed.")
		return nil, err
	}

	if err := c.Delete(ctx, job, &client.DeleteOptions{
		PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
	}); err != nil {
		contextLogger.Error(err, "Unable to delete major upgrade job.")
		return nil, err
	}

	return &ctrl.Result{Requeue: true}, nil
}

// handleRollbackIfNeeded checks whether the user rolled back the image
// while the upgrade job is still running (or has failed). If the requested
// major version is no longer higher than PGDataImageInfo.MajorVersion,
// the job is deleted so the cluster can restart on the old version.
//
// NOTE: this function runs regardless of job status, including while the
// job is still actively executing. If the user reverts the image mid-upgrade,
// the running job will be terminated.
func handleRollbackIfNeeded(
	ctx context.Context,
	c client.Client,
	recorder record.EventRecorder,
	cluster *apiv1.Cluster,
	job *batchv1.Job,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	requestedMajor, err := cluster.GetPostgresqlMajorVersion()
	if err != nil {
		contextLogger.Error(err, "Unable to retrieve the requested PostgreSQL version")
		return nil, err
	}

	// Equal major version is also treated as a rollback: the user changed
	// to a same-major image, so the in-progress upgrade is no longer wanted.
	if cluster.Status.PGDataImageInfo == nil || requestedMajor > cluster.Status.PGDataImageInfo.MajorVersion {
		return nil, nil
	}

	contextLogger.Info("Image rolled back during major upgrade, cleaning up the upgrade job",
		"requestedMajor", requestedMajor,
		"pgDataMajor", cluster.Status.PGDataImageInfo.MajorVersion)

	if job.GetDeletionTimestamp() != nil {
		return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if err := c.Delete(ctx, job, &client.DeleteOptions{
		PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
	}); err != nil {
		if !apierrs.IsNotFound(err) {
			return nil, err
		}
	}

	// reconcileImage set Status.Image to the upgrade target but left
	// PGDataImageInfo unchanged. Reset it so pods use the correct image.
	if err := status.PatchWithOptimisticLock(
		ctx,
		c,
		cluster,
		status.SetImage(cluster.Status.PGDataImageInfo.Image),
		status.SetTargetPGDataImageInfo(nil),
	); err != nil {
		contextLogger.Error(err, "Unable to reset status image after rollback")
		return nil, err
	}

	recorder.Eventf(cluster, "Normal", "MajorUpgradeRollback",
		"Cleaned up failed upgrade job, rolling back to major version %d", requestedMajor)

	return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// registerPhase sets a phase into the cluster
func registerPhase(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	phase string,
	reason string,
) error {
	return status.PatchWithOptimisticLock(
		ctx,
		c,
		cluster,
		status.SetPhase(phase, reason),
		status.SetClusterReadyCondition,
	)
}

// resolveExtensionsForMajorVersion resolves the extension configuration for the upgraded major version.
// This function handles both image catalog references and direct image name specifications.
func resolveExtensionsForMajorVersion(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	requestedMajor int,
) ([]apiv1.ExtensionConfiguration, error) {
	// If using imageCatalogRef, resolve extensions from the catalog
	if cluster.Spec.ImageCatalogRef != nil {
		catalog, err := imagecatalog.Get(ctx, c, cluster)
		if err != nil {
			return nil, fmt.Errorf("cannot get catalog: %w", err)
		}

		exts, err := extensions.ResolveFromCatalog(cluster, catalog, requestedMajor)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve extensions from catalog: %w", err)
		}

		return exts, nil
	}

	// If using imageName directly, extensions must be fully specified in cluster spec
	return extensions.ValidateWithoutCatalog(cluster)
}

// getPrimarySerial tries to obtain the primary serial from a group of PVCs
func getPrimarySerial(
	pvcs []corev1.PersistentVolumeClaim,
) (int, error) {
	for _, pvc := range pvcs {
		instanceRole, _ := utils.GetInstanceRole(pvc.Labels)
		if instanceRole != specs.ClusterRoleLabelPrimary {
			continue
		}

		return specs.GetNodeSerial(pvc.ObjectMeta)
	}

	return 0, ErrNoPrimaryPVCFound
}
