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

package snapshot

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// NewCmd implements the `snapshot` subcommand
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "snapshot",
		Short:   "Manage VolumeSnapshot exclusions for a cluster",
		GroupID: plugin.GroupIDCluster,
	}

	cmd.AddCommand(newEnableCmd())
	cmd.AddCommand(newDisableCmd())

	return cmd
}

func newEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable CLUSTER SNAPSHOT",
		Short: "Re-enable a previously excluded VolumeSnapshot for replica creation",
		Args:  plugin.RequiresArguments(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return Enable(context.Background(), plugin.Client, plugin.Namespace, args[0], args[1])
		},
	}
}

func newDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable CLUSTER SNAPSHOT",
		Short: "Exclude a VolumeSnapshot from being used for replica creation",
		Args:  plugin.RequiresArguments(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return Disable(context.Background(), plugin.Client, plugin.Namespace, args[0], args[1])
		},
	}
}
