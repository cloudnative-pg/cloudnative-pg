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

// Package restore implements the "instance restore" subcommand of the operator
package restore

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// NewCmd creates the "restore" subcommand
func NewCmd() *cobra.Command {
	var appDBName string
	var appUser string
	var clusterName string
	var namespace string
	var pgData string

	cmd := &cobra.Command{
		Use:           "restore [flags]",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			info := postgres.InitInfo{
				ApplicationDatabase: appDBName,
				ApplicationUser:     appUser,
				ClusterName:         clusterName,
				Namespace:           namespace,
				PgData:              pgData,
			}

			return restoreSubCommand(ctx, info)
		},
	}
	// give empty default value of app-db-name and app-user, so application database configuration
	// could be ignored
	cmd.Flags().StringVar(&appDBName, "app-db-name", "",
		"The name of the application containing the database")
	cmd.Flags().StringVar(&appUser, "app-user", "",
		"The name of the application user")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and the Pod in k8s")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")

	return cmd
}

func restoreSubCommand(ctx context.Context, info postgres.InitInfo) error {
	err := info.VerifyPGData()
	if err != nil {
		return err
	}

	err = info.Restore(ctx)
	if err != nil {
		log.Error(err, "Error while restoring a backup")
		return err
	}

	return nil
}
