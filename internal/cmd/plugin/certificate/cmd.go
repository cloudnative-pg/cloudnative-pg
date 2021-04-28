/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package certificate

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
)

// NewCmd creates the new "certificate" subcommand
func NewCmd() *cobra.Command {
	certificateCmd := &cobra.Command{
		Use:   "certificate [secretName]",
		Short: `Create a client certificate to connect to PostgreSQL using TLS and Certificate authentication`,
		Long: `This command creates a new Kubernetes secret containing the crypto-material.
This is needed to configure TLS with Certificate authentication access for an application to
connect to the PostgreSQL cluster.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := context.Background()
			secretName := args[0]

			user, _ := cmd.Flags().GetString("cnp-user")
			cluster, _ := cmd.Flags().GetString("cnp-cluster")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			output, _ := cmd.Flags().GetString("output")

			if user == "" {
				fmt.Println("Missing PostgreSQL user name. Hint: is the `--cnp-user` option specified?")
				return
			}

			if cluster == "" {
				fmt.Println("Missing cluster name. Hint: is the `--cnp-cluster` option specified?")
				return
			}

			params := Params{
				Name:        secretName,
				Namespace:   plugin.Namespace,
				User:        user,
				ClusterName: cluster,
			}

			err := Generate(ctx, params, dryRun, plugin.OutputFormat(output))
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}

	certificateCmd.Flags().String(
		"cnp-user", "", "The name of the PostgreSQL user")
	certificateCmd.Flags().String(
		"cnp-cluster", "", "The name of the PostgreSQL cluster")
	certificateCmd.Flags().StringP(
		"output", "o", "", "Output format. One of json|yaml")
	certificateCmd.Flags().Bool(
		"dry-run", false, "If specified, the secret is not created")

	return certificateCmd
}
