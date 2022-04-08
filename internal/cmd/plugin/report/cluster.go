/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package report

import (
	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
)

func clusterCmd() *cobra.Command {
	var (
		file, output string
		includeLogs  bool
	)

	cmd := &cobra.Command{
		Use:   "cluster [clusterName]",
		Short: "Report cluster resources, pods, events, logs (opt-in)",
		Long:  "Collects combined information on the cluster in a Zip file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			return Cluster(cmd.Context(), clusterName, plugin.Namespace,
				plugin.OutputFormat(output), file, includeLogs)
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", reportName("cluster")+".zip",
		"Output file (timestamp computed at each run)")
	cmd.Flags().StringVarP(&output, "output", "o", "yaml",
		"Output format (yaml or json)")
	cmd.Flags().BoolVarP(&includeLogs, "logs", "l", false, "include logs")

	return cmd
}
