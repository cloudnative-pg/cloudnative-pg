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

package destroy

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
			node := args[1]
			if _, err := strconv.Atoi(args[1]); err == nil {
				node = fmt.Sprintf("%s-%s", clusterName, node)
			}

			keepPVC, _ := cmd.Flags().GetBool("keep-pvc")
			force, _ := cmd.Flags().GetBool("force")
			if !force {
				if err := ensureOtherRunningInstances(ctx, clusterName, node); err != nil {
					return err
				}
			}
			return Destroy(ctx, clusterName, node, keepPVC)
		},
	}

	destroyCmd.Flags().BoolP("keep-pvc", "k", false,
		"Keep the PVC but detach it from instance")

	destroyCmd.Flags().BoolP("force", "f", false,
		"Force the deletion, even if it is the last remaining instance")

	return destroyCmd
}

func ensureOtherRunningInstances(ctx context.Context, clusterName string, node string) error {
	// List all pods for the cluster
	var podList corev1.PodList
	if err := plugin.Client.List(ctx, &podList, client.InNamespace(plugin.Namespace), client.MatchingLabels{
		utils.ClusterLabelName: clusterName,
		utils.PodRoleLabelName: string(utils.PodRoleInstance),
	}); err != nil {
		return fmt.Errorf("error listing pods for cluster %s: %v", clusterName, err)
	}

	var otherRunningInstancesCount int
	for _, pod := range podList.Items {
		if pod.Name == node {
			continue
		}

		if utils.IsPodReady(pod) && utils.IsPodActive(pod) {
			otherRunningInstancesCount++
		}
	}

	if otherRunningInstancesCount > 0 {
		return nil
	}

	fmt.Printf(
		"WARNING: No running instances remain. Are you sure you want to attempt destroying the instance? [y/N]: ",
	)
	var response string
	if _, err := fmt.Scanln(&response); err != nil {
		return err
	}
	if strings.ToLower(response) != "y" {
		return fmt.Errorf("operation aborted by the user")
	}

	return nil
}
