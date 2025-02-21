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

package controller

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// checks if taints are different
func areTaintsSame(t1 []corev1.Taint, t2 []corev1.Taint) bool {
	if len(t1) != len(t2) {
		return false // Different number of taints
	}

	// Create maps to store taints by key for efficient lookup
	m1 := make(map[string]corev1.Taint, len(t1))
	m2 := make(map[string]corev1.Taint, len(t2))

	for _, taint := range t1 {
		m1[taint.Key] = taint
	}
	for _, taint := range t2 {
		m2[taint.Key] = taint
	}

	if len(m1) != len(m2) {
		return false // Different number of unique keys
	}

	// Check if the keys are the same and the values/effects match
	for key, taint1 := range m1 {
		taint2, ok := m2[key]
		if !ok || taint1.Value != taint2.Value || taint1.Effect != taint2.Effect {
			return false // Key not found in t2 or values/effects don't match
		}
	}

	return true
}

var (
	isUsefulConfigMap = func(object client.Object) bool {
		return isOwnedByClusterOrSatisfiesPredicate(object, func(object client.Object) bool {
			_, ok := object.(*corev1.ConfigMap)
			return ok && hasReloadLabelSet(object)
		})
	}

	isUsefulClusterSecret = func(object client.Object) bool {
		return isOwnedByClusterOrSatisfiesPredicate(object, func(object client.Object) bool {
			_, ok := object.(*corev1.Secret)
			return ok && hasReloadLabelSet(object)
		})
	}

	configMapsPredicate = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isUsefulConfigMap(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isUsefulConfigMap(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return isUsefulConfigMap(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isUsefulConfigMap(e.ObjectNew)
		},
	}

	secretsPredicate = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isUsefulClusterSecret(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isUsefulClusterSecret(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return isUsefulClusterSecret(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isUsefulClusterSecret(e.ObjectNew)
		},
	}

	nodesPredicate = predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, oldOk := e.ObjectOld.(*corev1.Node)
			newNode, newOk := e.ObjectNew.(*corev1.Node)
			if !oldOk || !newOk {
				return false
			}

			return areTaintsSame(oldNode.Spec.Taints, newNode.Spec.Taints)
		},
		CreateFunc: func(_ event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(_ event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}
)

func isOwnedByClusterOrSatisfiesPredicate(
	object client.Object,
	predicate func(client.Object) bool,
) bool {
	_, owned := IsOwnedByCluster(object)
	return owned || predicate(object)
}

func hasReloadLabelSet(obj client.Object) bool {
	_, hasLabel := obj.GetLabels()[utils.WatchedLabelName]
	return hasLabel
}
