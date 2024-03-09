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

package drop

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/logical"
)

// NewCmd initializes the subscription create command
func NewCmd() *cobra.Command {
	var subscriptionName string
	var dbName string
	var dryRun bool

	subscriptionDropCmd := &cobra.Command{
		Use:   "drop cluster_name",
		Args:  cobra.ExactArgs(1),
		Short: "drop a logical replication subscription",
		RunE: func(cmd *cobra.Command, args []string) error {
			subscriptionName := strings.TrimSpace(subscriptionName)
			clusterName := args[0]

			if len(subscriptionName) == 0 {
				return fmt.Errorf("subscription is a required option")
			}
			if len(dbName) == 0 {
				return fmt.Errorf("dbname is a required option")
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
		"The name of the subscription to be dropped",
	)
	subscriptionDropCmd.Flags().StringVar(
		&dbName,
		"dbname",
		"",
		"The database in which the command should drop the subscription",
	)
	subscriptionDropCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"If specified, the subscription is not deleted",
	)

	return subscriptionDropCmd
}
