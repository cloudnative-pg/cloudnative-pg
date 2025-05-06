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

package destroy

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// NewCmd create the new "destroy" subcommand
func NewCmd() *cobra.Command {
	destroyCmd := &cobra.Command{
		Use:     "destroy CLUSTER INSTANCE",
		Short:   "Destroy the instance named CLUSTER-INSTANCE with the associated PVC",
		GroupID: plugin.GroupIDCluster,
		Args:    plugin.RequiresArguments(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clusterName := args[0]
			instance := args[1]
			if _, err := strconv.Atoi(args[1]); err == nil {
				instance = fmt.Sprintf("%s-%s", clusterName, instance)
			}

			keepPVC, _ := cmd.Flags().GetBool("keep-pvc")
			force, _ := cmd.Flags().GetBool("force")
			if !force {
				if err := ensureNotLastReadyPVC(ctx, clusterName, instance, force); err != nil {
					return err
				}
			}
			return Destroy(ctx, clusterName, instance, keepPVC)
		},
	}

	destroyCmd.Flags().BoolP("keep-pvc", "k", false,
		"Keep the PVC but detach it from instance")

	destroyCmd.Flags().BoolP("force", "f", false,
		"Force the deletion, even if it is the last remaining instance")

	return destroyCmd
}

// ensureNotLastReadyPVC checks that removing the target PVC won't remove
// the last "ready" PVC in the cluster. PVCs that are in the process of being
// deleted (non-zero DeletionTimestamp) are ignored.
func ensureNotLastReadyPVC(ctx context.Context, clusterName, pvcName string, force bool) error {
	// 1. List all PVCs in the cluster namespace that match the cluster label.
	var pvcList corev1.PersistentVolumeClaimList
	if err := plugin.Client.List(
		ctx,
		&pvcList,
		client.InNamespace(plugin.Namespace),
		client.MatchingLabels{
			utils.ClusterLabelName: clusterName,
		},
	); err != nil {
		return fmt.Errorf("error listing PVCs for cluster %q: %v", clusterName, err)
	}

	// 2. Count how many PVCs are in a "ready" state and *not* being deleted.
	//    Also check if the target PVC is among them.
	var totalReadyPVCs int
	var isTargetPVCReady bool

	for _, pvc := range pvcList.Items {
		// Skip PVCs that are being deleted (non-nil DeletionTimestamp).
		if pvc.ObjectMeta.DeletionTimestamp != nil && !pvc.ObjectMeta.DeletionTimestamp.IsZero() {
			continue
		}

		// Check the annotation for "ready" status.
		if pvc.Annotations[utils.PVCStatusAnnotationName] == "ready" {
			totalReadyPVCs++
			if pvc.Name == pvcName {
				isTargetPVCReady = true
			}
		}
	}

	// 3. If the target PVC isn't "ready," removing it doesn't affect the
	//    last "ready" PVC scenario.
	if !isTargetPVCReady {
		return nil
	}

	// 4. If removing this PVC would remove the last "ready" PVC, we need
	//    the 'force' flag to be true. Otherwise, return an error.
	const lastReadyPVCError = "cannot remove the last 'ready' PVC in cluster %q: %q (use --force to override)"
	if totalReadyPVCs == 1 && !force {
		return fmt.Errorf(lastReadyPVCError, clusterName, pvcName)
	}

	// If totalReadyPVCs != 1 or force is true, the function continues execution

	return nil
}
