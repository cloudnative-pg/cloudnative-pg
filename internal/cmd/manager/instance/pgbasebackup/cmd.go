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

// Package pgbasebackup implement the pgbasebackup bootstrap method
package pgbasebackup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system"
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
	var pgData string
	var pgWal string

	cmd := &cobra.Command{
		Use: "pgbasebackup",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return management.WaitForGetCluster(cmd.Context(), ctrl.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			contextLogger := log.FromContext(ctx)

			client, err := management.NewControllerRuntimeClient()
			if err != nil {
				return err
			}

			env := CloneInfo{
				info: &postgres.InitInfo{
					ClusterName: clusterName,
					Namespace:   namespace,
					PgData:      pgData,
					PgWal:       pgWal,
				},
				client: client,
			}

			if err = env.bootstrapUsingPgbasebackup(ctx); err != nil {
				contextLogger.Error(err, "Unable to bootstrap cluster")
			}
			return err
		},
		PostRunE: func(cmd *cobra.Command, _ []string) error {
			if err := istio.TryInvokeQuitEndpoint(cmd.Context()); err != nil {
				return err
			}

			return linkerd.TryInvokeShutdownEndpoint(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	cmd.Flags().StringVar(&pgWal, "pg-wal", "", "the PGWAL to be created")

	return cmd
}

// bootstrapUsingPgbasebackup creates a new data dir from the configuration
func (env *CloneInfo) bootstrapUsingPgbasebackup(ctx context.Context) error {
	var cluster apiv1.Cluster
	err := env.client.Get(ctx, ctrl.ObjectKey{Namespace: env.info.Namespace, Name: env.info.ClusterName}, &cluster)
	if err != nil {
		return err
	}

	coredumpFilter := cluster.GetCoredumpFilter()
	if err := system.SetCoredumpFilter(coredumpFilter); err != nil {
		return err
	}

	if cluster.ShouldPgBaseBackupCreateApplicationDatabase() {
		env.info.ApplicationUser = cluster.GetApplicationDatabaseOwner()
		env.info.ApplicationDatabase = cluster.GetApplicationDatabaseName()
	}

	server, ok := cluster.ExternalCluster(cluster.Spec.Bootstrap.PgBaseBackup.Source)
	if !ok {
		return fmt.Errorf("missing external cluster")
	}

	connectionString, err := external.ConfigureConnectionToServer(
		ctx, env.client, env.info.Namespace, &server)
	if err != nil {
		return err
	}

	// We explicitly disable wal_sender_timeout for join-related pg_basebackup executions.
	// A short timeout could not be enough in case the instance is slow to send data,
	// like when the I/O is overloaded.
	connectionString += " options='-c wal_sender_timeout=0s'"

	if err := postgres.ClonePgData(ctx, connectionString, env.info.PgData, env.info.PgWal); err != nil {
		return fmt.Errorf("while cloning pgdata: %w", err)
	}

	filePath := filepath.Join(env.info.PgData, constants.CheckEmptyWalArchiveFile)
	// We create the check empty wal archive file to tell that we should check if the
	// destination path it is empty
	if err := fileutils.CreateEmptyFile(filePath); err != nil {
		return fmt.Errorf("could not create %v file: %w", filePath, err)
	}

	if cluster.IsReplica() {
		// TODO: Using a replication slot on replica cluster is not supported (yet?)
		_, err = postgres.UpdateReplicaConfiguration(env.info.PgData, connectionString, "")
		return err
	}

	return env.configureInstanceAsNewPrimary(ctx, &cluster)
}

// configureInstanceAsNewPrimary sets up this instance as a new primary server, using
// the configuration created by the user and setting up the global objects as needed
func (env *CloneInfo) configureInstanceAsNewPrimary(ctx context.Context, cluster *apiv1.Cluster) error {
	if err := env.info.WriteInitialPostgresqlConf(ctx, cluster); err != nil {
		return err
	}

	if err := env.info.WriteRestoreHbaConf(ctx); err != nil {
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
	return env.info.ConfigureInstanceAfterRestore(ctx, cluster, nil)
}
