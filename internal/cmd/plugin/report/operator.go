/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package report

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/spf13/cobra"
)

func operatorCmd() *cobra.Command {
	var (
		file, output  string
		stopRedaction bool
	)
	cmd := &cobra.Command{
		Use:   "operator -f <filename.zip>",
		Short: "Report operator deployment, pod, events",
		Long:  "Collects combined information on the operator in a Zip file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return Operator(cmd.Context(), plugin.OutputFormat(output),
				file, stopRedaction)
		},
	}

	cmd.AddCommand()

	cmd.Flags().StringVarP(&file, "file", "f", "report.zip",
		"Output file")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().StringVarP(&output, "output", "o", "yaml",
		"Output format. One of yaml|json")
	cmd.Flags().BoolVarP(&stopRedaction, "stopRedaction", "S", false,
		"Don't redact secrets")

	return cmd
}
