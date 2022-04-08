/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package report

import (
	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
)

func operatorCmd() *cobra.Command {
	var (
		file, output  string
		stopRedaction bool
		includeLogs   bool
	)
	cmd := &cobra.Command{
		Use:   "operator -f <filename.zip>",
		Short: "Report operator deployment, pod, events, logs (opt-in)",
		Long:  "Collects combined information on the operator in a Zip file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return Operator(cmd.Context(), plugin.OutputFormat(output),
				file, stopRedaction, includeLogs)
		},
	}

	cmd.AddCommand()

	cmd.Flags().StringVarP(&file, "file", "f", reportName("operator")+".zip",
		"Output file")
	cmd.Flags().StringVarP(&output, "output", "o", "yaml",
		"Output format. One of yaml|json")
	cmd.Flags().BoolVarP(&stopRedaction, "stopRedaction", "S", false,
		"Don't redact secrets")
	cmd.Flags().BoolVarP(&includeLogs, "logs", "l", false, "include logs")

	return cmd
}
