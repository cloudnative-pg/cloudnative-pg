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

package status

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// Transaction is a function that modifies a cluster
type Transaction func(cluster *apiv1.Cluster)

// SetClusterReadyCondition updates the cluster's readiness condition
// according to the cluster phase
func SetClusterReadyCondition(cluster *apiv1.Cluster) {
	if cluster.Status.Conditions == nil {
		cluster.Status.Conditions = []metav1.Condition{}
	}

	condition := metav1.Condition{
		Type:    string(apiv1.ConditionClusterReady),
		Status:  metav1.ConditionFalse,
		Reason:  string(apiv1.ClusterIsNotReady),
		Message: "Cluster Is Not Ready",
	}

	if cluster.Status.Phase == apiv1.PhaseHealthy {
		condition = metav1.Condition{
			Type:    string(apiv1.ConditionClusterReady),
			Status:  metav1.ConditionTrue,
			Reason:  string(apiv1.ClusterReady),
			Message: "Cluster is Ready",
		}
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, condition)
}

// SetPhase is a transaction that sets the cluster phase and reason
func SetPhase(phase string, reason string) Transaction {
	return func(cluster *apiv1.Cluster) {
		cluster.Status.Phase = phase
		cluster.Status.PhaseReason = reason
	}
}

// SetImage is a transaction that sets the cluster image
func SetImage(image string) Transaction {
	return func(cluster *apiv1.Cluster) {
		cluster.Status.Image = image
	}
}

// SetPGDataImageInfo is a transaction that sets the PGDataImageInfo
func SetPGDataImageInfo(imageInfo *apiv1.ImageInfo) Transaction {
	return func(cluster *apiv1.Cluster) {
		cluster.Status.PGDataImageInfo = imageInfo
	}
}

// SetTimelineID is a transaction that sets the cluster timeline ID
func SetTimelineID(timelineID int) Transaction {
	return func(cluster *apiv1.Cluster) {
		cluster.Status.TimelineID = timelineID
	}
}
