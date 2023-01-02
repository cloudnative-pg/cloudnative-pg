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

package pvc

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Reconcile align the PVCs that are backing our cluster with the user specifications
// TODO: this should become the central place to decide the PVC actions
func Reconcile(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
) (ctrl.Result, error) {
	if !cluster.ShouldResizeInUseVolumes() {
		return ctrl.Result{}, nil
	}

	contextLogger := log.FromContext(ctx)

	for idx := range pvcs {
		pvc := &pvcs[idx]
		pvcRole := utils.PVCRole(pvc.Annotations[utils.PvcRoleLabelName])
		storageConfiguration, err := getStorageConfiguration(pvcRole, cluster)
		if err != nil {
			contextLogger.Error(err,
				"encountered an error while trying to obtain the storage configuration",
				"role", pvc.Annotations[utils.PvcRoleLabelName],
				"pvcName", pvc.Name,
			)
		}

		if err := reconcilePVC(ctx, c, pvc, storageConfiguration); err != nil {
			if apierrs.IsConflict(err) {
				contextLogger.Debug("Conflict error while reconciling PVCs", "error", err)
				return ctrl.Result{Requeue: true}, nil
			}
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// TODO: in future we should reconcile if we do an operation. Right now this approach would not work in CNPG
	return ctrl.Result{}, nil
}

func getStorageConfiguration(
	role utils.PVCRole,
	cluster *apiv1.Cluster,
) (*apiv1.StorageConfiguration, error) {
	switch role {
	case utils.PVCRolePgData:
		return &cluster.Spec.StorageConfiguration, nil
	case utils.PVCRolePgWal:
		return cluster.Spec.WalStorage, nil
	default:
		return nil, fmt.Errorf("unknown pvcRole")
	}
}

func reconcilePVC(
	ctx context.Context,
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	storageConfiguration *apiv1.StorageConfiguration,
) error {
	if storageConfiguration == nil {
		return fmt.Errorf("tried to reconcile a PVC without storageConfiguration")
	}

	contextLogger := log.FromContext(ctx)

	if storageConfiguration.PersistentVolumeClaimTemplate != nil {
		contextLogger.Debug(
			"found a pvcTemplate, skipping pvc reconciliation",
			"pvcName", pvc.Name,
			"template", storageConfiguration.PersistentVolumeClaimTemplate,
		)
		return nil
	}

	quantity, err := resource.ParseQuantity(storageConfiguration.Size)
	if err != nil {
		return err
	}

	oldPVC := pvc.DeepCopy()
	oldQuantity, ok := oldPVC.Spec.Resources.Requests["storage"]

	if !ok || oldQuantity.AsDec().Cmp(quantity.AsDec()) == -1 {
		// Increasing storage resources
		oldPVC.Spec.Resources.Requests["storage"] = quantity
		if err := c.Patch(ctx, pvc, client.MergeFrom(oldPVC)); err != nil {
			contextLogger.Error(err, "error while changing PVC storage requirement",
				"from", oldQuantity, "to", quantity,
				"pvcName", pvc.Name)
			return err
		}
	}

	// Decreasing resources is not possible
	contextLogger.Warning("cannot decrease storage requirement",
		"from", oldQuantity,
		"to", quantity,
		"pvcName", pvc.Name,
	)
	return fmt.Errorf("cannot decrease storage requirement")
}
