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
)

// PVCStatus describes the PVC phase
type PVCStatus = string

const (
	// StatusAnnotationName is an annotation that shows the current status of the PVC.
	// The status can be "initializing" or "ready"
	StatusAnnotationName = specs.MetadataNamespace + "/pvcStatus"

	// StatusInitializing is the annotation value for PVC initializing status
	StatusInitializing PVCStatus = "initializing"

	// StatusReady is the annotation value for PVC ready status
	StatusReady PVCStatus = "ready"

	// StatusDetached is the annotation value for PVC detached status
	StatusDetached PVCStatus = "detached"
)

// ErrorInvalidSize is raised when the size specified by the
// user is not valid and can't be specified in a PVC declaration
var ErrorInvalidSize = fmt.Errorf("invalid storage size")

// status is the status of the PVC we generated
type status struct {
	// List of available instances detected from pvcs
	instanceNames []string

	// List of PVCs that are being initialized (they have a corresponding Job but not a corresponding Pod)
	initializing []string

	// List of PVCs with resizing condition. Requires a pod restart.
	//
	// INFO: https://kubernetes.io/blog/2018/07/12/resizing-persistent-volumes-using-kubernetes/
	resizing []string

	// List of PVCs that are dangling (they don't have a corresponding Job nor a corresponding Pod)
	dangling []string

	// List of PVCs that are used (they have a corresponding Pod)
	healthy []string

	// List of PVCs that are unusable (they are part of an incomplete group)
	unusable []string
}

// EnrichStatus obtains and classifies the current status of each managed PVC
// nolint: gocognit
func EnrichStatus(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podList []corev1.Pod,
	jobList []batchv1.Job,
	pvcs []corev1.PersistentVolumeClaim,
) {
	contextLogger := log.FromContext(ctx)

	var result status

	// First we iterate over all the PVCs building the instances map.
	// It contains the PVCSs grouped by instance serial
	instances := make(map[int][]corev1.PersistentVolumeClaim)
	for _, pvc := range pvcs {
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
			result.resizing = append(result.resizing, pvc.Name)
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
			result.unusable = append(result.unusable, pvcNames...)
			continue instancesLoop
		}

		// If some PVC is missing, all the instance PVCs are unusable
		for _, expectedPVC := range expectedPVCs {
			if !slices.Contains(pvcNames, expectedPVC) {
				result.unusable = append(result.unusable, pvcNames...)
				continue instancesLoop
			}
		}

		// If we have PVCs that we don't expect, these PVCs need to
		// be classified as unusable
		for _, pvcName := range pvcNames {
			if !slices.Contains(expectedPVCs, pvcName) {
				result.unusable = append(result.unusable, pvcName)
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
			result.instanceNames = append(result.instanceNames, instanceName)
		}
		// Search for a Pod corresponding to this instance.
		// If found, all the PVCs are Healthy
		for idx := range podList {
			if IsUsedByPodSpec(podList[idx].Spec, pvcNames...) {
				// We found a Pod using this PVCs so this
				// PVCs are not dangling
				result.healthy = append(result.healthy, pvcNames...)
				continue instancesLoop
			}
		}

		// Search for a Job corresponding to this instance.
		// If found, all the PVCs are initializing
		for idx := range jobList {
			if IsUsedByPodSpec(jobList[idx].Spec.Template.Spec, pvcNames...) {
				// We have found a Job corresponding to this PVCs, so we
				// are initializing them or the initialization has just completed
				result.initializing = append(result.initializing, pvcNames...)
				continue instancesLoop
			}
		}

		if isAnyPvcUnusable {
			// This PVC has not a Job nor a Pod using it, but it is not marked as StatusReady
			// we need to ignore this instance and treat all the instance PVCs as unusable
			result.unusable = append(result.unusable, pvcNames...)
			contextLogger.Warning("found PVC that is not annotated as ready",
				"pvcNames", pvcNames,
				"instance", instanceName,
				"expectedPVCs", expectedPVCs,
				"foundPVCs", pvcNames,
			)
			continue instancesLoop
		}

		// These PVCs have not a Job nor a Pod using them, they are dangling
		result.dangling = append(result.dangling, pvcNames...)
	}

	cluster.Status.PVCCount = int32(len(pvcs))
	cluster.Status.InstanceNames = result.instanceNames
	cluster.Status.DanglingPVC = result.dangling
	cluster.Status.HealthyPVC = result.healthy
	cluster.Status.InitializingPVC = result.initializing
	cluster.Status.ResizingPVC = result.resizing
	cluster.Status.UnusablePVC = result.unusable
}
