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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/strings/slices"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Status describes the PVC phase
type Status = string

const (
	// StatusAnnotationName is an annotation that shows the current status of the PVC.
	// The status can be "initializing" or "ready"
	StatusAnnotationName = specs.MetadataNamespace + "/pvcStatus"

	// StatusInitializing is the annotation value for PVC initializing status
	StatusInitializing Status = "initializing"

	// StatusReady is the annotation value for PVC ready status
	StatusReady Status = "ready"

	// StatusDetached is the annotation value for PVC detached status
	StatusDetached Status = "detached"
)

// ErrorInvalidSize is raised when the size specified by the
// user is not valid and can't be specified in a PVC declaration
var ErrorInvalidSize = fmt.Errorf("invalid storage size")

// UsageStatus is the status of the PVC we generated
type UsageStatus struct {
	// List of available instances detected from pvcs
	InstanceNames []string

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

	// List of PVCs that are unusable (they are part of an incomplete group)
	Unusable []string
}

// GetName builds the name for a given PVC of the instance
func GetName(cluster *apiv1.Cluster, instanceName string, role utils.PVCRole) string {
	pvcName := instanceName
	if role == utils.PVCRolePgWal {
		pvcName += cluster.GetWalArchiveVolumeSuffix()
	}
	return pvcName
}

// FilterByInstance returns all the corev1.PersistentVolumeClaim that are used inside the podSpec
func FilterByInstance(
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

// CalculateUsageStatus fill the list with the PVCs which are dangling, given that
// PVC are usually named after Pods
// nolint: gocognit
func CalculateUsageStatus(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList []corev1.Pod,
	jobList []batchv1.Job,
	pvcList []corev1.PersistentVolumeClaim,
) (result UsageStatus) {
	contextLogger := log.FromContext(ctx)

	// First we iterate over all the PVCs building the instances map.
	// It contains the PVCSs grouped by instance serial
	instances := make(map[int][]corev1.PersistentVolumeClaim)
	for _, pvc := range pvcList {
		// Ignore PVCs that is in the wrong state
		if pvc.Status.Phase != corev1.ClaimPending &&
			pvc.Status.Phase != corev1.ClaimBound {
			continue
		}

		// There's no point in reattaching deleted PVCs
		if pvc.ObjectMeta.DeletionTimestamp != nil {
			continue
		}

		// Detect the instance serial number.
		// If it returns an error the PVC is ill-formed and we ignore it
		serial, err := specs.GetNodeSerial(pvc.ObjectMeta)
		if err != nil {
			continue
		}
		instances[serial] = append(instances[serial], pvc)

		// Given that we are iterating over the PVCs
		// we take the chance to build the list of resizing PVCs
		if isResizing(pvc) {
			result.Resizing = append(result.Resizing, pvc.Name)
		}
	}

	// For every instance we have we validate the list of PVCs
	// and detect if there is an attached Pod or Job
instancesLoop:
	for serial, pvcs := range instances {
		instanceName := fmt.Sprintf("%s-%v", cluster.Name, serial)
		expectedPVCs := getExpectedInstancePVCNames(cluster, instanceName)
		pvcNames := getNamesFromPVCList(pvcs)

		// If we have less PVCs that the expected number, all the instance PVCs are unusable
		if len(expectedPVCs) > len(pvcNames) {
			result.Unusable = append(result.Unusable, pvcNames...)
			continue instancesLoop
		}

		// If some PVC is missing, all the instance PVCs are unusable
		for _, expectedPVC := range expectedPVCs {
			if !slices.Contains(pvcNames, expectedPVC) {
				result.Unusable = append(result.Unusable, pvcNames...)
				continue instancesLoop
			}
		}

		// If we have PVCs that we don't expect, these PVCs need to
		// be classified as unusable
		for _, pvcName := range pvcNames {
			if !slices.Contains(expectedPVCs, pvcName) {
				result.Unusable = append(result.Unusable, pvcName)
				contextLogger.Warning("found more PVC than those expected",
					"instance", instanceName,
					"expectedPVCs", expectedPVCs,
					"foundPVCs", pvcNames,
				)
			}
		}

		// From this point we only consider expected PVCs.
		// Any extra PVC is already in the Unusable list
		pvcNames = expectedPVCs

		isAnyPvcUnusable := false
		for _, pvc := range pvcs {
			// We ignore any PVC that is not expected
			if !slices.Contains(expectedPVCs, pvc.Name) {
				continue
			}

			if pvc.Annotations[StatusAnnotationName] != StatusReady {
				isAnyPvcUnusable = true
			}
		}

		if !isAnyPvcUnusable {
			result.InstanceNames = append(result.InstanceNames, instanceName)
		}
		// Search for a Pod corresponding to this instance.
		// If found, all the PVCs are Healthy
		for idx := range podList {
			if IsUsedByPodSpec(podList[idx].Spec, pvcNames...) {
				// We found a Pod using this PVCs so this
				// PVCs are not dangling
				result.Healthy = append(result.Healthy, pvcNames...)
				continue instancesLoop
			}
		}

		// Search for a Job corresponding to this instance.
		// If found, all the PVCs are Initializing
		for idx := range jobList {
			if IsUsedByPodSpec(jobList[idx].Spec.Template.Spec, pvcNames...) {
				// We have found a Job corresponding to this PVCs, so we
				// are initializing them or the initialization has just completed
				result.Initializing = append(result.Initializing, pvcNames...)
				continue instancesLoop
			}
		}

		if isAnyPvcUnusable {
			// This PVC has not a Job nor a Pod using it, but it is not marked as StatusReady
			// we need to ignore this instance and treat all the instance PVCs as unusable
			result.Unusable = append(result.Unusable, pvcNames...)
			contextLogger.Warning("found PVC that is not annotated as ready",
				"pvcNames", pvcNames,
				"instance", instanceName,
				"expectedPVCs", expectedPVCs,
				"foundPVCs", pvcNames,
			)
			continue instancesLoop
		}

		// These PVCs have not a Job nor a Pod using them, they are dangling
		result.Dangling = append(result.Dangling, pvcNames...)
	}

	return result
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

// IsUsedByInstance returns a boolean indicating if that given PVC belongs to an instance
func IsUsedByInstance(cluster *apiv1.Cluster, instanceName, resourceName string) bool {
	expectedInstancePVCs := getExpectedInstancePVCNames(cluster, instanceName)
	return slices.Contains(expectedInstancePVCs, resourceName)
}

// getExpectedInstancePVCNames gets all the PVC names for a given instance
func getExpectedInstancePVCNames(cluster *apiv1.Cluster, instanceName string) []string {
	names := []string{instanceName}

	if cluster.ShouldCreateWalArchiveVolume() {
		names = append(names, instanceName+cluster.GetWalArchiveVolumeSuffix())
	}

	return names
}

// getNamesFromPVCList returns a list of PVC names extracted from a list of PVCs
func getNamesFromPVCList(pvcs []corev1.PersistentVolumeClaim) []string {
	pvcNames := make([]string, len(pvcs))
	for i, pvc := range pvcs {
		pvcNames[i] = pvc.Name
	}
	return pvcNames
}
