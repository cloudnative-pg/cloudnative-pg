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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// cancelSwitchoverMarker is stored as the current primary when a switchover
// is canceled. It names no instance, so the current and the target primary
// still differ and the old primary knows it must act. Setting the target back
// to the current primary instead would leave the old primary stuck.
const cancelSwitchoverMarker = "cancel"

// Promote promotes an instance in a cluster. While a switchover is in
// progress it can only cancel it, by promoting the current primary back, or
// redirect a canceled one. Both need force.
func Promote(ctx context.Context, cli client.Client,
	namespace, clusterName, serverName string, force bool,
) error {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: clusterName}, &cluster)
	if err != nil {
		return fmt.Errorf("cluster %s not found in namespace %s: %w", clusterName, namespace, err)
	}

	currentPrimary := cluster.Status.CurrentPrimary
	targetPrimary := cluster.Status.TargetPrimary
	transitionInProgress := currentPrimary != "" && currentPrimary != targetPrimary
	// A failover is not a switchover: it has no target yet (pending) or has
	// chosen it. Promoting lets it proceed. There is nothing to cancel.
	failoverInProgress := transitionInProgress &&
		(targetPrimary == apiv1.PendingFailoverMarker || cluster.Status.Phase == apiv1.PhaseFailOver)
	switchoverInProgress := transitionInProgress && !failoverInProgress

	// The marker is not an instance: messages must not name it
	cancelInProgress := currentPrimary == cancelSwitchoverMarker

	// If server name is equal to target primary, there is no need to promote
	// that instance
	if targetPrimary == serverName {
		switch {
		case cancelInProgress:
			fmt.Printf("A switchover was canceled and the promotion of %s is already in progress\n", serverName)
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

	canceling := false
	if switchoverInProgress {
		switch {
		case cancelInProgress:
			// Redirect the canceled switchover, so that it cannot get
			// stuck on an old primary that does not come back
			if !force {
				return fmt.Errorf("a switchover was canceled and %s is being promoted back: "+
					"promoting %s now changes that. Use --force to promote it anyway",
					targetPrimary, serverName)
			}
		case serverName != currentPrimary:
			return fmt.Errorf("a switchover from %s to %s is in progress and %s cannot be promoted until "+
				"it completes: to cancel it, promote %s back",
				currentPrimary, targetPrimary, serverName, currentPrimary)
		case !force:
			return fmt.Errorf("a switchover from %s to %s is in progress: promoting %s now cancels it. "+
				"If %s already promoted, the cluster can end up with two primaries "+
				"and the data written on it is lost. Use --force to cancel the switchover",
				currentPrimary, targetPrimary, serverName, targetPrimary)
		case !utils.IsPodActive(pod):
			return fmt.Errorf("%s is not active and cannot take the primary role back", serverName)
		default:
			canceling = true
		}
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
		if canceling {
			cluster.Status.CurrentPrimary = cancelSwitchoverMarker
		}
		cluster.Status.TargetPrimary = serverName
		cluster.Status.TargetPrimaryTimestamp = pgTime.GetCurrentTimestamp()
		// A failover stays a failover, or the next promote would see a switchover
		if !failoverInProgress {
			cluster.Status.Phase = apiv1.PhaseSwitchover
			cluster.Status.PhaseReason = fmt.Sprintf("Switching over to %v", serverName)
		}
		status.SetClusterReadyCondition(cluster)
	}
	if err := status.PatchWithOptimisticLock(ctx, cli, &cluster, reconcileTargetPrimaryFunc); err != nil {
		return err
	}
	if primaryChanged {
		return fmt.Errorf("the primary of cluster %s changed while promoting %s: "+
			"nothing was done, check the status of the cluster and try again", clusterName, serverName)
	}
	if canceling {
		fmt.Printf("The switchover to %s in cluster %s is canceled, node %s will be promoted back\n",
			targetPrimary, clusterName, serverName)
		return nil
	}
	fmt.Printf("Node %s in cluster %s will be promoted\n", serverName, clusterName)
	return nil
}
