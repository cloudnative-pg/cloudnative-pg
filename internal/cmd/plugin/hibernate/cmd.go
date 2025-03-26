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

package hibernate

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

var (
	hibernateOnCmd = &cobra.Command{
		Use:   "on CLUSTER",
		Short: "Hibernates the cluster named CLUSTER",
		Args:  plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			return annotateCluster(cmd.Context(), plugin.Client, client.ObjectKey{
				Name:      clusterName,
				Namespace: plugin.Namespace,
			}, utils.HibernationAnnotationValueOn)
		},
	}

	hibernateOffCmd = &cobra.Command{
		Use:   "off CLUSTER",
		Short: "Bring the cluster named CLUSTER back from hibernation",
		Args:  plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			return annotateCluster(cmd.Context(), plugin.Client, client.ObjectKey{
				Name:      clusterName,
				Namespace: plugin.Namespace,
			}, utils.HibernationAnnotationValueOff)
		},
	}
)

// NewCmd initializes the hibernate command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "hibernate",
		Short:   `Hibernation related commands`,
		GroupID: plugin.GroupIDCluster,
	}

	cmd.AddCommand(hibernateOnCmd)
	cmd.AddCommand(hibernateOffCmd)

	return cmd
}

func annotateCluster(
	ctx context.Context,
	cli client.Client,
	clusterKey client.ObjectKey,
	value utils.HibernationAnnotationValue,
) error {
	var cluster apiv1.Cluster

	if err := cli.Get(ctx, clusterKey, &cluster); err != nil {
		return fmt.Errorf("failed to get cluster %s: %w", clusterKey.Name, err)
	}

	if cluster.Annotations == nil {
		cluster.SetAnnotations(make(map[string]string))
	}

	origCluster := cluster.DeepCopy()

	cluster.Annotations[utils.HibernationAnnotationName] = string(value)

	if cluster.Annotations[utils.HibernationAnnotationName] == origCluster.Annotations[utils.HibernationAnnotationName] {
		return fmt.Errorf("cluster %s is already in the requested state", clusterKey.Name)
	}

	if err := cli.Patch(ctx, &cluster, client.MergeFrom(origCluster)); err != nil {
		return fmt.Errorf("failed to patch cluster %s: %w", clusterKey.Name, err)
	}

	return nil
}
