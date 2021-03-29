/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/cnp"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/cnp/certificate"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/cnp/promote"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/cnp/status"
)

var (
	configFlags *genericclioptions.ConfigFlags

	rootCmd = &cobra.Command{
		Use:   "kubectl-cnp",
		Short: "An interface to manage your Cloud Native PostgreSQL clusters",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return cnp.CreateKubernetesClient(configFlags)
		},
	}

	promoteCmd = &cobra.Command{
		Use:   "promote [cluster] [server]",
		Short: "Promote a certain server as a primary",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := context.Background()
			clusterName := args[0]
			serverName := args[1]

			promote.Promote(ctx, clusterName, serverName)
		},
	}

	statusCmd = &cobra.Command{
		Use:   "status [cluster]",
		Short: "Get the status of a PostgreSQL cluster",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := context.Background()
			clusterName := args[0]

			verbose, _ := cmd.Flags().GetBool("verbose")
			output, _ := cmd.Flags().GetString("output")

			err := status.Status(ctx, clusterName, verbose, cnp.OutputFormat(output))
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}

	certificateCmd = &cobra.Command{
		Use:   "certificate [secretName]",
		Short: `Create a client certificate to connect to PostgreSQL using TLS and Certificate authentication`,
		Long: `This command create a new Kubernetes secret containing the crypto-material
needed to configure TLS with Certificate authentication access for an application to
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

			params := certificate.Params{
				Name:        secretName,
				Namespace:   cnp.Namespace,
				User:        user,
				ClusterName: cluster,
			}

			err := certificate.Generate(ctx, params, dryRun, cnp.OutputFormat(output))
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
)

func main() {
	configFlags = genericclioptions.NewConfigFlags(true)
	configFlags.AddFlags(rootCmd.PersistentFlags())

	statusCmd.Flags().BoolP(
		"verbose", "v", false, "Print also the PostgreSQL configuration and HBA rules")
	statusCmd.Flags().StringP(
		"output", "o", "text", "Output format. One of text|json")

	certificateCmd.Flags().String(
		"cnp-user", "", "The name of the PostgreSQL user")
	certificateCmd.Flags().String(
		"cnp-cluster", "", "The name of the PostgreSQL cluster")
	certificateCmd.Flags().StringP(
		"output", "o", "", "Output format. One of json|yaml")
	certificateCmd.Flags().Bool(
		"dry-run", false, "If specified the secret is not created")

	rootCmd.AddCommand(promoteCmd, statusCmd, certificateCmd)

	_ = rootCmd.Execute()
}
