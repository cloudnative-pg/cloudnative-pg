/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"reflect"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	apiv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
)

var (
	log = ctrl.Log.WithName("cluster_predicates")
)

// ClusterPredicate filter events that invoke a Reconciliation loop
// by listening to changes in the Cluster objects which create a new
// generation (the Spec field has been updated) all the events related
// to all Pods that belong to a cluster
type ClusterPredicate struct {
}

// isControlledObject checks if a certain object is controlled
// by a PostgreSQL cluster
func isControlledObject(object client.Object) bool {
	if object == nil {
		return false
	}

	owner := v1.GetControllerOf(v1.Object(object))
	if owner == nil {
		return false
	}

	if owner.Kind != apiv1.ClusterKind {
		return false
	}

	if owner.APIVersion != apiGVString && owner.APIVersion != apiv1alpha1GVString {
		return false
	}

	return true
}

// isCluster checks if a certain object is a cluster
func isCluster(object client.Object) bool {
	_, okv1 := object.(*apiv1.Cluster)
	if okv1 {
		return true
	}

	_, okv1alpha1 := object.(*apiv1alpha1.Cluster)
	return okv1alpha1
}

// Create implements Predicate
func (p ClusterPredicate) Create(e event.CreateEvent) bool {
	if e.Object == nil {
		log.Error(nil, "Create event has no object to update", "event", e)
		return false
	}

	return isCluster(e.Object) || isControlledObject(e.Object)
}

// Delete implements Predicate
func (p ClusterPredicate) Delete(e event.DeleteEvent) bool {
	if e.Object == nil {
		log.Error(nil, "Delete event has no object to update", "event", e)
		return false
	}

	return isCluster(e.Object) || isControlledObject(e.Object)
}

// Generic implements Predicate
func (p ClusterPredicate) Generic(e event.GenericEvent) bool {
	if e.Object == nil {
		log.Error(nil, "Generic event has no object to update", "event", e)
		return false
	}

	return isCluster(e.Object) || isControlledObject(e.Object)
}

// Update implements default UpdateEvent filter for validating generation change
func (ClusterPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil {
		log.Error(nil, "Update event has no old object to update", "event", e)
		return false
	}
	if e.ObjectNew == nil {
		log.Error(nil, "Update event has no new object for update", "event", e)
		return false
	}

	if isCluster(e.ObjectNew) {
		// for update notifications, filter only the updates
		// that result in a change for the cluster specification or
		// in the cluster annotations (needed to restart the Pods)
		differentGenerations := e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
		differentAnnotations := !reflect.DeepEqual(e.ObjectNew.GetAnnotations(), e.ObjectOld.GetAnnotations())

		return differentGenerations || differentAnnotations
	}

	return isControlledObject(e.ObjectNew)
}
