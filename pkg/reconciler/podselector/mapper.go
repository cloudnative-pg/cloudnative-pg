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

package podselector

import (
	"context"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// clusterMatchesPod returns true if any of the cluster's podSelectorRefs
// match the given pod's labels.
func clusterMatchesPod(cluster *apiv1.Cluster, pod *corev1.Pod) bool {
	for i := range cluster.Spec.PodSelectorRefs {
		ref := &cluster.Spec.PodSelectorRefs[i]
		selector, err := metav1.LabelSelectorAsSelector(&ref.Selector)
		if err != nil {
			continue
		}
		if selector.Matches(labels.Set(pod.Labels)) {
			return true
		}
	}
	return false
}

// MapExternalPodsToClusters returns a handler that maps pod events to
// Cluster reconcile requests when the pod matches any Cluster's podSelectorRefs.
func MapExternalPodsToClusters(c client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			return nil
		}

		var clusterList apiv1.ClusterList
		if err := c.List(ctx, &clusterList,
			client.InNamespace(pod.Namespace),
		); err != nil {
			log.FromContext(ctx).Error(err, "Failed to list clusters for pod selector mapping",
				"podName", pod.Name, "namespace", pod.Namespace)
			return nil
		}

		var requests []reconcile.Request
		for i := range clusterList.Items {
			cluster := &clusterList.Items[i]
			if len(cluster.Spec.PodSelectorRefs) == 0 {
				continue
			}
			if clusterMatchesPod(cluster, pod) {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(cluster),
				})
			}
		}
		return requests
	}
}
