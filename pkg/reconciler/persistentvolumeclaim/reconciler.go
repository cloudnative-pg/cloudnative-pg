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
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ReconcileExistingResources reconciles the PVC already created
// TODO: in future it should also create the PVCs
func ReconcileExistingResources(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	instances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if err := reconcileOperatorLabels(ctx, c, cluster, instances, pvcs); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update role labels on pvcs: %w", err)
	}

	if err := reconcileClusterLabels(ctx, c, cluster, pvcs); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update cluster labels on pvcs: %w", err)
	}

	if err := reconcileClusterAnnotations(ctx, c, cluster, pvcs); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update annotations on pvcs: %w", err)
	}

	if err := reconcileResourceRequests(ctx, c, cluster, pvcs); err != nil {
		if apierrs.IsConflict(err) {
			contextLogger.Debug("Conflict error while reconciling PVCs", "error", err)
			return ctrl.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

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
		return nil, fmt.Errorf("unknown pvcRole: %s", string(role))
	}
}

func reconcilePVCQuantity(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvc *corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)
	pvcRole := utils.PVCRole(pvc.Labels[utils.PvcRoleLabelName])

	storageConfiguration, err := getStorageConfiguration(pvcRole, cluster)
	if err != nil {
		contextLogger.Error(err,
			"encountered an error while trying to obtain the storage configuration",
			"role", pvc.Labels[utils.PvcRoleLabelName],
			"pvcName", pvc.Name,
		)
		return err
	}

	if storageConfiguration == nil {
		return fmt.Errorf("tried to reconcile a PVC without storageConfiguration")
	}

	newSize := storageConfiguration.GetSizeOrNil()
	if newSize == nil {
		return ErrorInvalidSize
	}

	currentSize := pvc.Spec.Resources.Requests["storage"]

	switch currentSize.AsDec().Cmp(newSize.AsDec()) {
	case 0:
		return nil
	case 1:
		contextLogger.Warning("cannot decrease storage requirement",
			"from", currentSize, "to", newSize,
			"pvcName", pvc.Name)
		return nil
	}

	oldPVC := pvc.DeepCopy()
	// right now we reconcile the metadata in a different set of functions, so it's not needed to do it here
	pvc = resources.NewPersistentVolumeClaimBuilderFromPVC(pvc).
		WithRequests(corev1.ResourceList{"storage": *newSize}).
		Build()

	if err := c.Patch(ctx, pvc, client.MergeFrom(oldPVC)); err != nil {
		contextLogger.Error(err, "error while changing PVC storage requirement",
			"pvcName", pvc.Name,
			"requests", pvc.Spec.Resources.Requests,
			"oldRequests", oldPVC.Spec.Resources.Requests)
		return err
	}

	return nil
}

// reconcileClusterAnnotations we check if we need to add or modify existing annotations specified in the cluster but
// not existing in the PVCs. We do not support the case of removed annotations from the cluster resource.
func reconcileClusterAnnotations(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)

	for i := range pvcs {
		pvc := &pvcs[i]

		// if all the required annotations are already set and with the correct value,
		// we proceed to the next item
		if utils.IsAnnotationSubset(pvc.Annotations,
			cluster.Annotations,
			cluster.GetFixedInheritedLabels(),
			configuration.Current) &&
			utils.IsAnnotationAppArmorPresentInObject(&pvc.ObjectMeta, cluster.Annotations) {
			contextLogger.Debug(
				"Skipping cluster annotations reconciliation, because they are already present on pvc",
				"pvc", pvc.Name,
				"pvcAnnotations", pvc.Annotations,
				"clusterAnnotations", cluster.Annotations,
			)
			continue
		}

		// otherwise, we add the modified/new annotations to the pvc
		patch := client.MergeFrom(pvc.DeepCopy())
		utils.InheritAnnotations(&pvc.ObjectMeta, cluster.Annotations,
			cluster.GetFixedInheritedAnnotations(), configuration.Current)

		contextLogger.Info("Updating cluster annotations on pvc", "pvc", pvc.Name)
		if err := c.Patch(ctx, pvc, patch); err != nil {
			return err
		}
	}

	return nil
}

// reconcileClusterLabels we check if we need to add or modify existing labels specified in the cluster but
// not existing in the PVCs. We do not support the case of removed labels from the cluster resource.
func reconcileClusterLabels(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)

	for i := range pvcs {
		pvc := &pvcs[i]

		// if all the required labels are already set and with the correct value,
		// we proceed to the next item
		if utils.IsLabelSubset(pvc.Labels,
			cluster.Labels,
			cluster.GetFixedInheritedAnnotations(),
			configuration.Current) {
			contextLogger.Debug(
				"Skipping cluster label reconciliation, because they are already present on pvc",
				"pvc", pvc.Name,
				"pvcLabels", pvc.Labels,
				"clusterLabels", cluster.Labels,
			)
			continue
		}

		// otherwise, we add the modified/new labels to the pvc
		patch := client.MergeFrom(pvc.DeepCopy())
		utils.InheritLabels(&pvc.ObjectMeta, cluster.Labels, cluster.GetFixedInheritedLabels(), configuration.Current)

		contextLogger.Debug("Updating cluster labels on pvc", "pvc", pvc.Name)
		if err := c.Patch(ctx, pvc, patch); err != nil {
			return err
		}
		contextLogger.Info("Updated cluster label on pvc", "pvc", pvc.Name)
	}

	return nil
}

// reconcileOperatorLabels ensures that the PVCs have the correct labels
// nolint: gocognit
func reconcileOperatorLabels(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	instances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	for i := range pvcs {
		pvc := &pvcs[i]
		origPvc := pvc.DeepCopy()

		// this is needed, because on older versions pvc.labels could be nil
		if pvc.Labels == nil {
			pvc.Labels = map[string]string{}
		}

		pvcRole := utils.PVCRole(pvc.Labels[utils.PvcRoleLabelName])
		for _, instanceName := range cluster.Status.InstanceNames {
			if pvc.Name == GetName(cluster, instanceName, utils.PVCRolePgData) && pvcRole != utils.PVCRolePgData {
				pvc.Labels[utils.PvcRoleLabelName] = string(utils.PVCRolePgData)
				break
			}
			if pvc.Name == GetName(cluster, instanceName, utils.PVCRolePgWal) && pvcRole != utils.PVCRolePgWal {
				pvc.Labels[utils.PvcRoleLabelName] = string(utils.PVCRolePgWal)
				break
			}
		}

		for _, pod := range instances {
			if IsUsedByPodSpec(pod.Spec, pvc.Name) {
				podRole, podHasRole := pod.ObjectMeta.Labels[specs.ClusterRoleLabelName]

				if podHasRole && pvc.ObjectMeta.Labels[specs.ClusterRoleLabelName] != podRole {
					pvc.Labels[specs.ClusterRoleLabelName] = podRole
				}

				if pvc.ObjectMeta.Labels[utils.InstanceNameLabelName] != pod.Name {
					pvc.ObjectMeta.Labels[utils.InstanceNameLabelName] = pod.Name
				}

				break
			}
		}

		if reflect.DeepEqual(origPvc.Labels, pvc.Labels) {
			continue
		}

		if err := c.Patch(ctx, pvc, client.MergeFrom(origPvc)); err != nil {
			return err
		}
	}

	return nil
}
