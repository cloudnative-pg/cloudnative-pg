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
	"sort"

	"github.com/cloudnative-pg/machinery/pkg/log"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/strings/slices"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PVCStatus describes the PVC phase
type PVCStatus = string

const (
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

type status string

const (
	// List of available instances detected from PVCs
	instanceNames status = "instanceNames"

	// List of PVCs that are being initialized (they have a corresponding Job but not a corresponding Pod)
	initializing status = "initializing"

	// List of PVCs with resizing condition. Requires a pod restart.
	//
	// INFO: https://kubernetes.io/blog/2018/07/12/resizing-persistent-volumes-using-kubernetes/
	resizing status = "resizing"

	// List of PVCs that are dangling (they don't have a corresponding Job nor a corresponding Pod)
	dangling status = "dangling"

	// List of PVCs that are used (they have a corresponding Pod)
	healthy status = "healthy"

	// List of PVCs that are unusable (they are part of an incomplete group)
	unusable status = "unusable"

	// List of PVCs that we should ignore
	ignored status = "ignored"
)

type statuses map[status][]string

func (s statuses) add(label status, name string) {
	s[label] = append(s[label], name)
}

func (s statuses) getSorted(label status) []string {
	sort.Strings(s[label])

	return s[label]
}

// EnrichStatus obtains and classifies the current status of each managed PVC
func EnrichStatus(
	ctx context.Context,
	cluster *apiv1.Cluster,
	runningInstances []corev1.Pod,
	jobs []batchv1.Job,
	managedPVCs []corev1.PersistentVolumeClaim,
) {
	// First we iterate over all the PVCs building the instances map.
	// It contains the PVCSs grouped by instance serial
	instancesPVCs := make(map[string][]corev1.PersistentVolumeClaim)
	for _, pvc := range managedPVCs {
		// Ignore PVCs that is in the wrong state
		if pvc.Status.Phase != corev1.ClaimPending &&
			pvc.Status.Phase != corev1.ClaimBound {
			continue
		}

		// There's no point in reattaching ignored PVCs
		if pvc.ObjectMeta.DeletionTimestamp != nil {
			continue
		}

		// Detect the instance serial number.
		// If it returns an error the PVC is ill-formed and we ignore it
		serial, err := specs.GetNodeSerial(pvc.ObjectMeta)
		if err != nil {
			continue
		}
		instanceName := specs.GetInstanceName(cluster.Name, serial)
		instancesPVCs[instanceName] = append(instancesPVCs[instanceName], pvc)
	}

	// For every instance we have we validate the list of PVCs
	// and detect if there is an attached Pod or Job
	result := make(statuses)
	for instanceName, pvcs := range instancesPVCs {
		for _, pvc := range pvcs {
			pvcStatus := classifyPVC(ctx, pvc, runningInstances, jobs, pvcs, cluster, instanceName)
			result.add(pvcStatus, pvc.Name)
		}
		result.add(instanceNames, instanceName)
	}

	// an instance has no identity of its own, is a reflection of the available PVCs
	sortedInstances := result.getSorted(instanceNames)
	cluster.Status.Instances = len(sortedInstances)
	cluster.Status.InstanceNames = sortedInstances

	filteredPods := utils.FilterActivePods(runningInstances)
	cluster.Status.ReadyInstances = utils.CountReadyPods(filteredPods)
	cluster.Status.InstancesStatus = apiv1.ListStatusPods(runningInstances)

	cluster.Status.PVCCount = int32(len(managedPVCs)) //nolint:gosec
	cluster.Status.InitializingPVC = result.getSorted(initializing)
	cluster.Status.ResizingPVC = result.getSorted(resizing)
	cluster.Status.DanglingPVC = result.getSorted(dangling)
	cluster.Status.HealthyPVC = result.getSorted(healthy)
	cluster.Status.UnusablePVC = result.getSorted(unusable)
}

func classifyPVC(
	ctx context.Context,
	pvc corev1.PersistentVolumeClaim,
	podList []corev1.Pod,
	jobList []batchv1.Job,
	pvcList []corev1.PersistentVolumeClaim,
	cluster *apiv1.Cluster,
	instanceName string,
) status {
	// PVC to ignore
	if pvc.ObjectMeta.DeletionTimestamp != nil || hasUnknownStatus(ctx, pvc) {
		return ignored
	}

	expectedPVCs := getExpectedInstancePVCNamesFromCluster(cluster, instanceName)
	pvcNames := getNamesFromPVCList(pvcList)

	// PVC is part of an incomplete group
	if len(expectedPVCs) > len(pvcNames) || !slices.Contains(expectedPVCs, pvc.Name) {
		return unusable
	}

	// PVC is resizing
	if isResizing(pvc) {
		return resizing
	}

	// PVC has a corresponding Pod
	if hasPod(pvc, podList) {
		return healthy
	}

	// PVC has a corresponding Job but not a corresponding Pod
	if hasJob(pvc, jobList) {
		return initializing
	}

	// PVC does not have a corresponding Job nor a corresponding Pod
	return dangling
}

// hasJob checks if the PVC has a corresponding Job
func hasJob(pvc corev1.PersistentVolumeClaim, jobList []batchv1.Job) bool {
	// check if the PVC has a corresponding Job
	for _, job := range jobList {
		if jobUsesPVC(job, pvc) {
			// if the job is completed the PVC should be reported as not used
			return !utils.JobHasOneCompletion(job)
		}
	}
	return false
}

// hasPod checks if the PVC has a corresponding Pod
func hasPod(pvc corev1.PersistentVolumeClaim, podList []corev1.Pod) bool {
	for _, pod := range podList {
		if podUsesPVC(pod, pvc) {
			return true
		}
	}
	return false
}

// jobUsesPVC checks if the given Job uses the given PVC
func jobUsesPVC(job batchv1.Job, pvc corev1.PersistentVolumeClaim) bool {
	for _, vol := range job.Spec.Template.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == pvc.Name {
			return true
		}
	}
	return false
}

// podUsesPVC checks if the given Pod uses the given PVC
func podUsesPVC(pod corev1.Pod, pvc corev1.PersistentVolumeClaim) bool {
	for _, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == pvc.Name {
			return true
		}
	}
	return false
}

func hasUnknownStatus(ctx context.Context, pvc corev1.PersistentVolumeClaim) bool {
	// Expected statuses are: Ready, Initializing or empty (that means initializing)
	if pvc.Annotations[utils.PVCStatusAnnotationName] == StatusReady ||
		pvc.Annotations[utils.PVCStatusAnnotationName] == StatusInitializing ||
		pvc.Annotations[utils.PVCStatusAnnotationName] == "" {
		return false
	}

	contextLogger := log.FromContext(ctx)
	contextLogger.Warning("Unknown PVC status",
		"namespace", pvc.Namespace,
		"name", pvc.Name,
		"status", pvc.Annotations[utils.PVCStatusAnnotationName])

	return true
}
