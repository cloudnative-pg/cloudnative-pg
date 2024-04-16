package replicaclusterswitch

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

const (
	conditionDesignatedPrimaryTransition = "ReplicaClusterDesignatedPrimaryTransition"
	conditionFence                       = "ReplicaClusterFencing"
	// ConditionReplicaClusterSwitch is is a consumer facing condition and should not be used to decide actions
	// inside the code
	ConditionReplicaClusterSwitch = "ReplicaClusterSwitch"
)

// IsDesignatedPrimaryTransitionRequested returns a boolean indicating if the instance primary should transition to
// designated primary
func IsDesignatedPrimaryTransitionRequested(cluster *apiv1.Cluster) bool {
	return meta.IsStatusConditionFalse(cluster.Status.Conditions, conditionDesignatedPrimaryTransition)
}

// isDesignatedPrimaryTransitionCompleted returns a boolean indicating if the transition is complete
func isDesignatedPrimaryTransitionCompleted(cluster *apiv1.Cluster) bool {
	return meta.IsStatusConditionTrue(cluster.Status.Conditions, conditionDesignatedPrimaryTransition)
}

// SetDesignatedPrimaryTransitionCompleted creates the condition
func SetDesignatedPrimaryTransitionCompleted(cluster *apiv1.Cluster) {
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    conditionDesignatedPrimaryTransition,
		Status:  metav1.ConditionTrue,
		Reason:  "TransitionCompleted",
		Message: "Instance Manager has completed the DesignatedPrimary transition",
	})
}
