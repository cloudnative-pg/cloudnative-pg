/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package utils

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var log logr.Logger = zap.Logger(true).WithName("omnidb")

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

// IsPodUpgrading check if a new image is being applied
// to a Pod
func IsPodUpgrading(p corev1.Pod) bool {
	desiredImages := make(map[string]string)
	currentImages := make(map[string]string)

	for _, container := range p.Spec.Containers {
		desiredImages[container.Name] = container.Image
	}

	for _, container := range p.Status.ContainerStatuses {
		currentImages[container.Name] = container.Image
		if specImage, ok := desiredImages[container.Name]; !ok || !IsImageNameEqual(specImage, container.Image) {
			// We have a container running with a different image name
			return true
		}
	}

	for name := range desiredImages {
		if _, ok := currentImages[name]; !ok {
			// We have a container which is yet to be started
			return true
		}
	}

	return false
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

// CountUpgradingPods counts the number of Pods which are being upgraded to
// a different image
func CountUpgradingPods(podList []corev1.Pod) int {
	upgradingPods := 0
	for _, pod := range podList {
		if IsPodUpgrading(pod) {
			upgradingPods++
		}
	}
	return upgradingPods
}
