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

// Package pgbasebackup implement the pgbasebackup bootstrap method
package pgbasebackup

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// CloneInfo is the structure containing all the information needed
// to clone an existing server
type CloneInfo struct {
	info   *postgres.InitInfo
	client ctrl.Client
}

// NewCmd creates the "pgbasebackup" subcommand
func NewCmd() *cobra.Command {
	var appDBName string
	var appUser string
	var clusterName string
	var namespace string
	var pgData string

	cmd := &cobra.Command{
		Use: "pgbasebackup",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := management.NewControllerRuntimeClient()
			if err != nil {
				return err
			}

			env := CloneInfo{
				info: &postgres.InitInfo{
					ApplicationDatabase: appDBName,
					ApplicationUser:     appUser,
					ClusterName:         clusterName,
					Namespace:           namespace,
					PgData:              pgData,
				},
				client: client,
			}

			ctx := context.Background()

			if err = env.bootstrapUsingPgbasebackup(ctx); err != nil {
				log.Error(err, "Unable to boostrap cluster")
			}
			return err
		},
	}
	// give empty default value of app-db-name and app-user, so application database configuration
	// could be ignored
	cmd.Flags().StringVar(&appDBName, "app-db-name", "", "The name of the application containing"+
		" the database")
	cmd.Flags().StringVar(&appUser, "app-user", "", "The name of the application user")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")

	return cmd
}

// bootstrapUsingPgbasebackup creates a new data dir from the configuration
func (env *CloneInfo) bootstrapUsingPgbasebackup(ctx context.Context) error {
	var cluster apiv1.Cluster
	err := env.client.Get(ctx, ctrl.ObjectKey{Namespace: env.info.Namespace, Name: env.info.ClusterName}, &cluster)
	if err != nil {
		return err
	}

	server, ok := cluster.ExternalCluster(cluster.Spec.Bootstrap.PgBaseBackup.Source)
	if !ok {
		return fmt.Errorf("missing external cluster")
	}

	connectionString, pgpass, err := external.ConfigureConnectionToServer(
		ctx, env.client, env.info.Namespace, &server)
	if err != nil {
		return err
	}

	// Unfortunately lib/pq doesn't support the passfile
	// connection option so we must rely on an environment
	// variable.
	if pgpass != "" {
		if err = os.Setenv("PGPASSFILE", pgpass); err != nil {
			return err
		}
	}
	err = postgres.ClonePgData(connectionString, env.info.PgData)
	if err != nil {
		return err
	}

	if cluster.IsReplica() {
		_, err = postgres.UpdateReplicaConfigurationForPrimary(env.info.PgData, connectionString)
		return err
	}

	return env.configureInstanceAsNewPrimary(&cluster)
}

// configureInstanceAsNewPrimary sets up this instance as a new primary server, using
// the configuration created by the user and setting up the global objects as needed
func (env *CloneInfo) configureInstanceAsNewPrimary(cluster *apiv1.Cluster) error {
	if err := env.info.WriteInitialPostgresqlConf(cluster); err != nil {
		return err
	}

	if err := env.info.WriteRestoreHbaConf(); err != nil {
		return err
	}

	// We are passing an empty environment variable since the
	// cluster has just been bootstrap via pg_basebackup and at
	// this moment we only use streaming replication to download
	// the WALs.
	//
	// In the future, when we will support recovering WALs in the
	// designated primary from an object store, we'll need to use
	// the environment variables of the recovery object store.
	return env.info.ConfigureInstanceAfterRestore(nil)
}
