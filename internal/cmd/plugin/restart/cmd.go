/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package restart

import (
	"context"

	"github.com/spf13/cobra"
)

// NewCmd creates the new "reset" command
func NewCmd() *cobra.Command {
	restartCmd := &cobra.Command{
		Use:   "restart [clusterName]",
		Short: `Restart the cluster`,
		Long:  `The cluster will be restarted, rolling out new configurations if present.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clusterName := args[0]
			return Restart(ctx, clusterName)
		},
	}

	return restartCmd
}
