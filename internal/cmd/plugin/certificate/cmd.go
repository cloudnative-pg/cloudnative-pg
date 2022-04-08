/*
Copyright 2019-2022 The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package certificate

import (
	"context"

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
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			secretName := args[0]

			user, _ := cmd.Flags().GetString("cnp-user")
			cluster, _ := cmd.Flags().GetString("cnp-cluster")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			output, _ := cmd.Flags().GetString("output")

			params := Params{
				Name:        secretName,
				Namespace:   plugin.Namespace,
				User:        user,
				ClusterName: cluster,
			}

			return Generate(ctx, params, dryRun, plugin.OutputFormat(output))
		},
	}

	certificateCmd.Flags().String(
		"cnp-user", "", "The name of the PostgreSQL user")
	_ = certificateCmd.MarkFlagRequired("cnp-user")
	certificateCmd.Flags().String(
		"cnp-cluster", "", "The name of the PostgreSQL cluster")
	_ = certificateCmd.MarkFlagRequired("cnp-cluster")
	certificateCmd.Flags().StringP(
		"output", "o", "", "Output format. One of json|yaml")
	certificateCmd.Flags().Bool(
		"dry-run", false, "If specified, the secret is not created")

	return certificateCmd
}
