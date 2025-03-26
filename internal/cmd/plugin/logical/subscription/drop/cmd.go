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

package drop

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/logical"
)

// NewCmd initializes the subscription create command
func NewCmd() *cobra.Command {
	var subscriptionName string
	var dbName string
	var dryRun bool

	subscriptionDropCmd := &cobra.Command{
		Use:  "drop CLUSTER",
		Args: plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		Short: "drop a logical replication subscription",
		RunE: func(cmd *cobra.Command, args []string) error {
			subscriptionName := strings.TrimSpace(subscriptionName)
			clusterName := args[0]
			dbName := strings.TrimSpace(dbName)

			if len(dbName) == 0 {
				var err error
				dbName, err = logical.GetApplicationDatabaseName(cmd.Context(), clusterName)
				if err != nil {
					return err
				}
			}
			if len(dbName) == 0 {
				return fmt.Errorf(
					"the name of the database was not specified and there is no available application database")
			}

			sqlCommand := fmt.Sprintf(
				"DROP SUBSCRIPTION %s",
				pgx.Identifier{subscriptionName}.Sanitize(),
			)
			fmt.Println(sqlCommand)
			if dryRun {
				return nil
			}

			return logical.RunSQL(cmd.Context(), clusterName, dbName, sqlCommand)
		},
	}

	subscriptionDropCmd.Flags().StringVar(
		&subscriptionName,
		"subscription",
		"",
		"The name of the subscription to be dropped (required)",
	)
	_ = subscriptionDropCmd.MarkFlagRequired("subscription")

	subscriptionDropCmd.Flags().StringVar(
		&dbName,
		"dbname",
		"",
		"The database in which the command should drop the subscription (required)",
	)

	subscriptionDropCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"If specified, the subscription deletion commands are shown but not executed",
	)

	return subscriptionDropCmd
}
