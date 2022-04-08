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

package utils

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

const (
	// PodReasonEvicted is set inside the status as the Pod failure reason
	// when the Kubelet evicts a Pod
	PodReasonEvicted = "Evicted"
)

var utilsLog = log.WithName("utils")

// PodStatus represent the possible status of pods
type PodStatus string

const (
	// PodHealthy means that a Pod is active and ready
	PodHealthy = "healthy"

	// PodReplicating means that a Pod is still not ready but still active
	PodReplicating = "replicating"

	// PodFailed means that a Pod will not be scheduled again (deleted or evicted)
	PodFailed = "failed"
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

// IsPodActive checks if a pod is active, copied from:
//nolint:lll // https://github.com/kubernetes/kubernetes/blob/1bd00776b5d78828a065b5c21e7003accc308a06/test/e2e/framework/pod/resource.go#L664
func IsPodActive(p corev1.Pod) bool {
	return corev1.PodSucceeded != p.Status.Phase &&
		corev1.PodPending != p.Status.Phase &&
		corev1.PodFailed != p.Status.Phase &&
		p.DeletionTimestamp == nil
}

// IsPodEvicted checks if a pod has been evicted by the
// Kubelet
func IsPodEvicted(p corev1.Pod) bool {
	return corev1.PodFailed == p.Status.Phase &&
		PodReasonEvicted == p.Status.Reason
}

// IsPodAlive check if a pod is active and not crash-looping
func IsPodAlive(p corev1.Pod) bool {
	if corev1.PodRunning == p.Status.Phase {
		for _, container := range append(p.Status.InitContainerStatuses, p.Status.ContainerStatuses...) {
			if container.State.Waiting != nil && container.State.Waiting.Reason == "CrashLoopBackOff" {
				return false
			}
		}
	}
	return IsPodActive(p)
}

// FilterActivePods returns pods that have not terminated.
func FilterActivePods(pods []corev1.Pod) []corev1.Pod {
	var result []corev1.Pod
	for _, p := range pods {
		if IsPodActive(p) {
			result = append(result, p)
		} else {
			utilsLog.Trace("Ignoring inactive pod",
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
func ListStatusPods(podList []corev1.Pod) map[PodStatus][]string {
	podsNames := make(map[PodStatus][]string)

	for _, pod := range podList {
		switch {
		case IsPodReady(pod):
			podsNames[PodHealthy] = append(podsNames[PodHealthy], pod.Name)
		case IsPodActive(pod):
			podsNames[PodReplicating] = append(podsNames[PodReplicating], pod.Name)
		default:
			podsNames[PodFailed] = append(podsNames[PodFailed], pod.Name)
		}
	}

	return podsNames
}
