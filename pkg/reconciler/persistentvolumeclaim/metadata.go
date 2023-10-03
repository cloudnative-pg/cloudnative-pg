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
	"strconv"

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
	cluster *apiv1.Cluster,
	runningInstances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	for _, pod := range runningInstances {
		podRole, podHasRole := utils.GetInstanceRole(pod.ObjectMeta.Labels)
		podSerial, podSerialErr := specs.GetNodeSerial(pod.ObjectMeta)
		if podSerialErr != nil {
			return podSerialErr
		}

		instanceReconciler := metadataReconciler{
			name: "instance-inheritance",
			isUpToDate: func(pvc *corev1.PersistentVolumeClaim) bool {
				if podHasRole && pvc.ObjectMeta.Labels[utils.ClusterRoleLabelName] != podRole {
					return false
				}
				if podHasRole && pvc.ObjectMeta.Labels[utils.ClusterInstanceRoleLabelName] != podRole {
					return false
				}

				if serial, err := specs.GetNodeSerial(pvc.ObjectMeta); err != nil || serial != podSerial {
					return false
				}

				return true
			},
			update: func(pvc *corev1.PersistentVolumeClaim) {
				utils.SetInstanceRole(pvc.ObjectMeta, podRole)

				if pvc.Annotations == nil {
					pvc.Annotations = map[string]string{}
				}

				pvc.Annotations[utils.ClusterSerialAnnotationName] = strconv.Itoa(podSerial)
			},
		}

		// todo: this should not rely on expected cluster instance pvc but should fetch every possible pvc name
		instancePVCs := filterByInstanceExpectedPVCs(cluster, pod.Name, pvcs)
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
	runningInstances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	if err := reconcileMetadataComingFromInstance(ctx, c, cluster, runningInstances, pvcs); err != nil {
		return fmt.Errorf("cannot update role labels on pvcs: %w", err)
	}

	if err := newAnnotationReconciler(cluster).reconcile(ctx, c, pvcs); err != nil {
		return fmt.Errorf("cannot update annotations on pvcs: %w", err)
	}

	if err := newLabelReconciler(cluster).reconcile(ctx, c, pvcs); err != nil {
		return fmt.Errorf("cannot update cluster labels on pvcs: %w", err)
	}

	return nil
}

func newAnnotationReconciler(cluster *apiv1.Cluster) metadataReconciler {
	return metadataReconciler{
		name: "annotations",
		isUpToDate: func(pvc *corev1.PersistentVolumeClaim) bool {
			return utils.IsAnnotationSubset(pvc.Annotations,
				cluster.Annotations,
				cluster.GetFixedInheritedAnnotations(),
				configuration.Current)
		},
		update: func(pvc *corev1.PersistentVolumeClaim) {
			utils.InheritAnnotations(&pvc.ObjectMeta, cluster.Annotations,
				cluster.GetFixedInheritedAnnotations(), configuration.Current)
		},
	}
}

func newLabelReconciler(cluster *apiv1.Cluster) metadataReconciler { //nolint: gocognit
	return metadataReconciler{
		name: "labels",
		isUpToDate: func(pvc *corev1.PersistentVolumeClaim) bool {
			if !utils.IsLabelSubset(pvc.Labels,
				cluster.Labels,
				cluster.GetFixedInheritedLabels(),
				configuration.Current) {
				return false
			}

			pvcRole := utils.PVCRole(pvc.Labels[utils.PvcRoleLabelName])
			for _, instanceName := range cluster.Status.InstanceNames {
				var found bool
				if pvc.Name == GetName(instanceName, utils.PVCRolePgData) {
					found = true
					if pvcRole != utils.PVCRolePgData {
						return false
					}
				}

				if pvc.Name == GetName(instanceName, utils.PVCRolePgWal) {
					found = true
					if pvcRole != utils.PVCRolePgWal {
						return false
					}
				}

				if found && pvc.Labels[utils.InstanceNameLabelName] != instanceName {
					return false
				}
			}

			return true
		},
		update: func(pvc *corev1.PersistentVolumeClaim) {
			utils.InheritLabels(&pvc.ObjectMeta, cluster.Labels, cluster.GetFixedInheritedLabels(), configuration.Current)

			pvcRole := utils.PVCRole(pvc.Labels[utils.PvcRoleLabelName])
			for _, instanceName := range cluster.Status.InstanceNames {
				var found bool
				if pvc.Name == GetName(instanceName, utils.PVCRolePgData) {
					found = true
					if pvcRole != utils.PVCRolePgData {
						pvc.Labels[utils.PvcRoleLabelName] = string(utils.PVCRolePgData)
					}
				}

				if pvc.Name == GetName(instanceName, utils.PVCRolePgWal) {
					found = true
					if pvcRole != utils.PVCRolePgWal {
						pvc.Labels[utils.PvcRoleLabelName] = string(utils.PVCRolePgWal)
					}
				}

				if found {
					pvc.Labels[utils.InstanceNameLabelName] = instanceName
					break
				}
			}
		},
	}
}
