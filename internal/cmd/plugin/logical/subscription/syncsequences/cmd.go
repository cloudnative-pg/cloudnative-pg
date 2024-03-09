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

package syncsequences

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/logical"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
)

// NewCmd initializes the subscription create command
func NewCmd() *cobra.Command {
	var externalClusterName string
	var dbName string
	var dryRun bool

	syncSequencesCmd := &cobra.Command{
		Use:   "sync-sequences cluster_name",
		Short: "synchronize the sequences from the source database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			externalClusterName := strings.TrimSpace(externalClusterName)
			dbName := strings.TrimSpace(dbName)

			if len(externalClusterName) == 0 {
				return fmt.Errorf("the external cluster name is required")
			}

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
				return fmt.Errorf("cluster %s not found in namespace %s", clusterName, plugin.Namespace)
			}

			if len(dbName) == 0 {
				dbName = cluster.GetApplicationDatabaseName()
			}
			if len(dbName) == 0 {
				return fmt.Errorf(
					"the name of the database was not specified and there is no available application database")
			}

			externalCluster, ok := cluster.ExternalCluster(externalClusterName)
			if !ok {
				return fmt.Errorf("external cluster not existent in the cluster definition")
			}

			// Force the dbname parameter in the external cluster params.
			// This is needed since the user may not have specified it, or specified a different db
			// than the one where we should create the subscription
			externalCluster.ConnectionParameters["dbname"] = dbName
			connectionString := external.GetServerConnectionString(&externalCluster)

			sourceStatus, err := GetSequenceStatus(cmd.Context(), clusterName, connectionString)
			if err != nil {
				return fmt.Errorf("while getting sequences status from the source database: %w", err)
			}

			destinationStatus, err := GetSequenceStatus(cmd.Context(), clusterName, dbName)
			if err != nil {
				return fmt.Errorf("while getting sequences status from the destination database: %w", err)
			}

			script := CreateSyncScript(sourceStatus, destinationStatus)
			fmt.Println(script)
			if dryRun {
				return nil
			}

			return logical.RunSQL(cmd.Context(), clusterName, dbName, script)
		},
	}

	syncSequencesCmd.Flags().StringVar(
		&externalClusterName,
		"external-cluster",
		"",
		"The external cluster name",
	)
	syncSequencesCmd.Flags().StringVar(
		&dbName,
		"dbname",
		"",
		"The name of the application where to refresh the sequences. Defaults to the application database if available",
	)
	syncSequencesCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"If specified, the subscription is not created",
	)

	return syncSequencesCmd
}
