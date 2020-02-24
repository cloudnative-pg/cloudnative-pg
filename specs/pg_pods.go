/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

// GetNodeSerial get the serial number of a Pod created by the operator
// for a Cluster
func GetNodeSerial(pod corev1.Pod) (int, error) {
	nodeSerial, ok := pod.Annotations[ClusterSerialAnnotationName]
	if !ok {
		return 0, fmt.Errorf("missing node serial annotation")
	}

	result, err := strconv.Atoi(nodeSerial)
	if err != nil {
		return 0, fmt.Errorf("wrong node serial annotation: %v", nodeSerial)
	}

	return result, nil
}

// IsPodPrimary check if a certain pod belongs to a primary
func IsPodPrimary(pod corev1.Pod) bool {
	role, hasRole := pod.ObjectMeta.Labels["role"]
	if !hasRole {
		return false
	}

	return role == "primary"
}

// IsPodStandby check if a certain pod belongs to a standby
func IsPodStandby(pod corev1.Pod) bool {
	return !IsPodPrimary(pod)
}
