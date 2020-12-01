/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package utils

import (
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	log = ctrl.Log.WithName("utils")
)

// IsPodReady check if a Pod is ready or not
func IsPodReady(pod corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.ContainersReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

// IsPodActive check if a pod is active
func IsPodActive(p corev1.Pod) bool {
	return corev1.PodSucceeded != p.Status.Phase &&
		corev1.PodFailed != p.Status.Phase &&
		p.DeletionTimestamp == nil
}

// FilterActivePods returns pods that have not terminated.
func FilterActivePods(pods []corev1.Pod) []corev1.Pod {
	var result []corev1.Pod
	for _, p := range pods {
		if IsPodActive(p) {
			result = append(result, p)
		} else {
			log.V(4).Info("Ignoring inactive pod %v/%v in state %v, deletion time %v",
				"namespace", p.Namespace,
				"name", p.Name,
				"phase", p.Status.Phase,
				"deletionTimestamp", p.DeletionTimestamp)
		}
	}
	return result
}

// CountReadyPods counts the number of Pods which are ready
func CountReadyPods(podList []corev1.Pod) int {
	readyPods := 0
	for _, pod := range podList {
		if IsPodReady(pod) {
			readyPods++
		}
	}
	return readyPods
}

// ListStatusPods return a list of active Pods
func ListStatusPods(podList []corev1.Pod) map[string][]string {
	var podsNames = make(map[string][]string)

	for _, pod := range podList {
		switch {
		case IsPodReady(pod):
			podsNames["healthy"] = append(podsNames["healthy"], pod.Name)
		case IsPodActive(pod):
			podsNames["replicating"] = append(podsNames["replicating"], pod.Name)
		default:
			podsNames["failed"] = append(podsNames["failed"], pod.Name)
		}
	}

	return podsNames
}
