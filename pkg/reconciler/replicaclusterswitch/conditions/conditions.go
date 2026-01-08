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

// Package conditions contains utilities for working with replica cluster switch conditions
package conditions

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

const (
	// DesignatedPrimaryTransition is the condition type for designated primary transitions
	DesignatedPrimaryTransition = "ReplicaClusterDesignatedPrimaryTransition"

	// Fence is the condition type for replica cluster fencing
	Fence = "ReplicaClusterFencing"

	// ReplicaClusterSwitch is a consumer facing condition and should not be used to decide actions
	// inside the code
	ReplicaClusterSwitch = "ReplicaClusterSwitch"
)

// IsDesignatedPrimaryTransitionRequested returns a boolean indicating if the instance primary should transition to
// designated primary
func IsDesignatedPrimaryTransitionRequested(cluster *apiv1.Cluster) bool {
	return meta.IsStatusConditionFalse(cluster.Status.Conditions, DesignatedPrimaryTransition)
}

// IsDesignatedPrimaryTransitionCompleted returns a boolean indicating if the transition is complete
func IsDesignatedPrimaryTransitionCompleted(cluster *apiv1.Cluster) bool {
	return meta.IsStatusConditionTrue(cluster.Status.Conditions, DesignatedPrimaryTransition)
}

// SetDesignatedPrimaryTransitionCompleted creates the condition
func SetDesignatedPrimaryTransitionCompleted(cluster *apiv1.Cluster) {
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    DesignatedPrimaryTransition,
		Status:  metav1.ConditionTrue,
		Reason:  "TransitionCompleted",
		Message: "Instance Manager has completed the DesignatedPrimary transition",
	})
}
