/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package promote

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// NewCmd create the new "promote" subcommand
func NewCmd() *cobra.Command {
	promoteCmd := &cobra.Command{
		Use:   "promote [cluster] [node]",
		Short: "Promote the pod named [cluster]-[node] or [node] to primary",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clusterName := args[0]
			node := args[1]
			if _, err := strconv.Atoi(args[1]); err == nil {
				node = fmt.Sprintf("%s-%s", clusterName, node)
			}
			return Promote(ctx, clusterName, node)
		},
	}

	return promoteCmd
}
