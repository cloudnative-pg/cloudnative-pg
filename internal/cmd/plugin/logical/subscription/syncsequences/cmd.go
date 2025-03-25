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

package syncsequences

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/logical"
)

// NewCmd initializes the subscription create command
func NewCmd() *cobra.Command {
	var subscriptionName string
	var dbName string
	var dryRun bool
	var offset int

	syncSequencesCmd := &cobra.Command{
		Use:   "sync-sequences CLUSTER",
		Short: "synchronize the sequences from the source database",
		Args:  plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			subscriptionName := strings.TrimSpace(subscriptionName)
			dbName := strings.TrimSpace(dbName)

			var cluster apiv1.Cluster
			err := plugin.Client.Get(
				cmd.Context(),
				client.ObjectKey{
					Namespace: plugin.Namespace,
					Name:      clusterName,
				},
				&cluster,
			)
			if err != nil {
				return fmt.Errorf("cluster %s not found in namespace %s: %w",
					clusterName, plugin.Namespace, err)
			}

			if len(dbName) == 0 {
				dbName = cluster.GetApplicationDatabaseName()
			}
			if len(dbName) == 0 {
				return fmt.Errorf(
					"the name of the database was not specified and there is no available application database")
			}

			connectionString, err := logical.GetSubscriptionConnInfo(cmd.Context(), clusterName, dbName, subscriptionName)
			if err != nil {
				return fmt.Errorf(
					"while getting connection string from subscription: %w", err)
			}
			if len(connectionString) == 0 {
				return fmt.Errorf(
					"subscription %s was not found", subscriptionName)
			}

			sourceStatus, err := GetSequenceStatus(cmd.Context(), clusterName, connectionString)
			if err != nil {
				return fmt.Errorf("while getting sequences status from the source database: %w", err)
			}

			destinationStatus, err := GetSequenceStatus(cmd.Context(), clusterName, dbName)
			if err != nil {
				return fmt.Errorf("while getting sequences status from the destination database: %w", err)
			}

			script := CreateSyncScript(sourceStatus, destinationStatus, offset)
			fmt.Println(script)
			if dryRun {
				return nil
			}

			return logical.RunSQL(cmd.Context(), clusterName, dbName, script)
		},
	}

	syncSequencesCmd.Flags().StringVar(
		&subscriptionName,
		"subscription",
		"",
		"The name of the subscription on which to refresh sequences (required)",
	)
	_ = syncSequencesCmd.MarkFlagRequired("subscription")

	syncSequencesCmd.Flags().StringVar(
		&dbName,
		"dbname",
		"",
		"The name of the database where the subscription is present and sequences need to be updated. "+
			"Defaults to the application database, if available",
	)
	syncSequencesCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"If specified, the subscription is not created",
	)
	syncSequencesCmd.Flags().IntVar(
		&offset,
		"offset",
		0,
		"The number to add to every sequence number before being updated",
	)

	return syncSequencesCmd
}
