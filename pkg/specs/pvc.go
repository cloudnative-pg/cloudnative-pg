/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"
)

// DetectDanglingPVCs fill the list of the PVCs which are dangling, given that
// PVC are usually named after Pods
func DetectDanglingPVCs(
	podList []corev1.Pod,
	pvcList []corev1.PersistentVolumeClaim,
) []string {
	var result []string

	for _, pvc := range pvcList {
		if pvc.Status.Phase != corev1.ClaimPending &&
			pvc.Status.Phase != corev1.ClaimBound {
			continue
		}

		// There's no point in reattaching deleted PVCs
		if pvc.ObjectMeta.DeletionTimestamp != nil {
			continue
		}
		found := false

		for idx := range podList {
			if podList[idx].Name == pvc.Name {
				found = true
				break
			}
		}

		if !found {
			result = append(result, pvc.Name)
		}
	}

	return result
}
