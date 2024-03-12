/*
Copyright The CloudNativePG Contributors

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

package create

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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
	var dryRun bool

	publicationCreateCmd := &cobra.Command{
		Use:   "create cluster_name",
		Args:  cobra.ExactArgs(1),
		Short: "create a logical replication publication",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbName := strings.TrimSpace(dbName)
			publicationName := strings.TrimSpace(publicationName)
			clusterName := args[0]

			if len(dbName) == 0 {
				return fmt.Errorf("dbname is a required option")
			}

			if len(publicationName) == 0 {
				return fmt.Errorf("publication is a required option")
			}

			if allTables && (len(schemaNames) > 0 || len(tableExprs) > 0) {
				return fmt.Errorf("cannot publicate all tables and selected schema/tables at the same time")
			}
			if !allTables && len(schemaNames) == 0 && len(tableExprs) == 0 {
				return fmt.Errorf("no publication target selected")
			}

			sqlCommandBuilder := PublicationCmdBuilder{
				PublicationName: publicationName,
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
				for _, tableExpression := range tableExprs {
					targets.PublicationObjects = append(
						targets.PublicationObjects,
						&PublicationObjectTableExpression{
							TableExpression: tableExpression,
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
		&publicationName,
		"publication",
		"",
		"The name of the publication to be created",
	)

	publicationCreateCmd.Flags().BoolVar(
		&allTables,
		"all-tables",
		false,
		"Create the publication for all the tables in the database or in the schema",
	)
	publicationCreateCmd.Flags().StringVar(
		&dbName,
		"dbname",
		"",
		"The database in which the command should create the publication" +
		" (default: `app`)",
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
	publicationCreateCmd.Flags().StringVar(
		&externalClusterName,
		"external-cluster",
		"",
		"The cluster where to create the publication. Defaults to the local cluster",
	)
	publicationCreateCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"If specified, the publication is not created",
	)

	return publicationCreateCmd
}
