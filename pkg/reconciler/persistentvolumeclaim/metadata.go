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
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

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
func reconcileOperatorLabels(
	ctx context.Context,
	c client.Client,
	instances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	for _, pod := range instances {
		podRole, podHasRole := pod.ObjectMeta.Labels[specs.ClusterRoleLabelName]

		instancePVCs := FilterByInstance(pvcs, pod.Spec)
		for i := range instancePVCs {
			pvc := &instancePVCs[i]
			var modified bool
			// this is needed, because on older versions pvc.labels could be nil
			if pvc.Labels == nil {
				pvc.Labels = map[string]string{}
			}

			origPvc := pvc.DeepCopy()
			if podHasRole && pvc.ObjectMeta.Labels[specs.ClusterRoleLabelName] != podRole {
				pvc.Labels[specs.ClusterRoleLabelName] = podRole
				modified = true
			}
			if pvc.ObjectMeta.Labels[utils.InstanceNameLabelName] != pod.Name {
				pvc.ObjectMeta.Labels[utils.InstanceNameLabelName] = pod.Name
				modified = true
			}
			if !modified {
				continue
			}
			if err := c.Patch(ctx, pvc, client.MergeFrom(origPvc)); err != nil {
				return err
			}
		}
	}

	return nil
}
