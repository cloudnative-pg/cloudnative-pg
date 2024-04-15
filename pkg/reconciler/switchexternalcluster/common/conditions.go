package common

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// A condition of switchexternalcluster
const (
	ConditionDesignatedPrimaryTransition = "ExternalClusterDesignatedPrimaryTransition"
	ConditionFence                       = "ExternalClusterFencing"
)

// IsDesignatedPrimaryTransitionRequested returns a boolean indicating if the instance primary should transition to
// designated primary
func IsDesignatedPrimaryTransitionRequested(cluster *apiv1.Cluster) bool {
	return meta.IsStatusConditionFalse(cluster.Status.Conditions, ConditionDesignatedPrimaryTransition)
}

// IsDesignatedPrimaryTransitionCompleted returns a boolean indicating if the transition is complete
func IsDesignatedPrimaryTransitionCompleted(cluster *apiv1.Cluster) bool {
	return meta.IsStatusConditionTrue(cluster.Status.Conditions, ConditionDesignatedPrimaryTransition)
}

// SetDesignatedPrimaryTransitionCompleted creates the condition
func SetDesignatedPrimaryTransitionCompleted(cluster *apiv1.Cluster) {
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    ConditionDesignatedPrimaryTransition,
		Status:  metav1.ConditionTrue,
		Reason:  "TransitionCompleted",
		Message: "Instance Manager has completed the DesignatedPrimary transition",
	})
}

// SetDesignatedPrimaryTransitionRequested creates the condition
func SetDesignatedPrimaryTransitionRequested(cluster *apiv1.Cluster) {
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    ConditionDesignatedPrimaryTransition,
		Status:  metav1.ConditionFalse,
		Reason:  "ExternalClusterAfterCreation",
		Message: "Enabled external cluster after a node was generated",
	})
}

// SetFenceRequest creates the condition
func SetFenceRequest(cluster *apiv1.Cluster) {
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    ConditionFence,
		Status:  metav1.ConditionTrue,
		Reason:  "ExternalClusterAfterCreation",
		Message: "Enabled external cluster after a node was generated",
	})
}
