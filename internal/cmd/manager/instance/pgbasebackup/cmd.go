/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package pgbasebackup implement the pgbasebackup bootstrap method
package pgbasebackup

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/external"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// CloneInfo is the structure containing all the information needed
// to clone an existing server
type CloneInfo struct {
	info   *postgres.InitInfo
	client ctrl.Client
}

// NewCmd creates the "pgbasebackup" subcommand
func NewCmd() *cobra.Command {
	var clusterName string
	var namespace string
	var pwFile string
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
					PgData:       pgData,
					PasswordFile: pwFile,
					ClusterName:  clusterName,
					Namespace:    namespace,
				},
				client: client,
			}

			ctx := context.Background()

			if err = env.bootstrapUsingPgbasebackup(ctx); err != nil {
				log.Log.Error(err, "Unable to boostrap cluster")
			}
			return err
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().StringVar(&pwFile, "pw-file", "",
		"The file containing the PostgreSQL superuser password to use during the init phase")
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

	server, ok := cluster.ExternalServer(cluster.Spec.Bootstrap.PgBaseBackup.Source)
	if !ok {
		return fmt.Errorf("missing external server")
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

	return env.configureInstanceAsNewPrimary(ctx)
}

// configureInstanceAsNewPrimary sets up this instance as a new primary server, using
// the configuration created by the user and setting up the global object as needed
func (env *CloneInfo) configureInstanceAsNewPrimary(ctx context.Context) error {
	if err := env.info.WriteInitialPostgresqlConf(ctx, env.client); err != nil {
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
