/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package reload

import (
	"context"

	"github.com/spf13/cobra"
)

// NewCmd creates the new "reset" command
func NewCmd() *cobra.Command {
	restartCmd := &cobra.Command{
		Use:   "reload [clusterName]",
		Short: `Reload the cluster`,
		Long:  `Triggers a reconciliation loop for all the cluster's instances, rolling out new configurations if present.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clusterName := args[0]
			return Reload(ctx, clusterName)
		},
	}

	return restartCmd
}
