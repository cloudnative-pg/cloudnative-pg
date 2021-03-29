/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// PVCUsageStatus is the status of the PVC we generated
type PVCUsageStatus struct {
	// List of PVC that are being initialized (they have a corresponding Job but not a corresponding Pod)
	Initializing []string

	// List of PVC that are dangling (they don't have a corresponding Job nor a corresponding Pod)
	Dangling []string
}

// DetectPVCs fill the list of the PVCs which are dangling, given that
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
			continue
		}

		jobFound := false
		for idx := range jobList {
			if strings.HasPrefix(jobList[idx].Name, pvc.Name+"-") {
				jobFound = true
				break
			}
		}

		if jobFound {
			// We have found a Job corresponding to this PVC, so we
			// are initializing it or the initialization is just completed
			result.Initializing = append(result.Initializing, pvc.Name)
		} else {
			// This PVC has not a Job nor a Pod using it, it's dangling
			result.Dangling = append(result.Dangling, pvc.Name)
		}
	}

	return result
}
