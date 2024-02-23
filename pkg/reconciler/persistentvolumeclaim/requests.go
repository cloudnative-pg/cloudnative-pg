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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	apiv1resources "github.com/cloudnative-pg/cloudnative-pg/api/v1/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
)

// reconcileResourceRequests align the resource requests
func reconcileResourceRequests(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	if !cluster.ShouldResizeInUseVolumes() {
		return nil
	}

	for idx := range pvcs {
		if err := reconcilePVCQuantity(ctx, c, cluster, &pvcs[idx]); err != nil {
			return err
		}
	}

	return nil
}

func reconcilePVCQuantity(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvc *corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)
	pvcRole, err := GetExpectedObjectCalculator(pvc.GetLabels())
	if err != nil {
		contextLogger.Error(err,
			"encountered an error while trying to get pvc role from label",
			"role", pvc.Labels[apiv1resources.PvcRoleLabelName],
		)
		return err
	}

	storageConfiguration, err := pvcRole.GetStorageConfiguration(cluster)
	if err != nil {
		contextLogger.Error(err,
			"encountered an error while trying to obtain the storage configuration",
			"role", pvc.Labels[apiv1resources.PvcRoleLabelName],
			"pvcName", pvc.Name,
		)
		return err
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
