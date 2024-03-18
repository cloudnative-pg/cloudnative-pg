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

package persistentvolumeclaim

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/conditions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// reconcileResourceRequests align the resource requests
func reconcileResourceRequests(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	for idx := range pvcs {
		if err := reconcilePVCQuantity(ctx, c, cluster, &pvcs[idx]); err != nil {
			return err
		}
	}

	return handleDisableResizeInUsePVC(ctx, c, cluster, pvcs)
}

func reconcilePVCQuantity(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvc *corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)

	storageConfiguration, err := getStorageConfiguration(ctx, cluster, pvc)
	if err != nil {
		return nil
	}

	if !storageConfiguration.ShouldResizeInUseVolumes() {
		// skip reconcile storage size and should be done by the user manually
		return nil
	}

	parsedSize := storageConfiguration.GetSizeOrNil()
	if parsedSize == nil {
		return ErrorInvalidSize
	}
	currentSize := pvc.Spec.Resources.Requests["storage"]

	switch currentSize.AsDec().Cmp(parsedSize.AsDec()) {
	case 0:
		return nil
	case 1:
		contextLogger.Warning("cannot decrease storage requirement",
			"from", currentSize, "to", parsedSize,
			"pvcName", pvc.Name)
		return nil
	}

	oldPVC := pvc.DeepCopy()
	// right now we reconcile the metadata in a different set of functions, so it's not needed to do it here
	pvc = resources.NewPersistentVolumeClaimBuilderFromPVC(pvc).
		WithRequests(corev1.ResourceList{"storage": *parsedSize}).
		Build()

	if err := c.Patch(ctx, pvc, client.MergeFrom(oldPVC)); err != nil {
		contextLogger.Error(err, "error while changing PVC storage requirement",
			"pvcName", pvc.Name,
			"pvc", pvc,
			"requests", pvc.Spec.Resources.Requests,
			"oldRequests", oldPVC.Spec.Resources.Requests)
		return err
	}

	return nil
}

func handleDisableResizeInUsePVC(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)

	var (
		unmatchedPVCs      []string
		disableResizeCount int
	)
	for i := range pvcs {
		pvc := pvcs[i]
		storageConfiguration, err := getStorageConfiguration(ctx, cluster, &pvc)
		if err != nil {
			return err
		}

		if storageConfiguration.ShouldResizeInUseVolumes() {
			continue
		}

		disableResizeCount++

		sizeFromConfig := storageConfiguration.GetSizeOrNil().AsDec()
		sizeFromPVC := pvc.Spec.Resources.Requests.Storage().AsDec()
		if sizeFromConfig.Cmp(sizeFromPVC) != 0 {
			unmatchedPVCs = append(unmatchedPVCs, pvc.Name)
		}
	}

	condition := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionPVCResize)).DeepCopy()
	if condition == nil && len(unmatchedPVCs) == 0 {
		return nil
	}

	if condition == nil {
		condition = &metav1.Condition{Type: string(apiv1.ConditionPVCResize)}
	}

	if len(unmatchedPVCs) == 0 {
		condition.Status = metav1.ConditionTrue
		condition.Reason = string(apiv1.ConditionReasonPVCResizeSuccess)
		condition.Message = "No pvcs are needed to be resized manually"
	} else {
		contextLogger.Info(
			"Resizing storage is required by the user manually.",
			"pvcsToBeResized", unmatchedPVCs,
		)
		condition.Status = metav1.ConditionFalse
		condition.Reason = string(apiv1.ConditionReasonPVCResizePending)
		condition.Message = fmt.Sprintf("pvcs %v are needed to be resized manually.", unmatchedPVCs)
	}

	return conditions.Patch(ctx, c, cluster, condition)
}

func getStorageConfiguration(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pvc *corev1.PersistentVolumeClaim) (
	apiv1.StorageConfiguration,
	error,
) {
	contextLogger := log.FromContext(ctx)
	pvcRole, err := GetExpectedObjectCalculator(pvc.GetLabels())
	if err != nil {
		contextLogger.Error(err,
			"encountered an error while trying to get pvc role from label",
			"role", pvc.Labels[utils.PvcRoleLabelName],
		)
		return apiv1.StorageConfiguration{}, err
	}

	storageConfiguration, err := pvcRole.GetStorageConfiguration(cluster)
	if err != nil {
		contextLogger.Error(err,
			"encountered an error while trying to obtain the storage configuration",
			"role", pvc.Labels[utils.PvcRoleLabelName],
			"pvcName", pvc.Name,
		)
		return apiv1.StorageConfiguration{}, err
	}
	return storageConfiguration, nil
}
