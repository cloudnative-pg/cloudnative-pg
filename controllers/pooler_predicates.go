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
)

// secretsPoolerPredicate contains the set of predicate functions of the pooler secrets
var (
	secretsPoolerPredicate = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isUsefulPoolerSecret(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isUsefulPoolerSecret(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return isUsefulPoolerSecret(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isUsefulPoolerSecret(e.ObjectNew)
		},
	}
)

func isOwnedByPoolerOrSatisfiesPredicate(
	object client.Object,
	predicate func(client.Object) bool,
) bool {
	_, owned := isOwnedByPooler(object)
	return owned || predicate(object)
}

func isUsefulPoolerSecret(object client.Object) bool {
	return isOwnedByPoolerOrSatisfiesPredicate(object, func(object client.Object) bool {
		_, ok := object.(*corev1.Secret)
		return ok && hasReloadLabelSet(object)
	})
}
