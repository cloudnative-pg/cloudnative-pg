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
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// FilterByPodSpec returns all the corev1.PersistentVolumeClaim that are used inside the podSpec
func FilterByPodSpec(
	pvcs []corev1.PersistentVolumeClaim,
	instanceSpec corev1.PodSpec,
) []corev1.PersistentVolumeClaim {
	var usedByPodSpec []corev1.PersistentVolumeClaim
	for _, volume := range instanceSpec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}

		for _, pvc := range pvcs {
			if volume.PersistentVolumeClaim.ClaimName == pvc.Name {
				usedByPodSpec = append(usedByPodSpec, pvc)
			}
		}
	}

	return usedByPodSpec
}

// IsUsedByPodSpec checks if the given pod spec is using the PVCs
func IsUsedByPodSpec(podSpec corev1.PodSpec, pvcNames ...string) bool {
external:
	for _, pvcName := range pvcNames {
		for _, volume := range podSpec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
				continue external
			}
		}
		return false
	}
	return true
}

// isResizing returns true if PersistentVolumeClaimResizing condition is present
func isResizing(pvc corev1.PersistentVolumeClaim) bool {
	for _, condition := range pvc.Status.Conditions {
		if condition.Type == corev1.PersistentVolumeClaimResizing {
			return true
		}
	}

	return false
}

// isFileSystemResizePending returns true if PersistentVolumeClaimFileSystemResizePending condition is present.
// This condition indicates the volume has been resized at the storage layer but the filesystem
// resize is pending - it requires a pod to mount the volume to complete the resize.
func isFileSystemResizePending(pvc corev1.PersistentVolumeClaim) bool {
	for _, condition := range pvc.Status.Conditions {
		if condition.Type == corev1.PersistentVolumeClaimFileSystemResizePending {
			return true
		}
	}

	return false
}

// BelongToInstance returns a boolean indicating if that given PVC belongs to an instance
func BelongToInstance(cluster *apiv1.Cluster, instanceName, pvcName string) bool {
	expectedPVCs := getExpectedInstancePVCNamesFromCluster(cluster, instanceName)
	return slices.Contains(expectedPVCs, pvcName)
}

func filterByInstanceExpectedPVCs(
	cluster *apiv1.Cluster,
	instanceName string,
	pvcs []corev1.PersistentVolumeClaim,
) []corev1.PersistentVolumeClaim {
	expectedInstancePVCs := getExpectedInstancePVCNamesFromCluster(cluster, instanceName)
	var belongingPVCs []corev1.PersistentVolumeClaim
	for i := range pvcs {
		pvc := pvcs[i]
		if slices.Contains(expectedInstancePVCs, pvc.Name) {
			belongingPVCs = append(belongingPVCs, pvc)
		}
	}

	return belongingPVCs
}

// getNamesFromPVCList returns a list of PVC names extracted from a list of PVCs
func getNamesFromPVCList(pvcs []corev1.PersistentVolumeClaim) []string {
	pvcNames := make([]string, len(pvcs))
	for i, pvc := range pvcs {
		pvcNames[i] = pvc.Name
	}
	return pvcNames
}

// InstanceHasMissingMounts returns true if the instance has expected PVCs that are not mounted
func InstanceHasMissingMounts(cluster *apiv1.Cluster, instance *corev1.Pod) bool {
	for _, pvcName := range getExpectedInstancePVCNamesFromCluster(cluster, instance.Name) {
		if !IsUsedByPodSpec(instance.Spec, pvcName) {
			return true
		}
	}
	return false
}

type expectedPVC struct {
	calculator    ExpectedObjectCalculator
	name          string
	initialStatus PVCStatus
}

func (e *expectedPVC) toCreateConfiguration(
	serial int,
	storage apiv1.StorageConfiguration,
	source *corev1.TypedLocalObjectReference,
) *CreateConfiguration {
	cc := &CreateConfiguration{
		Status:     e.initialStatus,
		NodeSerial: serial,
		Calculator: e.calculator,
		Storage:    storage,
		Source:     source,
	}

	return cc
}

func getExpectedPVCsFromCluster(cluster *apiv1.Cluster, instanceName string) []expectedPVC {
	roles := []ExpectedObjectCalculator{NewPgDataCalculator()}
	if cluster.ShouldCreateWalArchiveVolume() {
		roles = append(roles, NewPgWalCalculator())
	}
	for _, tbsConfig := range cluster.Spec.Tablespaces {
		roles = append(roles, NewPgTablespaceCalculator(tbsConfig.Name))
	}
	return buildExpectedPVCs(instanceName, roles)
}

// getExpectedInstancePVCNamesFromCluster gets all the PVC names for a given instance
func getExpectedInstancePVCNamesFromCluster(cluster *apiv1.Cluster, instanceName string) []string {
	expectedPVCs := getExpectedPVCsFromCluster(cluster, instanceName)
	expectedPVCNames := make([]string, len(expectedPVCs))
	for idx, mount := range expectedPVCs {
		expectedPVCNames[idx] = mount.name
	}
	return expectedPVCNames
}

// here we should register any new PVC for the instance
func buildExpectedPVCs(instanceName string, roles []ExpectedObjectCalculator) []expectedPVC {
	expectedMounts := make([]expectedPVC, len(roles))

	for i, rl := range roles {
		expectedMounts[i] = expectedPVC{
			name:          rl.GetName(instanceName),
			calculator:    rl,
			initialStatus: rl.GetInitialStatus(),
		}
	}

	return expectedMounts
}

// GetInstancePVCs gets all the PVC associated with a given instance
func GetInstancePVCs(
	ctx context.Context,
	cli client.Client,
	instanceName string,
	namespace string,
) ([]corev1.PersistentVolumeClaim, error) {
	// getPvcList returns the PVCs matching the instance name as well as the role
	getPvcList := func(pvcMeta Meta, instance string) (*corev1.PersistentVolumeClaimList, error) {
		var pvcList corev1.PersistentVolumeClaimList
		matchClusterName := client.MatchingLabels(pvcMeta.GetLabels(instance))
		err := cli.List(ctx,
			&pvcList,
			client.InNamespace(namespace),
			matchClusterName,
		)
		if errors.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &pvcList, nil
	}

	var pvcs []corev1.PersistentVolumeClaim

	pgData, err := getPvcList(NewPgDataCalculator(), instanceName)
	if err != nil {
		return nil, err
	}
	if pgData != nil && len(pgData.Items) > 0 {
		pvcs = append(pvcs, pgData.Items...)
	}

	pgWal, err := getPvcList(NewPgWalCalculator(), instanceName)
	if err != nil {
		return nil, err
	}
	if pgWal != nil && len(pgWal.Items) > 0 {
		pvcs = append(pvcs, pgWal.Items...)
	}

	tablespacesPVClist, err := getPvcList(newTablespaceMetaCalculator(), instanceName)
	if err != nil {
		return nil, err
	}
	if tablespacesPVClist != nil && len(tablespacesPVClist.Items) > 0 {
		pvcs = append(pvcs, tablespacesPVClist.Items...)
	}

	return pvcs, nil
}
