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

package replicaclusterswitch

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Reconcile reconciles the cluster replica cluster switching.
func Reconcile(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	instances postgres.PostgresqlStatusList,
) (*ctrl.Result, error) {
	if !cluster.IsReplica() {
		return nil, nil
	}

	contextLogger := log.FromContext(ctx).WithName("replica_cluster")

	if isDesignatedPrimaryTransitionCompleted(cluster) {
		return &ctrl.Result{RequeueAfter: time.Second}, cleanupTransitionMetadata(ctx, cli, cluster)
	}

	// waiting for the instance manager
	if IsDesignatedPrimaryTransitionRequested(cluster) {
		contextLogger.Info("waiting for the instance manager to transition the primary instance to a designated primary")
		return nil, nil
	}

	if !containsPrimaryInstance(instances) {
		// no primary instance present means that we have no work to do
		return nil, nil
	}

	return startTransition(ctx, cli, cluster)
}

func containsPrimaryInstance(instances postgres.PostgresqlStatusList) bool {
	for _, item := range instances.Items {
		if item.IsPrimary {
			return true
		}
	}

	return false
}

func startTransition(ctx context.Context, cli client.Client, cluster *apiv1.Cluster) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).WithName("replica_cluster_start_transition")
	contextLogger.Info("starting the transition to replica cluster")
	err := utils.NewFencingMetadataExecutor(cli).AddFencing().ForAllInstances().Execute(
		ctx,
		client.ObjectKeyFromObject(cluster),
		cluster,
	)

	origCluster := cluster.DeepCopy()
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    conditionDesignatedPrimaryTransition,
		Status:  metav1.ConditionFalse,
		Reason:  "ReplicaClusterAfterCreation",
		Message: "Enabled external cluster after a node was generated",
	})
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    conditionFence,
		Status:  metav1.ConditionTrue,
		Reason:  "ReplicaClusterAfterCreation",
		Message: "Enabled external cluster after a node was generated",
	})
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    ConditionReplicaClusterSwitch,
		Status:  metav1.ConditionFalse,
		Reason:  "ReplicaEnabledSetTrue",
		Message: "Starting the Replica cluster transition",
	})

	cluster.Status.SwitchReplicaClusterStatus.InProgress = true
	if err := cli.Status().Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
		return nil, err
	}

	return &ctrl.Result{RequeueAfter: time.Second}, err
}

func cleanupTransitionMetadata(ctx context.Context, cli client.Client, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx).WithName("replica_cluster_cleanup_transition")
	contextLogger.Info("removing all the unnecessary metadata from the cluster object")
	if meta.IsStatusConditionPresentAndEqual(cluster.Status.Conditions, conditionFence, metav1.ConditionTrue) &&
		cluster.IsInstanceFenced("*") {
		if err := utils.NewFencingMetadataExecutor(cli).RemoveFencing().ForAllInstances().Execute(
			ctx,
			client.ObjectKeyFromObject(cluster),
			cluster,
		); err != nil {
			return err
		}
	}
	origCluster := cluster.DeepCopy()
	meta.RemoveStatusCondition(&cluster.Status.Conditions, conditionDesignatedPrimaryTransition)
	meta.RemoveStatusCondition(&cluster.Status.Conditions, conditionFence)
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    ConditionReplicaClusterSwitch,
		Status:  metav1.ConditionTrue,
		Reason:  "ReplicaEnabledSetTrue",
		Message: "Completed the Replica cluster transition",
	})
	cluster.Status.SwitchReplicaClusterStatus.InProgress = false

	return cli.Status().Patch(ctx, cluster, client.MergeFrom(origCluster))
}
