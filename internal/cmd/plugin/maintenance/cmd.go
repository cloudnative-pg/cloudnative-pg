/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package maintenance

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewCmd creates the new 'maintenance' command
func NewCmd() *cobra.Command {
	var allNamespaces,
		reusePVC,
		confirmationRequired bool

	maintenanceCmd := &cobra.Command{
		Use:   "maintenance [set/unset]",
		Short: "Sets or removes maintenance mode from clusters",
	}

	maintenanceCmd.AddCommand(&cobra.Command{
		Use:   "set [cluster]",
		Short: "Sets maintenance mode",
		Long: "This command will set maintenance mode on a single cluster or on all clusters " +
			"in the current namespace if not specified differently through flags",
		Args: cobra.MaximumNArgs(1),
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
