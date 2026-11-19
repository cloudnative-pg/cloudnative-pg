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

// Package promote implement the kubectl-cnpg promote command
package promote

import (
	"context"
	"fmt"

	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
)

// cancelSwitchoverMarker is what the current primary is set to when a
// switchover is cancelled. It is not the name of any instance, so the target
// primary is no longer the current one and the instance that is made the target
// again promotes itself back, as it does for any switchover.
const cancelSwitchoverMarker = "cancel"

// Promote promotes an instance in a cluster. A switchover that is in progress
// can only be cancelled, by promoting the current primary back, and that
// needs force.
func Promote(ctx context.Context, cli client.Client,
	namespace, clusterName, serverName string, force bool,
) error {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: clusterName}, &cluster)
	if err != nil {
		return fmt.Errorf("cluster %s not found in namespace %s: %w", clusterName, namespace, err)
	}

	// A failover waiting for a quorum has the pending marker as target
	// primary, and promoting an instance is how a person decides to go on
	currentPrimary := cluster.Status.CurrentPrimary
	targetPrimary := cluster.Status.TargetPrimary
	switchoverInProgress := currentPrimary != "" &&
		currentPrimary != targetPrimary &&
		targetPrimary != apiv1.PendingFailoverMarker

	// A cancelled switchover has no current primary to name or to promote
	// back, until its target takes over
	cancelInProgress := currentPrimary == cancelSwitchoverMarker

	// If server name is equal to target primary, there is no need to promote
	// that instance
	if targetPrimary == serverName {
		switch {
		case cancelInProgress:
			fmt.Printf("A switchover was cancelled and the promotion of %s is already in progress\n", serverName)
		case switchoverInProgress:
			fmt.Printf("A switchover from %s to %s is already in progress\n", currentPrimary, serverName)
		default:
			fmt.Printf("%s is already the primary node in the cluster\n", serverName)
		}
		return nil
	}

	if cluster.IsInstanceFenced(serverName) {
		return fmt.Errorf("%s is fenced and cannot be promoted", serverName)
	}
	// Check if the Pod exist
	var pod corev1.Pod
	err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: serverName}, &pod)
	if err != nil {
		return fmt.Errorf("new primary node %s not found in namespace %s: %w", serverName, namespace, err)
	}

	cancelling := false
	if switchoverInProgress {
		switch {
		case cancelInProgress:
			return fmt.Errorf("a switchover was cancelled and the promotion of %s is in progress: "+
				"%s cannot be promoted until it completes", targetPrimary, serverName)
		case serverName != currentPrimary:
			return fmt.Errorf("a switchover from %s to %s is in progress and %s cannot be promoted until "+
				"it completes: to cancel it, promote %s back",
				currentPrimary, targetPrimary, serverName, currentPrimary)
		case !force:
			return fmt.Errorf("a switchover from %s to %s is in progress: promoting %s now cancels it. "+
				"Nothing is lost if %s did not promote yet, but if it did, the cluster ends up with two primaries. "+
				"Use --force to cancel the switchover",
				currentPrimary, targetPrimary, serverName, targetPrimary)
		}
		cancelling = true
	}

	// The Pod exists, let's update the cluster's status with the new target primary.
	// The status is read again when it is patched: what was decided above
	// does not hold if the primary changed in the meantime.
	primaryChanged := false
	reconcileTargetPrimaryFunc := func(cluster *apiv1.Cluster) {
		primaryChanged = cluster.Status.CurrentPrimary != currentPrimary ||
			cluster.Status.TargetPrimary != targetPrimary
		if primaryChanged {
			return
		}
		if cancelling {
			cluster.Status.CurrentPrimary = cancelSwitchoverMarker
		}
		cluster.Status.TargetPrimary = serverName
		cluster.Status.TargetPrimaryTimestamp = pgTime.GetCurrentTimestamp()
		cluster.Status.Phase = apiv1.PhaseSwitchover
		cluster.Status.PhaseReason = fmt.Sprintf("Switching over to %v", serverName)
	}
	if err := status.PatchWithOptimisticLock(ctx, cli, &cluster,
		reconcileTargetPrimaryFunc,
		status.SetClusterReadyCondition,
	); err != nil {
		return err
	}
	if primaryChanged {
		return fmt.Errorf("the primary of cluster %s changed while promoting %s: "+
			"nothing was done, check the status of the cluster and try again", clusterName, serverName)
	}
	if cancelling {
		fmt.Printf("The switchover to %s in cluster %s is cancelled, node %s will be promoted back\n",
			targetPrimary, clusterName, serverName)
		return nil
	}
	fmt.Printf("Node %s in cluster %s will be promoted\n", serverName, clusterName)
	return nil
}
