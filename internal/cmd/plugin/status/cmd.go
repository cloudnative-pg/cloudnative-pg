/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package status

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
)

// NewCmd create the new "status" subcommand
func NewCmd() *cobra.Command {
	statusCmd := &cobra.Command{
		Use:   "status [cluster]",
		Short: "Get the status of a PostgreSQL cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clusterName := args[0]

			verbose, _ := cmd.Flags().GetBool("verbose")
			output, _ := cmd.Flags().GetString("output")

			return Status(ctx, clusterName, verbose, plugin.OutputFormat(output))
		},
	}

	statusCmd.Flags().BoolP(
		"verbose", "v", false, "Print also the PostgreSQL configuration and HBA rules")
	statusCmd.Flags().StringP(
		"output", "o", "text", "Output format. One of text|json")

	return statusCmd
}
