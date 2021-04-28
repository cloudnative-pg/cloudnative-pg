/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package promote

import (
	"context"

	"github.com/spf13/cobra"
)

// NewCmd create the new "promote" subcommand
func NewCmd() *cobra.Command {
	promoteCmd := &cobra.Command{
		Use:   "promote [cluster] [server]",
		Short: "Promote a certain server as a primary",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := context.Background()
			clusterName := args[0]
			serverName := args[1]

			Promote(ctx, clusterName, serverName)
		},
	}

	return promoteCmd
}
