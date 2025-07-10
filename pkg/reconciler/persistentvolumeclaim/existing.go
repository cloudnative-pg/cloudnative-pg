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

package persistentvolumeclaim

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

type reconciliationUnit func(
	ctx context.Context,
	c client.Client,
	storageConfiguration *apiv1.StorageConfiguration,
	pvc *corev1.PersistentVolumeClaim,
) error

// reconcileExistingPVCs align the existing pvcs to the desired state
func reconcileExistingPVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	if len(pvcs) == 0 {
		return nil
	}

	contextLogger := log.FromContext(ctx)

	var reconciliationUnits []reconciliationUnit

	if cluster.ShouldResizeInUseVolumes() {
		reconciliationUnits = append(reconciliationUnits, reconcilePVCQuantity)
	}
	if cluster.Spec.StorageConfiguration.PersistentVolumeClaimTemplate != nil {
		reconciliationUnits = append(reconciliationUnits, reconcileVolumeAttributeClass)
	}

	if len(reconciliationUnits) == 0 {
		return nil
	}

	for idx := range pvcs {
		pvc := &pvcs[idx]

		pvcRole, err := GetExpectedObjectCalculator(pvc.GetLabels())
		if err != nil {
			contextLogger.Error(err,
				"encountered an error while trying to get pvc role from label",
				"role", pvc.Labels[utils.PvcRoleLabelName],
			)
			return err
		}

		storageConfiguration, err := pvcRole.GetStorageConfiguration(cluster)
		if err != nil {
			contextLogger.Error(err,
				"encountered an error while trying to obtain the storage configuration",
				"role", pvc.Labels[utils.PvcRoleLabelName],
				"pvcName", pvc.Name,
			)
			return err
		}

		for _, reconciler := range reconciliationUnits {
			if err := reconciler(ctx, c, &storageConfiguration, pvc); err != nil {
				return err
			}
		}
	}

	return nil
}

func reconcileVolumeAttributeClass(
	ctx context.Context,
	c client.Client,
	storageConfiguration *apiv1.StorageConfiguration,
	pvc *corev1.PersistentVolumeClaim,
) error {
	if storageConfiguration.PersistentVolumeClaimTemplate == nil {
		return nil
	}

	expectedVolumeAttributesClassName := storageConfiguration.PersistentVolumeClaimTemplate.VolumeAttributesClassName
	if expectedVolumeAttributesClassName == pvc.Spec.VolumeAttributesClassName {
		return nil
	}

	oldPVC := pvc.DeepCopy()
	pvc.Spec.VolumeAttributesClassName = expectedVolumeAttributesClassName
	if err := c.Patch(ctx, pvc, client.MergeFrom(oldPVC)); err != nil {
		return fmt.Errorf("error while changing PVC volume attributes class name: %w", err)
	}

	return nil
}

func reconcilePVCQuantity(
	ctx context.Context,
	c client.Client,
	storageConfiguration *apiv1.StorageConfiguration,
	pvc *corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)

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
		return fmt.Errorf("error while changing PVC storage requirement: %w", err)
	}

	return nil
}
