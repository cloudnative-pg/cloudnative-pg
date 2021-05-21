/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package instance implements the "instance" subcommand of the operator
package instance

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/instance/initdb"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/instance/join"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/instance/pgbasebackup"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/instance/restore"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/instance/run"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/instance/status"
)

// NewCmd creates the "instance" command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Instance management subfeatures",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("missing subcommand")
		},
	}

	cmd.AddCommand(initdb.NewCmd())
	cmd.AddCommand(join.NewCmd())
	cmd.AddCommand(run.NewCmd())
	cmd.AddCommand(status.NewCmd())
	cmd.AddCommand(pgbasebackup.NewCmd())
	cmd.AddCommand(restore.NewCmd())

	return cmd
}
