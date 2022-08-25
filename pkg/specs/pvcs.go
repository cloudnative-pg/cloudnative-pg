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

package specs

import (
	"context"
	"fmt"
	"strconv"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/strings/slices"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// PVCStatusAnnotationName is an annotation that shows the current status of the PVC.
	// The status can be "initializing" or "ready"
	PVCStatusAnnotationName = MetadataNamespace + "/pvcStatus"

	// PVCStatusInitializing is the annotation value for PVC initializing status
	PVCStatusInitializing = "initializing"

	// PVCStatusReady is the annotation value for PVC ready status
	PVCStatusReady = "ready"
)

// ErrorInvalidSize is raised when the size specified by the
// user is not valid and can't be specified in a PVC declaration
var ErrorInvalidSize = fmt.Errorf("invalid storage size")

// PVCUsageStatus is the status of the PVC we generated
type PVCUsageStatus struct {
	// List of PVCs that are being initialized (they have a corresponding Job but not a corresponding Pod)
	Initializing []string

	// List of PVCs with Resizing condition. Requires a pod restart.
	//
	// INFO: https://kubernetes.io/blog/2018/07/12/resizing-persistent-volumes-using-kubernetes/
	Resizing []string

	// List of PVCs that are dangling (they don't have a corresponding Job nor a corresponding Pod)
	Dangling []string

	// List of PVCs that are used (they have a corresponding Pod)
	Healthy []string
}

// CreatePVC create spec of a PVC, given its name and the storage configuration
func CreatePVC(
	storageConfiguration apiv1.StorageConfiguration,
	cluster apiv1.Cluster,
	nodeSerial int,
	role utils.PVCRole,
) (*corev1.PersistentVolumeClaim, error) {
	instanceName := fmt.Sprintf("%s-%v", cluster.Name, nodeSerial)
	pvcName := instanceName
	if role == utils.PVCRolePgWal {
		pvcName += cluster.GetWalArchiveVolumeSuffix()
	}

	result := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				utils.InstanceNameLabelName: instanceName,
				utils.PvcRoleLabelName:      string(role),
			},
			Annotations: map[string]string{
				ClusterSerialAnnotationName: strconv.Itoa(nodeSerial),
				PVCStatusAnnotationName:     PVCStatusInitializing,
			},
		},
	}

	// If the customer supplied a spec, let's use it
	if storageConfiguration.PersistentVolumeClaimTemplate != nil {
		storageConfiguration.PersistentVolumeClaimTemplate.DeepCopyInto(&result.Spec)
	}

	// If the customer specified a storage class, let's use it
	if storageConfiguration.StorageClass != nil {
		result.Spec.StorageClassName = storageConfiguration.StorageClass
	}

	// Insert the storage requirement
	parsedSize, err := resource.ParseQuantity(storageConfiguration.Size)
	if err != nil {
		return nil, ErrorInvalidSize
	}

	result.Spec.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			"storage": parsedSize,
		},
	}

	if len(result.Spec.AccessModes) == 0 {
		result.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		}
	}

	return result, nil
}

// FilterInstancePVCs returns all the corev1.PersistentVolumeClaim that are used inside the podSpec
func FilterInstancePVCs(
	pvcs []corev1.PersistentVolumeClaim,
	instanceSpec corev1.PodSpec,
) []corev1.PersistentVolumeClaim {
	var instancePVCs []corev1.PersistentVolumeClaim
	for _, volume := range instanceSpec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}

		for _, pvc := range pvcs {
			if volume.PersistentVolumeClaim.ClaimName == pvc.Name {
				instancePVCs = append(instancePVCs, pvc)
			}
		}
	}

	return instancePVCs
}

// DetectPVCs fill the list with the PVCs which are dangling, given that
// PVC are usually named after Pods
// nolint: gocognit
func DetectPVCs(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList []corev1.Pod,
	jobList []batchv1.Job,
	pvcList []corev1.PersistentVolumeClaim,
) (result PVCUsageStatus) {
	contextLogger := log.FromContext(ctx)

pvcLoop:
	for _, pvc := range pvcList {
		if pvc.Status.Phase != corev1.ClaimPending &&
			pvc.Status.Phase != corev1.ClaimBound {
			continue
		}

		// There's no point in reattaching deleted PVCs
		if pvc.ObjectMeta.DeletionTimestamp != nil {
			continue
		}

		if isResizing(pvc) {
			result.Resizing = append(result.Resizing, pvc.Name)
		}

		// Find a Pod corresponding to this PVC
		for idx := range podList {
			if IsPodSpecUsingPVC(podList[idx].Spec, pvc.Name) {
				// We found a Pod using this PVC so this
				// PVC is not dangling
				result.Healthy = append(result.Healthy, pvc.Name)
				continue pvcLoop
			}
		}

		for idx := range jobList {
			if IsPodSpecUsingPVC(jobList[idx].Spec.Template.Spec, pvc.Name) {
				// We have found a Job corresponding to this PVC, so we
				// are initializing it or the initialization is just completed
				result.Initializing = append(result.Initializing, pvc.Name)
				continue pvcLoop
			}
		}

		if pvc.Annotations[PVCStatusAnnotationName] != PVCStatusReady {
			// This PVC has not a Job nor a Pod using it, but it is not marked as PVCStatusReady
			// we need to ignore it here
			continue
		}

		// This PVC has not a Job nor a Pod using it, it's dangling
		result.Dangling = append(result.Dangling, pvc.Name)
	}

	if !cluster.ShouldCreateWalArchiveVolume() {
		return result
	}

	for _, instance := range getInstancesPods(podList) {
		expectedPVCs := getExpectedInstancePVCNames(cluster, instance.Name)
		var indexOfFoundPVCs []int

		for idx, pvcName := range result.Healthy {
			if slices.Contains(expectedPVCs, pvcName) {
				indexOfFoundPVCs = append(indexOfFoundPVCs, idx)
			}
		}

		switch {
		case len(expectedPVCs) > len(indexOfFoundPVCs):
			// we lost some pvc null them all
			for _, index := range indexOfFoundPVCs {
				result.Dangling = append(result.Dangling, result.Healthy[index])
				result.Healthy = removeElementByIndex(result.Healthy, index)
			}
		case len(indexOfFoundPVCs) > len(expectedPVCs):
			contextLogger.Warning("found more PVC than those expected",
				"instance", instance,
				"expectedPVCs", expectedPVCs,
				"foundPVCs", indexOfFoundPVCs,
			)
		}
	}

	return result
}

func removeElementByIndex[T any](slice []T, index int) []T {
	return append(slice[:index], slice[index+1:]...)
}

// IsPodSpecUsingPVC checks if the given pod spec is using the pvc
func IsPodSpecUsingPVC(podSpec corev1.PodSpec, pvcName string) bool {
	for _, volume := range podSpec.Volumes {
		if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
			return true
		}
	}
	return false
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

// DoesPVCBelongToInstance returns a boolean indicating if that given PVC belongs to an instance
func DoesPVCBelongToInstance(cluster *apiv1.Cluster, instanceName, resourceName string) bool {
	expectedInstancePVCs := getExpectedInstancePVCNames(cluster, instanceName)
	return slices.Contains(expectedInstancePVCs, resourceName)
}

// getInstancesPods filters a list of pods and returns only the CNPG instances pods
func getInstancesPods(pods []corev1.Pod) []corev1.Pod {
	var instancesName []corev1.Pod
	for _, pod := range pods {
		_, ok := pod.Labels[ClusterRoleLabelName]
		if ok {
			instancesName = append(instancesName, pod)
		}
	}

	return instancesName
}

// getExpectedInstancePVCNames gets all the PVC names for a given instance
func getExpectedInstancePVCNames(cluster *apiv1.Cluster, instanceName string) []string {
	names := []string{instanceName}

	if cluster.ShouldCreateWalArchiveVolume() {
		names = append(names, instanceName+cluster.GetWalArchiveVolumeSuffix())
	}

	return names
}
