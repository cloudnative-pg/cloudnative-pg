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

package status

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// RegisterPhase update phase in the status cluster with the
// proper reason
func RegisterPhase(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	phase string,
	reason string,
) error {
	existingCluster := cluster.DeepCopy()
	return RegisterPhaseWithOrigCluster(ctx, cli, cluster, existingCluster, phase, reason)
}

// RegisterPhaseWithOrigCluster update phase in the status cluster with the
// proper reason, it also receives an origCluster to preserve other modifications done to the status
func RegisterPhaseWithOrigCluster(
	ctx context.Context,
	cli client.Client,
	modifiedCluster *apiv1.Cluster,
	origCluster *apiv1.Cluster,
	phase string,
	reason string,
) error {
	// we ensure that the modifiedCluster conditions aren't nil before operating
	if modifiedCluster.Status.Conditions == nil {
		modifiedCluster.Status.Conditions = []metav1.Condition{}
	}

	modifiedCluster.Status.Phase = phase
	modifiedCluster.Status.PhaseReason = reason

	condition := metav1.Condition{
		Type:    string(apiv1.ConditionClusterReady),
		Status:  metav1.ConditionFalse,
		Reason:  string(apiv1.ClusterIsNotReady),
		Message: "Cluster Is Not Ready",
	}

	if modifiedCluster.Status.Phase == apiv1.PhaseHealthy {
		condition = metav1.Condition{
			Type:    string(apiv1.ConditionClusterReady),
			Status:  metav1.ConditionTrue,
			Reason:  string(apiv1.ClusterReady),
			Message: "Cluster is Ready",
		}
	}

	meta.SetStatusCondition(&modifiedCluster.Status.Conditions, condition)

	if !reflect.DeepEqual(origCluster, modifiedCluster) {
		if err := cli.Status().Patch(ctx, modifiedCluster, client.MergeFrom(origCluster)); err != nil {
			return err
		}
	}

	return nil
}
