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

package maintenance

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// NewCmd creates the new 'maintenance' command
func NewCmd() *cobra.Command {
	var allNamespaces,
		reusePVC,
		confirmationRequired bool

	maintenanceCmd := &cobra.Command{
		Use:     "maintenance [set/unset]",
		Short:   "Sets or removes maintenance mode from clusters",
		GroupID: plugin.GroupIDCluster,
	}

	maintenanceCmd.AddCommand(&cobra.Command{
		Use:   "set [cluster]",
		Short: "Sets maintenance mode",
		Long: "This command will set maintenance mode on a single cluster or on all clusters " +
			"in the current namespace if not specified differently through flags",
		Args: cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var clusterName string
			if len(args) > 0 {
				if allNamespaces {
					return fmt.Errorf("can not specify --all-namespaces and a cluster: %s", args[0])
				}
				clusterName = args[0]
			}
			return Maintenance(cmd.Context(), allNamespaces, reusePVC, confirmationRequired, clusterName, true)
		},
	})

	maintenanceCmd.AddCommand(&cobra.Command{
		Use:   "unset [cluster]",
		Short: "Removes maintenance mode",
		Long: "This command will unset maintenance mode on a single cluster or on all clusters " +
			"in the current namespace if not specified differently through flags",
		Args: cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var clusterName string
			if len(args) > 0 {
				if allNamespaces {
					return fmt.Errorf("can not specify --all-namespaces and a cluster: %s", args[0])
				}
				clusterName = args[0]
			}
			return Maintenance(cmd.Context(), allNamespaces, reusePVC, confirmationRequired, clusterName, false)
		},
	})

	maintenanceCmd.PersistentFlags().BoolVarP(&allNamespaces,
		"all-namespaces", "A", false, "Apply operation to all clusters in all namespaces")
	maintenanceCmd.PersistentFlags().BoolVar(&reusePVC,
		"reusePVC", false, "Optional flag to set 'reusePVC' to true")
	maintenanceCmd.PersistentFlags().BoolVarP(&confirmationRequired,
		"yes", "y", false, "Whether it should ask for confirmation before proceeding")

	return maintenanceCmd
}
