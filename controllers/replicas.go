/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/2ndquadrant/cloud-native-postgresql/specs"
	"github.com/2ndquadrant/cloud-native-postgresql/utils"
)

// getSacrificialPod get the Pod who is supposed to be deleted
// when the cluster is scaled down
func getSacrificialPod(podList []corev1.Pod) *corev1.Pod {
	resultIdx := -1
	var lastFoundSerial int

	for idx, pod := range podList {
		// Avoid parting non ready nodes, non active nodes, or master nodes
		if !utils.IsPodReady(pod) || !utils.IsPodActive(pod) || specs.IsPodMaster(pod) {
			continue
		}

		podSerial, err := specs.GetNodeSerial(pod)

		// This isn't one of our Pods, since I can't get the node serial
		if err != nil {
			continue
		}

		if lastFoundSerial == 0 || lastFoundSerial < podSerial {
			resultIdx = idx
			lastFoundSerial = podSerial
		}
	}

	if resultIdx == -1 {
		return nil
	}
	return &podList[resultIdx]
}

// getMasterPod get the Pod who is supposed to be master
// master of this cluster
func getMasterPod(podList []corev1.Pod) *corev1.Pod {
	for idx, pod := range podList {
		if !specs.IsPodMaster(pod) {
			continue
		}

		if !utils.IsPodReady(pod) || !utils.IsPodActive(pod) {
			continue
		}

		_, err := specs.GetNodeSerial(pod)

		// This isn't one of our Pods, since I can't get the node serial
		if err != nil {
			continue
		}

		return &podList[idx]
	}

	return nil
}
