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

// NewCmd initializes the publication create command
func NewCmd() *cobra.Command {
	var publicationName string
	var dbName string
	var externalClusterName string
	var dryRun bool

	publicationDropCmd := &cobra.Command{
		Use:  "drop CLUSTER",
		Args: plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		Short: "drop a logical replication publication",
		RunE: func(cmd *cobra.Command, args []string) error {
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

			sqlCommand := fmt.Sprintf(
				"DROP PUBLICATION %s",
				pgx.Identifier{publicationName}.Sanitize(),
			)
			fmt.Println(sqlCommand)
			if dryRun {
				return nil
			}

			target := dbName
			if len(externalClusterName) > 0 {
				var err error
				target, err = logical.GetConnectionString(cmd.Context(), clusterName, externalClusterName, dbName)
				if err != nil {
					return err
				}
			}

			return logical.RunSQL(cmd.Context(), clusterName, target, sqlCommand)
		},
	}

	publicationDropCmd.Flags().StringVar(
		&publicationName,
		"publication",
		"",
		"The name of the publication to be dropped (required)",
	)
	_ = publicationDropCmd.MarkFlagRequired("publication")

	publicationDropCmd.Flags().StringVar(
		&dbName,
		"dbname",
		"",
		"The database in which the command should drop the publication",
	)
	publicationDropCmd.Flags().StringVar(
		&externalClusterName,
		"external-cluster",
		"",
		"The cluster where to drop the publication. Defaults to the local cluster",
	)
	publicationDropCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"If specified, the publication deletion commands are shown but not executed",
	)

	return publicationDropCmd
}
