/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package fence

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var (
	fenceOnCmd = &cobra.Command{
		Use:   "on [cluster] [node]",
		Short: `Fence an instance named [cluster]-[node] or [node]`,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			node := args[1]
			if _, err := strconv.Atoi(args[1]); err == nil {
				node = fmt.Sprintf("%s-%s", clusterName, node)
			}

			return fencingOn(cmd.Context(), clusterName, node)
		},
	}

	fenceOffCmd = &cobra.Command{
		Use:   "off [cluster] [node]",
		Short: `Remove fence for an instance named [cluster]-[node] or [node]`,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			node := args[1]
			if _, err := strconv.Atoi(args[1]); err == nil {
				node = fmt.Sprintf("%s-%s", clusterName, node)
			}
			return fencingOff(cmd.Context(), clusterName, node)
		},
	}
)

// NewCmd creates the new "fencing" command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fencing",
		Short: `Fencing related commands`,
	}
	cmd.AddCommand(fenceOnCmd)
	cmd.AddCommand(fenceOffCmd)

	return cmd
}
