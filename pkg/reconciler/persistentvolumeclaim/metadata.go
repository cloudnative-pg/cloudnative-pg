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
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

type metadataReconciler struct {
	name       string
	isUpToDate func(pvc *corev1.PersistentVolumeClaim) bool
	update     func(pvc *corev1.PersistentVolumeClaim)
}

func (m metadataReconciler) reconcile(
	ctx context.Context,
	c client.Client,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)

	for i := range pvcs {
		pvc := &pvcs[i]

		if m.isUpToDate(pvc) {
			contextLogger.Trace(
				"Skipping reconciliation, no changes to be done",
				"pvc", pvc.Name,
				"reconciler", m.name,
			)
			continue
		}

		patch := client.MergeFrom(pvc.DeepCopy())
		m.update(pvc)

		contextLogger.Info("Updating pvc metadata", "pvc", pvc.Name, "reconciler", m.name)
		if err := c.Patch(ctx, pvc, patch); err != nil {
			return err
		}
	}

	return nil
}

// reconcileMetadataComingFromInstance ensures that the PVCs have the correct metadata that is inherited by the instance
func reconcileMetadataComingFromInstance(
	ctx context.Context,
	c client.Client,
	instances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	for _, pod := range instances {
		podRole, podHasRole := pod.ObjectMeta.Labels[specs.ClusterRoleLabelName]
		instanceReconciler := metadataReconciler{
			name: "instance-inheritance",
			isUpToDate: func(pvc *corev1.PersistentVolumeClaim) bool {
				return (podHasRole && pvc.ObjectMeta.Labels[specs.ClusterRoleLabelName] != podRole) &&
					pvc.ObjectMeta.Labels[utils.InstanceNameLabelName] != pod.Name
			},
			update: func(pvc *corev1.PersistentVolumeClaim) {
				// this is needed, because on older versions pvc.labels could be nil
				if pvc.Labels == nil {
					pvc.Labels = map[string]string{}
				}
				pvc.Labels[specs.ClusterRoleLabelName] = podRole
				pvc.Labels[utils.InstanceNameLabelName] = pod.Name
			},
		}

		instancePVCs := FilterByInstance(pvcs, pod.Spec)
		if err := instanceReconciler.reconcile(ctx, c, instancePVCs); err != nil {
			return err
		}
	}

	return nil
}

func reconcileMetadata(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	instances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	annotationReconciler := metadataReconciler{
		name: "annotations",
		isUpToDate: func(pvc *corev1.PersistentVolumeClaim) bool {
			return utils.IsAnnotationSubset(pvc.Annotations,
				cluster.Annotations,
				cluster.GetFixedInheritedAnnotations(),
				configuration.Current) &&
				utils.IsAnnotationAppArmorPresentInObject(&pvc.ObjectMeta, cluster.Annotations)
		},
		update: func(pvc *corev1.PersistentVolumeClaim) {
			utils.InheritAnnotations(&pvc.ObjectMeta, cluster.Annotations,
				cluster.GetFixedInheritedAnnotations(), configuration.Current)
		},
	}

	labelReconciler := metadataReconciler{
		name: "labels",
		isUpToDate: func(pvc *corev1.PersistentVolumeClaim) bool {
			return utils.IsLabelSubset(pvc.Labels,
				cluster.Labels,
				cluster.GetFixedInheritedLabels(),
				configuration.Current)
		},
		update: func(pvc *corev1.PersistentVolumeClaim) {
			utils.InheritLabels(&pvc.ObjectMeta, cluster.Labels, cluster.GetFixedInheritedLabels(), configuration.Current)
		},
	}

	if err := reconcileMetadataComingFromInstance(ctx, c, instances, pvcs); err != nil {
		return fmt.Errorf("cannot update role labels on pvcs: %w", err)
	}

	if err := annotationReconciler.reconcile(ctx, c, pvcs); err != nil {
		return fmt.Errorf("cannot update annotations on pvcs: %w", err)
	}

	if err := labelReconciler.reconcile(ctx, c, pvcs); err != nil {
		return fmt.Errorf("cannot update cluster labels on pvcs: %w", err)
	}

	return nil
}
