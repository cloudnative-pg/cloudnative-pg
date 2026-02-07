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

package replicaclusterswitch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/replicaclusterswitch/conditions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Reconcile reconciles the cluster replica cluster switching.
func Reconcile(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	instanceClient remote.InstanceClient,
	instances postgres.PostgresqlStatusList,
) (*ctrl.Result, error) {
	if !cluster.IsReplica() {
		return nil, nil
	}

	contextLogger := log.FromContext(ctx).WithName("replica_cluster")

	if conditions.IsDesignatedPrimaryTransitionCompleted(cluster) {
		return reconcileDemotionToken(ctx, cli, cluster, instanceClient, instances)
	}

	// waiting for the instance manager
	if conditions.IsDesignatedPrimaryTransitionRequested(cluster) {
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

	// TODO(leonardoce): should we fence just the primary?
	if err := utils.NewFencingMetadataExecutor(cli).AddFencing().ForAllInstances().Execute(
		ctx,
		client.ObjectKeyFromObject(cluster),
		cluster,
	); err != nil {
		return nil, fmt.Errorf("while fencing primary cluster to demote it: %w", err)
	}

	if err := status.PatchWithOptimisticLock(
		ctx,
		cli,
		cluster,
		func(cluster *apiv1.Cluster) {
			meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
				Type:    conditions.DesignatedPrimaryTransition,
				Status:  metav1.ConditionFalse,
				Reason:  "ReplicaClusterAfterCreation",
				Message: "Enabled external cluster after a node was generated",
			})
			meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
				Type:    conditions.Fence,
				Status:  metav1.ConditionTrue,
				Reason:  "ReplicaClusterAfterCreation",
				Message: "Enabled external cluster after a node was generated",
			})
			meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
				Type:    conditions.ReplicaClusterSwitch,
				Status:  metav1.ConditionFalse,
				Reason:  "ReplicaEnabledSetTrue",
				Message: "Starting the Replica cluster transition",
			})

			cluster.Status.SwitchReplicaClusterStatus.InProgress = true
		},
	); err != nil {
		return nil, err
	}

	return &ctrl.Result{RequeueAfter: time.Second}, nil
}

func cleanupTransitionMetadata(ctx context.Context, cli client.Client, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx).WithName("replica_cluster_cleanup_transition")
	contextLogger.Info("removing all the unnecessary metadata from the cluster object")

	// TODO(leonardoce): should we unfence just the primary?
	if meta.IsStatusConditionPresentAndEqual(cluster.Status.Conditions, conditions.Fence, metav1.ConditionTrue) &&
		cluster.IsInstanceFenced("*") {
		if err := utils.NewFencingMetadataExecutor(cli).RemoveFencing().ForAllInstances().Execute(
			ctx,
			client.ObjectKeyFromObject(cluster),
			cluster,
		); err != nil {
			return err
		}
	}

	return status.PatchWithOptimisticLock(
		ctx,
		cli,
		cluster,
		func(cluster *apiv1.Cluster) {
			meta.RemoveStatusCondition(&cluster.Status.Conditions, conditions.DesignatedPrimaryTransition)
			meta.RemoveStatusCondition(&cluster.Status.Conditions, conditions.Fence)
			meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
				Type:    conditions.ReplicaClusterSwitch,
				Status:  metav1.ConditionTrue,
				Reason:  "ReplicaEnabledSetTrue",
				Message: "Completed the Replica cluster transition",
			})
			cluster.Status.SwitchReplicaClusterStatus.InProgress = false
		},
	)
}

func reconcileDemotionToken(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	instanceClient remote.InstanceClient,
	instances postgres.PostgresqlStatusList,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).WithName("replica_cluster")

	demotionToken, err := generateDemotionToken(ctx, cluster, instanceClient, instances)
	if err != nil {
		if errors.Is(err, errPostgresNotShutDown) {
			return &ctrl.Result{
				RequeueAfter: 10 * time.Second,
			}, nil
		}

		return nil, err
	}

	if cluster.Status.DemotionToken != demotionToken {
		origCluster := cluster.DeepCopy()
		contextLogger.Info(
			"patching the demotionToken in the  cluster status",
			"value", demotionToken,
			"previousValue", cluster.Status.DemotionToken)
		cluster.Status.DemotionToken = demotionToken

		if err := cli.Status().Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
			return nil, fmt.Errorf("while setting demotion token: %w", err)
		}
	}

	if err := cleanupTransitionMetadata(ctx, cli, cluster); err != nil {
		return nil, fmt.Errorf("while cleaning up demotion transition metadata: %w", err)
	}

	return &ctrl.Result{}, nil
}
