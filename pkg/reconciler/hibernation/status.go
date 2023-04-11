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

package hibernation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

const (
	// HibernationAnnotationName is the name of the hibernation annotation
	HibernationAnnotationName = "cnpg.io/hibernation"

	// HibernationOff is the value of hibernation annotation when the hibernation
	// has been deactivated for the cluster
	HibernationOff = "off"

	// HibernationOn is the value of hibernation annotation when the hibernation
	// has been requested for the cluster
	HibernationOn = "on"
)

const (
	// HibernationConditionType is the name of the condition representing
	// the hibernation status
	HibernationConditionType = "cnpg.io/hibernation"

	// HibernationConditionReasonWrongAnnotationValue is the value of the hibernation
	// condition that is used when the value of the annotation is not correct
	HibernationConditionReasonWrongAnnotationValue = "WrongAnnotationValue"

	// HibernationConditionReasonHibernated is the value of the hibernation
	// condition that is used when the cluster is hibernated
	HibernationConditionReasonHibernated = "Hibernated"

	// HibernationConditionReasonDeletingPods is the value of the hibernation
	// condition that is used when the operator is deleting
	// the cluster's Pod
	HibernationConditionReasonDeletingPods = "DeletingPods"

	// HibernationConditionReasonWaitingPodsDeletion is the value of the
	// hibernation condition that is used when the operator is waiting for a Pod
	// to be deleted
	HibernationConditionReasonWaitingPodsDeletion = "WaitingPodsDeletion"
)

// ErrInvalidHibernationValue is raised when the hibernation annotation has
// an invalid value
type ErrInvalidHibernationValue struct {
	value string
}

// Error implements the error interface
func (err *ErrInvalidHibernationValue) Error() string {
	return fmt.Sprintf("invalid annotation value: %s", err.value)
}

// EnrichStatus obtains and classifies the hibernation status of the cluster
func EnrichStatus(
	_ context.Context,
	cluster *apiv1.Cluster,
	podList []corev1.Pod,
) {
	hibernationRequested, err := getHibernationAnnotationValue(cluster)
	if err != nil {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    HibernationConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  HibernationConditionReasonWrongAnnotationValue,
			Message: err.Error(),
		})
		return
	}

	if !hibernationRequested {
		meta.RemoveStatusCondition(&cluster.Status.Conditions, HibernationConditionType)
		return
	}

	// We proceed to hibernate the cluster only when it is ready.
	// Hibernating a non-ready cluster may be dangerous since the PVCs
	// won't be completely created.
	if cluster.Status.Phase != apiv1.PhaseHealthy {
		return
	}

	if len(podList) == 0 {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    HibernationConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  HibernationConditionReasonHibernated,
			Message: "Cluster has been hibernated",
		})
		return
	}

	for idx := range podList {
		if podList[idx].DeletionTimestamp != nil {
			meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
				Type:    HibernationConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  HibernationConditionReasonWaitingPodsDeletion,
				Message: fmt.Sprintf("Waiting for %s to be deleted", podList[idx].Name),
			})
			return
		}
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    HibernationConditionType,
		Status:  metav1.ConditionFalse,
		Reason:  HibernationConditionReasonDeletingPods,
		Message: "Hibernation is in progress",
	})
}

func getHibernationAnnotationValue(cluster *apiv1.Cluster) (bool, error) {
	value, ok := cluster.Annotations[HibernationAnnotationName]
	if !ok {
		return false, nil
	}

	switch value {
	case HibernationOn:
		return true, nil

	case HibernationOff:
		return false, nil

	default:
		return false, &ErrInvalidHibernationValue{value: value}
	}
}
