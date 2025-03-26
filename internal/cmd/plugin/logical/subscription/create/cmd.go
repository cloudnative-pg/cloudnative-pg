/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package create

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/logical"
)

// NewCmd initializes the subscription create command
func NewCmd() *cobra.Command {
	var externalClusterName string
	var publicationName string
	var subscriptionName string
	var publicationDBName string
	var subscriptionDBName string
	var parameters string
	var dryRun bool

	subscriptionCreateCmd := &cobra.Command{
		Use:   "create CLUSTER",
		Short: "create a logical replication subscription",
		Args:  plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			externalClusterName := strings.TrimSpace(externalClusterName)
			publicationName := strings.TrimSpace(publicationName)
			subscriptionName := strings.TrimSpace(subscriptionName)
			publicationDBName := strings.TrimSpace(publicationDBName)
			subscriptionDBName := strings.TrimSpace(subscriptionDBName)

			if len(subscriptionDBName) == 0 {
				var err error
				subscriptionDBName, err = logical.GetApplicationDatabaseName(cmd.Context(), clusterName)
				if err != nil {
					return err
				}
			}
			if len(subscriptionDBName) == 0 {
				return fmt.Errorf(
					"the name of the database was not specified and there is no available application database")
			}

			connectionString, err := logical.GetConnectionString(
				cmd.Context(),
				clusterName,
				externalClusterName,
				publicationDBName,
			)
			if err != nil {
				return err
			}

			createCmd := SubscriptionCmdBuilder{
				SubscriptionName: subscriptionName,
				PublicationName:  publicationName,
				ConnectionString: connectionString,
				Parameters:       parameters,
			}
			sqlCommand := createCmd.ToSQL()

			fmt.Println(sqlCommand)
			if dryRun {
				return nil
			}

			return logical.RunSQL(cmd.Context(), clusterName, subscriptionDBName, sqlCommand)
		},
	}

	subscriptionCreateCmd.Flags().StringVar(
		&externalClusterName,
		"external-cluster",
		"",
		"The external cluster name (required)",
	)
	_ = subscriptionCreateCmd.MarkFlagRequired("external-cluster")

	subscriptionCreateCmd.Flags().StringVar(
		&publicationName,
		"publication",
		"",
		"The name of the publication to subscribe to (required)",
	)
	_ = subscriptionCreateCmd.MarkFlagRequired("publication")

	subscriptionCreateCmd.Flags().StringVar(
		&subscriptionName,
		"subscription",
		"",
		"The name of the subscription to create (required)",
	)
	_ = subscriptionCreateCmd.MarkFlagRequired("subscription")

	subscriptionCreateCmd.Flags().StringVar(
		&subscriptionDBName,
		"dbname",
		"",
		"The name of the database where to create the subscription. Defaults to the application database if available",
	)
	subscriptionCreateCmd.Flags().StringVar(
		&publicationDBName,
		"publication-dbname",
		"",
		"The name of the database containing the publication on the external cluster. "+
			"Defaults to the one in the external cluster definition",
	)
	subscriptionCreateCmd.Flags().StringVar(
		&parameters,
		"parameters",
		"",
		"The subscription parameters. IMPORTANT: this command won't perform any validation. "+
			"Users are responsible for passing them correctly",
	)
	subscriptionCreateCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"If specified, the subscription commands are shown but not executed",
	)

	return subscriptionCreateCmd
}
