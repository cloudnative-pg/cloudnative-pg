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
	"strconv"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
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

// reconcileInstanceRoleLabel ensures that the PVCs have the correct instance role label attached to them
func reconcileInstanceRoleLabel(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	if cluster.Status.CurrentPrimary == "" {
		return nil
	}
	for _, instanceName := range cluster.Status.InstanceNames {
		instanceRole := specs.ClusterRoleLabelReplica
		if instanceName == cluster.Status.CurrentPrimary {
			instanceRole = specs.ClusterRoleLabelPrimary
		}

		instanceReconciler := metadataReconciler{
			name: "instance-role",
			isUpToDate: func(pvc *corev1.PersistentVolumeClaim) bool {
				//nolint:staticcheck // still in use for backward compatibility
				if pvc.Labels[utils.ClusterRoleLabelName] != instanceRole {
					return false
				}
				if pvc.Labels[utils.ClusterInstanceRoleLabelName] != instanceRole {
					return false
				}

				return true
			},
			update: func(pvc *corev1.PersistentVolumeClaim) {
				utils.SetInstanceRole(pvc.ObjectMeta, instanceRole)
			},
		}

		// todo: this should not rely on expected cluster instance pvc but should fetch every possible pvc name
		instancePVCs := filterByInstanceExpectedPVCs(cluster, instanceName, pvcs)
		if err := instanceReconciler.reconcile(ctx, c, instancePVCs); err != nil {
			return err
		}
	}
	return nil
}

// ReconcileMetadata a ensures that the pvc metadata is kept up to date
func ReconcileMetadata(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	if err := reconcileInstanceRoleLabel(ctx, c, cluster, pvcs); err != nil {
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

// ReconcileSerialAnnotation ensures that all the PVCs have the correct serial annotation
func ReconcileSerialAnnotation(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	runningInstances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	for _, pod := range runningInstances {
		podSerial, podSerialErr := specs.GetNodeSerial(pod.ObjectMeta)
		if podSerialErr != nil {
			return podSerialErr
		}

		instanceReconciler := metadataReconciler{
			name: "serial",
			isUpToDate: func(pvc *corev1.PersistentVolumeClaim) bool {
				if serial, err := specs.GetNodeSerial(pvc.ObjectMeta); err != nil || serial != podSerial {
					return false
				}

				return true
			},
			update: func(pvc *corev1.PersistentVolumeClaim) {
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

			// Check common labels
			commonLabels := []string{
				utils.KubernetesAppManagedByLabelName,
				utils.KubernetesAppLabelName,
				utils.KubernetesAppComponentLabelName,
			}
			for _, label := range commonLabels {
				if _, found := pvc.Labels[label]; !found {
					return false
				}
			}

			pvcRole := pvc.Labels[utils.PvcRoleLabelName]
			for _, instanceName := range cluster.Status.InstanceNames {
				var found bool
				if pvc.Name == NewPgDataCalculator().GetName(instanceName) {
					found = true
					if pvcRole != string(utils.PVCRolePgData) {
						return false
					}
				}

				if pvc.Name == NewPgWalCalculator().GetName(instanceName) {
					found = true
					if pvcRole != string(utils.PVCRolePgWal) {
						return false
					}
				}

				for _, tbsConfig := range cluster.Spec.Tablespaces {
					if NewPgTablespaceCalculator(tbsConfig.Name).GetName(instanceName) == pvc.Name {
						found = true
						if pvcRole != string(utils.PVCRolePgTablespace) {
							return false
						}

						if pvc.Labels[utils.TablespaceNameLabelName] != tbsConfig.Name {
							return false
						}
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

			// Set common labels
			pvc.Labels[utils.KubernetesAppManagedByLabelName] = utils.ManagerName
			pvc.Labels[utils.KubernetesAppLabelName] = utils.AppName
			pvc.Labels[utils.KubernetesAppComponentLabelName] = utils.DatabaseComponentName

			pvcRole := pvc.Labels[utils.PvcRoleLabelName]
			for _, instanceName := range cluster.Status.InstanceNames {
				var found bool
				if pvc.Name == NewPgDataCalculator().GetName(instanceName) {
					found = true
					if pvcRole != string(utils.PVCRolePgData) {
						pvc.Labels[utils.PvcRoleLabelName] = string(utils.PVCRolePgData)
					}
				}

				if pvc.Name == NewPgWalCalculator().GetName(instanceName) {
					found = true
					if pvcRole != string(utils.PVCRolePgWal) {
						pvc.Labels[utils.PvcRoleLabelName] = string(utils.PVCRolePgWal)
					}
				}

				for _, tbsConfig := range cluster.Spec.Tablespaces {
					if NewPgTablespaceCalculator(tbsConfig.Name).GetName(instanceName) == pvc.Name {
						found = true
						if pvcRole != string(utils.PVCRolePgTablespace) {
							pvc.Labels[utils.PvcRoleLabelName] = string(utils.PVCRolePgTablespace)
						}

						if pvc.Labels[utils.TablespaceNameLabelName] != tbsConfig.Name {
							pvc.Labels[utils.TablespaceNameLabelName] = tbsConfig.Name
						}
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
