/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package pgbouncer implements the "pgbouncer" subcommand of the operator
package pgbouncer

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/pgbouncer/run"
)

// NewCmd creates the "instance" command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "pgbouncer",
		Short:         "pgbouncer management subfeatures",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("missing subcommand")
		},
	}

	cmd.AddCommand(run.NewCmd())

	return cmd
}
