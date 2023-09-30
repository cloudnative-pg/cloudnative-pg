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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// GetName builds the name for a given PVC of the instance
func GetName(instanceName string, role utils.PVCRole) string {
	pvcName := instanceName
	if role == utils.PVCRolePgWal {
		pvcName += apiv1.WalArchiveVolumeSuffix
	}
	return pvcName
}

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
	role          utils.PVCRole
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
		Role:       e.role,
		Storage:    storage,
		Source:     source,
	}

	return cc
}

func getExpectedPVCsFromCluster(cluster *apiv1.Cluster, instanceName string) []expectedPVC {
	roles := []utils.PVCRole{utils.PVCRolePgData}

	if cluster.ShouldCreateWalArchiveVolume() {
		roles = append(roles, utils.PVCRolePgWal)
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

func containsRole(roles []utils.PVCRole, role utils.PVCRole) bool {
	for _, pvcRole := range roles {
		if pvcRole == role {
			return true
		}
	}
	return false
}

// here we should register any new PVC for the instance
func buildExpectedPVCs(instanceName string, roles []utils.PVCRole) []expectedPVC {
	var expectedMounts []expectedPVC

	if containsRole(roles, utils.PVCRolePgData) {
		// At the moment detecting a pod is missing the data pvc has no real use.
		// In the future we will handle all the PVC creation with the package reconciler
		dataPVCName := GetName(instanceName, utils.PVCRolePgData)
		expectedMounts = append(expectedMounts,
			expectedPVC{
				name: dataPVCName,
				role: utils.PVCRolePgData,
				// This requires a init, ideally we should move to a design where each pvc can be init separately
				// and then  attached
				initialStatus: StatusInitializing,
			},
		)
	}

	if containsRole(roles, utils.PVCRolePgWal) {
		walPVCName := GetName(instanceName, utils.PVCRolePgWal)
		expectedMounts = append(expectedMounts,
			expectedPVC{
				name:          walPVCName,
				role:          utils.PVCRolePgWal,
				initialStatus: StatusReady,
			},
		)
	}

	return expectedMounts
}

func getStorageConfiguration(
	cluster *apiv1.Cluster,
	role utils.PVCRole,
) (apiv1.StorageConfiguration, error) {
	var storageConfiguration *apiv1.StorageConfiguration
	switch role {
	case utils.PVCRolePgData:
		storageConfiguration = &cluster.Spec.StorageConfiguration
	case utils.PVCRolePgWal:
		storageConfiguration = cluster.Spec.WalStorage
	default:
		return apiv1.StorageConfiguration{}, fmt.Errorf("unknown pvcRole: %s", string(role))
	}

	if storageConfiguration == nil {
		return apiv1.StorageConfiguration{},
			fmt.Errorf("storage configuration doesn't exist for the given PVC role: %s", role)
	}

	return *storageConfiguration, nil
}

// GetInstancePVCs gets all the PVC associated with a given instance
func GetInstancePVCs(
	ctx context.Context,
	cli client.Client,
	instanceName string,
	namespace string,
) ([]corev1.PersistentVolumeClaim, error) {
	getPVC := func(name string) (*corev1.PersistentVolumeClaim, error) {
		var pvc corev1.PersistentVolumeClaim
		err := cli.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &pvc)
		if errors.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &pvc, nil
	}

	var pvcs []corev1.PersistentVolumeClaim

	pgDataName := GetName(instanceName, utils.PVCRolePgData)
	pgData, err := getPVC(pgDataName)
	if err != nil {
		return nil, err
	}
	if pgData != nil {
		pvcs = append(pvcs, *pgData)
	}

	pgWalName := GetName(instanceName, utils.PVCRolePgWal)
	pgWal, err := getPVC(pgWalName)
	if err != nil {
		return nil, err
	}
	if pgWal != nil {
		pvcs = append(pvcs, *pgWal)
	}

	return pvcs, nil
}
