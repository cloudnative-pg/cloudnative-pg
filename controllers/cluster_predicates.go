package controllers

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
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
func isControlledObject(objectMeta metav1.Object) bool {
	owner := metav1.GetControllerOf(objectMeta)
	if owner == nil {
		return false
	}

	if owner.APIVersion != apiGVString || owner.Kind != v1alpha1.ClusterKind {
		return false
	}

	return true
}

// isCluster checks if a certain object is a cluster
func isCluster(object runtime.Object) bool {
	_, ok := object.(*v1alpha1.Cluster)
	return ok
}

// Create implements Predicate
func (p ClusterPredicate) Create(e event.CreateEvent) bool {
	if e.Meta == nil {
		log.Error(nil, "Create event has no metadata", "event", e)
		return false
	}
	if e.Object == nil {
		log.Error(nil, "Create event has no runtime object to update", "event", e)
		return false
	}

	return isCluster(e.Object) || isControlledObject(e.Meta)
}

// Delete implements Predicate
func (p ClusterPredicate) Delete(e event.DeleteEvent) bool {
	if e.Meta == nil {
		log.Error(nil, "Delete event has no metadata", "event", e)
		return false
	}
	if e.Object == nil {
		log.Error(nil, "Delete event has no runtime object to update", "event", e)
		return false
	}

	return isCluster(e.Object) || isControlledObject(e.Meta)
}

// Generic implements Predicate
func (p ClusterPredicate) Generic(e event.GenericEvent) bool {
	if e.Meta == nil {
		log.Error(nil, "Generic event has no metadata", "event", e)
		return false
	}
	if e.Object == nil {
		log.Error(nil, "Generic event has no runtime object to update", "event", e)
		return false
	}

	return isCluster(e.Object) || isControlledObject(e.Meta)
}

// Update implements default UpdateEvent filter for validating generation change
func (ClusterPredicate) Update(e event.UpdateEvent) bool {
	if e.MetaOld == nil {
		log.Error(nil, "Update event has no old metadata", "event", e)
		return false
	}
	if e.ObjectOld == nil {
		log.Error(nil, "Update event has no old runtime object to update", "event", e)
		return false
	}
	if e.ObjectNew == nil {
		log.Error(nil, "Update event has no new runtime object for update", "event", e)
		return false
	}
	if e.MetaNew == nil {
		log.Error(nil, "Update event has no new metadata", "event", e)
		return false
	}

	if isCluster(e.ObjectNew) {
		// for update notifications, filter only the updates
		// that result in a change for the cluster specification
		return e.MetaNew.GetGeneration() != e.MetaOld.GetGeneration()
	}

	return isControlledObject(e.MetaNew)
}
