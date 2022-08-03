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
	"strings"

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
	Initializing []apiv1.InstancePVC

	// List of PVCs with Resizing condition. Requires a pod restart.
	//
	// INFO: https://kubernetes.io/blog/2018/07/12/resizing-persistent-volumes-using-kubernetes/
	Resizing []apiv1.InstancePVC

	// List of PVCs that are dangling (they don't have a corresponding Job nor a corresponding Pod)
	Dangling []apiv1.InstancePVC

	// List of PVCs that are used (they have a corresponding Pod)
	Healthy []apiv1.InstancePVC
}

// CreatePVC create spec of a PVC, given its name and the storage configuration
func CreatePVC(
	storageConfiguration apiv1.StorageConfiguration,
	cluster apiv1.Cluster,
	suffix string,
	nodeSerial int,
) (*corev1.PersistentVolumeClaim, error) {
	pvcName := fmt.Sprintf("%s-%v%s", cluster.Name, nodeSerial, suffix)

	result := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				utils.InstanceLabelName: generateInstanceName(cluster, nodeSerial),
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

// DetectPVCs fill the list with the PVCs which are dangling, given that
// PVC are usually named after Pods
// nolint: gocognit
func DetectPVCs(
	ctx context.Context,
	cluster apiv1.Cluster,
	podList []corev1.Pod,
	jobList []batchv1.Job,
	pvcList []corev1.PersistentVolumeClaim,
) (result PVCUsageStatus) {
	contextLogger := log.FromContext(ctx)
	var foundInstances []string
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

		// TODO optimize
		// we save all the instance we found so far
		instanceName := pvc.Labels[utils.InstanceLabelName]
		if !slices.Contains(foundInstances, instanceName) {
			foundInstances = append(foundInstances, instanceName)
		}
		// END TODO

		instancePVC := apiv1.InstancePVC{
			InstanceName: instanceName,
			PvcName:      pvc.Name,
		}

		if isResizing(pvc) {
			result.Resizing = append(result.Resizing, instancePVC)
		}

		// Find a Pod corresponding to this PVC
		for idx := range podList {
			for _, volume := range podList[idx].Spec.Volumes {
				if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == instancePVC.PvcName {
					// We found a Pod using this PVC so this
					// PVC is not dangling
					result.Healthy = append(result.Healthy, instancePVC)
					continue pvcLoop
				}
			}
		}

		for idx := range jobList {
			if IsJobOperatingOnPVC(jobList[idx], pvc) {
				// We have found a Job corresponding to this PVC, so we
				// are initializing it or the initialization is just completed
				result.Initializing = append(result.Initializing, instancePVC)
				continue pvcLoop
			}
		}

		if pvc.Annotations[PVCStatusAnnotationName] != PVCStatusReady {
			// This PVC has not a Job nor a Pod using it, but it is not marked as PVCStatusReady
			// we need to ignore it here
			continue
		}

		// This PVC has not a Job nor a Pod using it, it's dangling
		result.Dangling = append(result.Dangling, instancePVC)
	}

	if !cluster.ShouldCreateWalArchiveVolume() {
		return result
	}

	// NORMALIZE. TODO: refactor
	for _, instance := range foundInstances {
		expected := len(cluster.GetInstancePVCNames(instance))
		var found []int
		for idx, pvc := range result.Healthy {
			if pvc.InstanceName == instance {
				found = append(found, idx)
			}
		}
		contextLogger.Info("detectPVC info", "instance", instance, "found", found, "expected", expected)
		if expected == len(found) {
			continue
		}

		if expected > len(found) {
			// we lost some pvc null them all
			for _, index := range found {
				result.Dangling = append(result.Dangling, result.Healthy[index])
				result.Healthy = removeElementByIndex(result.Healthy, index)
			}
		}

		// if len(found) > expected {
		// something terribly wrong
		//}
	}

	return result
}

func removeElementByIndex[T any](slice []T, index int) []T {
	return append(slice[:index], slice[index+1:]...)
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
