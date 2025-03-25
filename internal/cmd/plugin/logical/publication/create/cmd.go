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

// NewCmd initializes the publication create command
func NewCmd() *cobra.Command {
	var dbName string
	var allTables bool
	var schemaNames []string
	var tableExprs []string
	var publicationName string
	var externalClusterName string
	var publicationParameters string
	var dryRun bool

	publicationCreateCmd := &cobra.Command{
		Use:  "create CLUSTER",
		Args: plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		Short: "create a logical replication publication",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbName := strings.TrimSpace(dbName)
			publicationName := strings.TrimSpace(publicationName)
			clusterName := args[0]

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

			sqlCommandBuilder := PublicationCmdBuilder{
				PublicationName:       publicationName,
				PublicationParameters: publicationParameters,
			}

			if allTables {
				sqlCommandBuilder.PublicationTarget = &PublicationTargetALLTables{}
			} else {
				targets := &PublicationTargetPublicationObjects{}
				for _, schemaName := range schemaNames {
					targets.PublicationObjects = append(
						targets.PublicationObjects,
						&PublicationObjectSchema{
							SchemaName: schemaName,
						},
					)
				}

				if len(tableExprs) > 0 {
					targets.PublicationObjects = append(
						targets.PublicationObjects,
						&PublicationObjectTableExpression{
							TableExpressions: tableExprs,
						},
					)
				}
				sqlCommandBuilder.PublicationTarget = targets
			}

			target := dbName
			if len(externalClusterName) > 0 {
				var err error
				target, err = logical.GetConnectionString(cmd.Context(), clusterName, externalClusterName, dbName)
				if err != nil {
					return err
				}
			}

			sqlCommand := sqlCommandBuilder.ToSQL()
			fmt.Println(sqlCommand)
			if dryRun {
				return nil
			}

			return logical.RunSQL(cmd.Context(), clusterName, target, sqlCommand)
		},
	}

	publicationCreateCmd.Flags().StringVar(
		&dbName,
		"dbname",
		"",
		"The database in which the command should create the publication "+
			"(defaults to the name of the application database)",
	)

	publicationCreateCmd.Flags().StringVar(
		&publicationName,
		"publication",
		"",
		"The name of the publication to be created (required)",
	)
	_ = publicationCreateCmd.MarkFlagRequired("publication")

	publicationCreateCmd.Flags().BoolVar(
		&allTables,
		"all-tables",
		false,
		"Create the publication for all the tables in the database or in the schema",
	)
	publicationCreateCmd.Flags().StringSliceVar(
		&schemaNames,
		"schema",
		nil,
		"Create the publication for all the tables in the selected schema",
	)
	publicationCreateCmd.Flags().StringSliceVar(
		&tableExprs,
		"table",
		nil,
		"Create the publication for the selected table expression",
	)
	publicationCreateCmd.MarkFlagsOneRequired("all-tables", "schema", "table")
	publicationCreateCmd.MarkFlagsMutuallyExclusive("all-tables", "schema")
	publicationCreateCmd.MarkFlagsMutuallyExclusive("all-tables", "table")

	publicationCreateCmd.Flags().StringVar(
		&externalClusterName,
		"external-cluster",
		"",
		"The cluster in which to create the publication. Defaults to the local cluster",
	)
	publicationCreateCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"If specified, the publication commands are shown but not executed",
	)

	publicationCreateCmd.Flags().StringVar(
		&publicationParameters,
		"parameters",
		"",
		"The publication parameters. IMPORTANT: this command won't perform any validation. "+
			"Users are responsible for passing them correctly",
	)

	return publicationCreateCmd
}
