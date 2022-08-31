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

package controllers

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

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
			return oldOk && newOk && oldNode.Spec.Unschedulable != newNode.Spec.Unschedulable
		},
		CreateFunc: func(createEvent event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(createEvent event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(genericEvent event.GenericEvent) bool {
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
	_, hasLabel := obj.GetLabels()[specs.WatchedLabelName]
	return hasLabel
}
