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
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

func (r *ClusterReconciler) reconcileInPlaceMajorVersionUpgrades(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if job := getMajorUpdateJob(resources.jobs.Items); job != nil {
		return r.majorVersionUpgradeHandleCompletion(ctx, cluster, *job, resources)
	}

	if cluster.Status.MajorVersionUpgradeFromImage == nil {
		return nil, nil
	}

	desiredVersion, err := cluster.GetPostgresqlVersion()
	if err != nil {
		contextLogger.Error(err, "Unable to retrieve the new PostgreSQL version")
		return nil, err
	}

	_, primaryNodeSerial, err := getNodeSerialsFromPVCs(resources.pvcs.Items)
	if err != nil || primaryNodeSerial == 0 {
		if err == nil {
			err = fmt.Errorf("no primary pvc found")
		}
		contextLogger.Error(err, "Unable to retrieve the primary node serial")
		return nil, err
	}

	contextLogger.Info("Reconciling in-place major version upgrades",
		"primaryNodeSerial", primaryNodeSerial, "desiredVersion", desiredVersion.Major())

	err = r.RegisterPhase(ctx, cluster, apiv1.PhaseMajorUpgrade,
		fmt.Sprintf("Upgrading cluster to major version %v", desiredVersion.Major()))
	if err != nil {
		return nil, err
	}

	if result, err := r.deleteAllPodsInMajorUpgradePreparation(ctx, resources); err != nil {
		contextLogger.Error(err, "Unable to delete pods and jobs in preparation for major upgrade")
		return nil, err
	} else if result != nil {
		return result, err
	}

	if result, err := r.createMajorUpgradeJob(ctx, cluster, primaryNodeSerial); err != nil {
		contextLogger.Error(err, "Unable to create major upgrade job")
		return nil, err
	} else if result != nil {
		return result, err
	}

	return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func getMajorUpdateJob(items []batchv1.Job) *batchv1.Job {
	for _, job := range items {
		if job.GetLabels()[utils.JobRoleLabelName] == string(specs.JobMajorUpgrade) {
			return &job
		}
	}

	return nil
}

func (r *ClusterReconciler) deleteAllPodsInMajorUpgradePreparation(
	ctx context.Context,
	resources *managedResources,
) (*ctrl.Result, error) {
	foundSomethingToDelete := false

	for _, pod := range resources.instances.Items {
		if pod.GetDeletionTimestamp() != nil {
			continue
		}

		foundSomethingToDelete = true
		if err := r.Delete(ctx, &pod); err != nil {
			return nil, err
		}
	}

	for _, job := range resources.jobs.Items {
		if job.GetDeletionTimestamp() != nil {
			continue
		}

		foundSomethingToDelete = true
		if err := r.Delete(ctx, &job, &client.DeleteOptions{
			PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
		}); err != nil {
			return nil, err
		}
	}

	if foundSomethingToDelete {
		return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return nil, nil
}

func (r *ClusterReconciler) createMajorUpgradeJob(
	ctx context.Context,
	cluster *apiv1.Cluster,
	primaryNodeSerial int,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	job := specs.CreateMajorUpgradeJob(cluster, primaryNodeSerial, *cluster.Status.MajorVersionUpgradeFromImage)

	if err := ctrl.SetControllerReference(cluster, job, r.Scheme); err != nil {
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
	utils.SetInstanceRole(job.Spec.Template.ObjectMeta, specs.ClusterRoleLabelPrimary)

	contextLogger.Info("Creating new major upgrade Job",
		"jobName", job.Name,
		"primary", true)

	if err := r.Create(ctx, job); err != nil {
		if errors.IsAlreadyExists(err) {
			// This Job was already created, maybe the cache is stale.
			return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return nil, err
	}

	return nil, nil
}

func (r *ClusterReconciler) majorVersionUpgradeHandleCompletion(
	ctx context.Context,
	cluster *apiv1.Cluster,
	job batchv1.Job,
	resources *managedResources,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if !utils.JobHasOneCompletion(job) {
		contextLogger.Warning("Unexpected state: major upgrade job not completed.")
		return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	for _, pvc := range resources.pvcs.Items {
		if pvc.GetDeletionTimestamp() != nil {
			continue
		}

		if specs.IsPrimary(pvc.ObjectMeta) {
			continue
		}

		if err := r.Delete(ctx, &pvc); err != nil {
			// Ignore if NotFound, otherwise report the error
			if !errors.IsNotFound(err) {
				return nil, err
			}
		}
	}

	jobImage, err := getImageFromMajorUpgradeJob(job)
	if err != nil {
		contextLogger.Error(err, "Unable to retrieve image name from major upgrade job.")
		return &ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if err := status.PatchWithOptimisticLock(
		ctx,
		r.Client,
		cluster,
		status.SetMajorVersionUpgradeFromImage(&jobImage),
	); err != nil {
		contextLogger.Error(err, "Unable to update cluster status after major upgrade completed.")
		return nil, err
	}

	if err := r.Delete(ctx, &job, &client.DeleteOptions{
		PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
	}); err != nil {
		contextLogger.Error(err, "Unable to delete major upgrade job.")
		return nil, err
	}

	return &ctrl.Result{Requeue: true}, nil
}

func getImageFromMajorUpgradeJob(job batchv1.Job) (string, error) {
	for _, container := range job.Spec.Template.Spec.Containers {
		if container.Name == string(specs.JobMajorUpgrade) {
			return container.Image, nil
		}
	}

	return "", fmt.Errorf("container not found")
}
