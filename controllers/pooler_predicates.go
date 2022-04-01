/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
