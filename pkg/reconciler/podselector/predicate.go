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
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// ExternalPodsPredicate filters pod events to only pass through
// pods that are not owned by a Cluster resource and where the
// pod's IP or labels have changed (for updates).
func ExternalPodsPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return !isOwnedByCluster(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if isOwnedByCluster(e.ObjectNew) {
				return false
			}
			// Only reconcile if the pod's IP or labels changed,
			// since those are the only fields that affect resolved IPs.
			oldPod, ok1 := e.ObjectOld.(*corev1.Pod)
			newPod, ok2 := e.ObjectNew.(*corev1.Pod)
			if ok1 && ok2 {
				return !slices.Equal(oldPod.Status.PodIPs, newPod.Status.PodIPs) ||
					!maps.Equal(oldPod.Labels, newPod.Labels) ||
					(oldPod.DeletionTimestamp == nil) != (newPod.DeletionTimestamp == nil)
			}
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return !isOwnedByCluster(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return !isOwnedByCluster(e.Object)
		},
	}
}

// isOwnedByCluster returns true if the object is owned by a Cluster resource.
func isOwnedByCluster(obj client.Object) bool {
	owner := metav1.GetControllerOf(obj)
	if owner == nil {
		return false
	}
	return owner.Kind == apiv1.ClusterKind &&
		owner.APIVersion == apiv1.SchemeGroupVersion.String()
}
