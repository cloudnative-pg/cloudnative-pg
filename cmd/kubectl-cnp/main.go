/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package main

import (
	"os"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/certificate"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/promote"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/restart"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/status"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/versions"
)

func main() {
	configFlags := genericclioptions.NewConfigFlags(true)

	rootCmd := &cobra.Command{
		Use:          "kubectl-cnp",
		Short:        "An interface to manage your Cloud Native PostgreSQL clusters",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return plugin.CreateKubernetesClient(configFlags)
		},
	}

	configFlags.AddFlags(rootCmd.PersistentFlags())

	rootCmd.AddCommand(status.NewCmd())
	rootCmd.AddCommand(promote.NewCmd())
	rootCmd.AddCommand(certificate.NewCmd())
	rootCmd.AddCommand(restart.NewCmd())
	rootCmd.AddCommand(versions.NewCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
