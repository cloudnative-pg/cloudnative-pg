/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
)

var utilsLog = log.WithName("utils")

// IsPodReady reports whether the Pod has its PodReady condition set to True.
//
// The PodReady condition is kept up-to-date by two independent control-plane
// actors:
//
//   - the kubelet on the pod's node, while the node is healthy: the transition
//     from True to False happens within FailureThreshold consecutive probe
//     periods of the underlying container becoming unhealthy (about 30s with
//     stock defaults: PeriodSeconds=10, FailureThreshold=3, unless the user
//     overrides them via `.spec.probes.readiness`).
//   - the node lifecycle controller when the node stops reporting to the API
//     server; once the node transitions to `Unknown` (after
//     `--node-monitor-grace-period`, 40s by default on Kubernetes 1.29-1.31
//     and 50s on 1.32+) the controller calls `MarkPodsNotReady`, which flips
//     PodReady to False on every pod of that node. With stock defaults the
//     operator observes the flip about 40 to 55 seconds after the node
//     becomes unreachable.
//
// Note that `tolerationSeconds` on the `node.kubernetes.io/unreachable` taint
// (300s by default) controls pod eviction, not this condition: PodReady has
// already flipped to False well before eviction happens.
//
// This helper is the source of truth for "is this pod serviceable right now?"
// across the operator: failover election, backup target selection,
// failover-quorum reachability, offline volume-snapshot fencing, and
// user-visible cluster health classification.
func IsPodReady(pod corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

// PodHasContainerStatuses checks if a Pod has container status elements
func PodHasContainerStatuses(pod corev1.Pod) bool {
	return len(pod.Status.ContainerStatuses) > 0
}

// IsPodActive checks if a pod is active, copied from:
// https://github.com/kubernetes/kubernetes/blob/1bd0077/test/e2e/framework/pod/resource.go#L664
func IsPodActive(p corev1.Pod) bool {
	return corev1.PodSucceeded != p.Status.Phase &&
		corev1.PodPending != p.Status.Phase &&
		corev1.PodFailed != p.Status.Phase &&
		p.DeletionTimestamp == nil
}

// IsPodUnschedulable check if a Pod is unschedulable
func IsPodUnschedulable(p *corev1.Pod) bool {
	if corev1.PodPending != p.Status.Phase {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled &&
			c.Status == corev1.ConditionFalse &&
			c.Reason == corev1.PodReasonUnschedulable {
			return true
		}
	}

	return false
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
