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

package disk

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// NewCmd create the new "disk" subcommand
func NewCmd() *cobra.Command {
	diskCmd := &cobra.Command{
		Use:     "disk",
		Short:   "Disk usage and health commands",
		GroupID: plugin.GroupIDTroubleshooting,
	}

	diskCmd.AddCommand(newStatusCmd())

	return diskCmd
}

// newStatusCmd creates the "disk status" subcommand
func newStatusCmd() *cobra.Command {
	statusCmd := &cobra.Command{
		Use:   "status CLUSTER",
		Short: "Get disk usage and WAL health status for a cluster",
		Long: `Display disk usage, WAL health, and auto-resize status for all instances in a cluster.

Shows:
- Per-instance disk usage for data, WAL, and tablespace volumes
- WAL archive health status
- Inactive replication slots
- Recent auto-resize events`,
		Args: plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if strings.HasPrefix(toComplete, "-") {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			clusterName := args[0]

			output, _ := cmd.Flags().GetString("output")

			return Status(ctx, clusterName, plugin.OutputFormat(output))
		},
	}

	statusCmd.Flags().StringP(
		"output", "o", "text", "Output format. One of text|json")

	return statusCmd
}
