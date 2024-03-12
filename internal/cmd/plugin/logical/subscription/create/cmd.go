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
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/logical"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
)

// NewCmd initializes the subscription create command
func NewCmd() *cobra.Command {
	var externalClusterName string
	var publicationName string
	var subscriptionName string
	var dbName string
	var parameters string
	var dryRun bool

	subscriptionCreateCmd := &cobra.Command{
		Use:   "create cluster_name",
		Short: "create a logical replication subscription",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			externalClusterName := strings.TrimSpace(externalClusterName)
			publicationName := strings.TrimSpace(publicationName)
			subscriptionName := strings.TrimSpace(subscriptionName)
			dbName := strings.TrimSpace(dbName)

			if len(externalClusterName) == 0 {
				return fmt.Errorf("the external cluster name is required")
			}
			if len(publicationName) == 0 {
				return fmt.Errorf("the name of the publication is required")
			}
			if len(subscriptionName) == 0 {
				return fmt.Errorf("the name of the subscription is required")
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

			return logical.RunSQL(cmd.Context(), clusterName, dbName, sqlCommand)
		},
	}

	subscriptionCreateCmd.Flags().StringVar(
		&externalClusterName,
		"external-cluster",
		"",
		"The external cluster name",
	)
	subscriptionCreateCmd.Flags().StringVar(
		&publicationName,
		"publication",
		"",
		"The name of the publication to subscribe to",
	)
	subscriptionCreateCmd.Flags().StringVar(
		&subscriptionName,
		"subscription",
		"",
		"The name of the subscription to create",
	)
	subscriptionCreateCmd.Flags().StringVar(
		&dbName,
		"dbname",
		"",
		"The name of the application where to create the subscription. Defaults to the application database if available",
	)
	subscriptionCreateCmd.Flags().StringVar(
		&parameters,
		"parameters",
		"",
		"The subscription parameters. IMPORTANT: this command won't perform any validation. "+
			"Users are responsible to pass them correctly",
	)
	subscriptionCreateCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"If specified, the subscription is not created",
	)

	return subscriptionCreateCmd
}
