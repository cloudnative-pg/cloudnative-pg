/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

var log = ctrl.Log.WithName("cluster_predicates")

var (
	configMapsPredicate = predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			_, ok := e.ObjectNew.(*corev1.ConfigMap)
			if !ok {
				return false
			}
			_, owned := isOwnedByCluster(e.ObjectNew)
			return owned || hasReloadLabelSet(e.ObjectNew)
		},
	}
	secretsPredicate = predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			_, ok := e.ObjectNew.(*corev1.Secret)
			if !ok {
				return false
			}
			_, owned := isOwnedByCluster(e.ObjectNew)
			return owned || hasReloadLabelSet(e.ObjectNew)
		},
	}
	nodesPredicate = predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, oldOk := e.ObjectOld.(*corev1.Node)
			newNode, newOk := e.ObjectNew.(*corev1.Node)
			return oldOk && newOk && oldNode.Spec.Unschedulable != newNode.Spec.Unschedulable
		},
	}
)

func hasReloadLabelSet(obj client.Object) bool {
	_, hasLabel := obj.GetLabels()[specs.ConfigMapWatchedLabelName]
	return hasLabel
}
