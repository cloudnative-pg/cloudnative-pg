/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package report

import (
	"github.com/spf13/cobra"
)

// NewCmd creates the new "report" command
func NewCmd() *cobra.Command {
	reportCmd := &cobra.Command{
		Use:   "report operator/cluster",
		Short: "Report on the operator",
	}

	reportCmd.AddCommand(operatorCmd())

	return reportCmd
}
