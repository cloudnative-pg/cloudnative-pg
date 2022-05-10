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
	"fmt"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
	Resizing []apiv1.ResizingPVCInformation
	// List of PVCs that are dangling (they don't have a corresponding Job nor a corresponding Pod)
	Dangling []string

	// List of PVCs that are used (they have a corresponding Pod)
	Healthy []string
}

// CreatePVC create spec of a PVC, given its name and the storage configuration
func CreatePVC(
	storageConfiguration apiv1.StorageConfiguration,
	name string,
	namespace string,
	nodeSerial int,
) (*corev1.PersistentVolumeClaim, error) {
	pvcName := fmt.Sprintf("%s-%v", name, nodeSerial)

	result := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
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

// DetectPVCs fill the list with the PVCs which are dangling, given that
// PVC are usually named after Pods
func DetectPVCs(
	podList []corev1.Pod,
	jobList []batchv1.Job,
	pvcList []corev1.PersistentVolumeClaim,
) (result PVCUsageStatus) {
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
			result.Resizing = append(result.Resizing, apiv1.ResizingPVCInformation{
				Name:    pvc.Name,
				Current: pvc.Status.Capacity.Storage().String(),
				Desired: pvc.Spec.Resources.Requests.Storage().String(),
			})
		}

		// Find a Pod corresponding to this PVC
		podFound := false
		for idx := range podList {
			if podList[idx].Name == pvc.Name {
				podFound = true
				break
			}
		}

		if podFound {
			// We found a Pod using this PVC so this
			// PVC is not dangling
			result.Healthy = append(result.Healthy, pvc.Name)
			continue
		}

		jobFound := false
		for idx := range jobList {
			if IsJobOperatingOnPVC(jobList[idx], pvc) {
				jobFound = true
				break
			}
		}

		if jobFound {
			// We have found a Job corresponding to this PVC, so we
			// are initializing it or the initialization is just completed
			result.Initializing = append(result.Initializing, pvc.Name)
			continue
		}

		if pvc.Annotations[PVCStatusAnnotationName] != PVCStatusReady {
			// This PVC has not a Job nor a Pod using it, but it is not marked as PVCStatusReady
			// we need to ignore it here
			continue
		}

		// This PVC has not a Job nor a Pod using it, it's dangling
		result.Dangling = append(result.Dangling, pvc.Name)
	}

	return result
}

// IsJobOperatingOnPVC checks if a Job is initializing the provided PVC
func IsJobOperatingOnPVC(job batchv1.Job, pvc corev1.PersistentVolumeClaim) bool {
	return strings.HasPrefix(job.Name, pvc.Name+"-")
}

// isResizing returns true if PersistentVolumeClaimResizing condition is present
func isResizing(pvc corev1.PersistentVolumeClaim) bool {
	for _, condition := range pvc.Status.Conditions {
		if condition.Type == corev1.PersistentVolumeClaimResizing ||
			condition.Type == corev1.PersistentVolumeClaimFileSystemResizePending {
			return true
		}
	}

	return false
}
