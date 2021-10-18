/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/backup"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/bootstrap"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/instance"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/pgbouncer"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/walarchive"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/walrestore"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/versions"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
	managerFlags := &manager.Flags{}

	cmd := &cobra.Command{
		Use:          "manager [cmd]",
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			managerFlags.ConfigureLogging()
		},
	}

	managerFlags.AddFlags(cmd.PersistentFlags())

	cmd.AddCommand(backup.NewCmd())
	cmd.AddCommand(bootstrap.NewCmd())
	cmd.AddCommand(controller.NewCmd())
	cmd.AddCommand(instance.NewCmd())
	cmd.AddCommand(walarchive.NewCmd())
	cmd.AddCommand(walrestore.NewCmd())
	cmd.AddCommand(versions.NewCmd())
	cmd.AddCommand(pgbouncer.NewCmd())

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
