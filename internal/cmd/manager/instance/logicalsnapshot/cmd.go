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

package logicalsnapshot

import (
	"context"
	"fmt"
	"os"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"

	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logicalimport"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

// LogicalDump the value needed for the logical dump
type LogicalDump struct {
	ClusterName               string
	Namespace                 string
	PgData                    string
	PodName                   string
	AppDBName                 string
	AppUser                   string
	PostInitSQLStr            string
	PostInitApplicationSQLStr string
	PostInitTemplateSQLStr    string
	ParentNode                string
	InitDBFlagsString         string
}

// NewCmd creates the "logicalsnapshot" subcommand
func NewCmd() *cobra.Command {
	ld := LogicalDump{}

	cmd := &cobra.Command{
		Use: "logicalsnapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := management.NewControllerRuntimeClient()
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			if err = ld.executeLogicalDumpRestore(ctx, client); err != nil {
				log.Error(err, "Unable to boostrap cluster")
			}
			return err
		},
	}

	cmd.Flags().StringVar(&ld.ClusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&ld.Namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().StringVar(&ld.PgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	cmd.Flags().StringVar(&ld.PodName, "pod-name", os.Getenv("POD_NAME"), "The pod name to "+
		"be checked against the cluster state")
	cmd.Flags().StringVar(&ld.AppDBName, "app-db-name", "app",
		"The name of the application containing the database")
	cmd.Flags().StringVar(&ld.AppUser, "app-user", "app",
		"The name of the application user")
	cmd.Flags().StringVar(&ld.PostInitSQLStr, "post-init-sql", "", "The list of SQL queries to be "+
		"executed to configure the new instance")
	cmd.Flags().StringVar(&ld.PostInitApplicationSQLStr, "post-init-application-sql", "", "The list of SQL queries to be "+
		"executed inside application database right after the database is created")
	cmd.Flags().StringVar(&ld.PostInitTemplateSQLStr, "post-init-template-sql", "", "The list of SQL queries to be "+
		"executed inside template1 database to configure the new instance")
	cmd.Flags().StringVar(&ld.ParentNode, "parent-node", "", "The origin node")
	cmd.Flags().StringVar(&ld.InitDBFlagsString, "initdb-flags", "", "The list of flags to be passed "+
		"to initdb while creating the initial database")

	return cmd
}

func (ld *LogicalDump) executeLogicalDumpRestore(ctx context.Context, client ctrl.Client) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("starting logical backup", "data-passed", ld)

	var cluster apiv1.Cluster
	if err := client.Get(ctx, ctrl.ObjectKey{Namespace: ld.Namespace, Name: ld.ClusterName}, &cluster); err != nil {
		return err
	}
	initDBFlags, err := shellquote.Split(ld.InitDBFlagsString)
	if err != nil {
		log.Error(err, "Error while parsing initdb flags")
		return err
	}
	postInitSQL, err := shellquote.Split(ld.PostInitSQLStr)
	if err != nil {
		log.Error(err, "Error while parsing post init SQL queries")
		return err
	}

	postInitApplicationSQL, err := shellquote.Split(ld.PostInitApplicationSQLStr)
	if err != nil {
		log.Error(err, "Error while parsing post init template SQL queries")
		return err
	}

	postInitTemplateSQL, err := shellquote.Split(ld.PostInitTemplateSQLStr)
	if err != nil {
		log.Error(err, "Error while parsing post init template SQL queries")
		return err
	}

	info := postgres.InitInfo{
		ApplicationDatabase:    ld.AppDBName,
		ApplicationUser:        ld.AppUser,
		ClusterName:            ld.ClusterName,
		InitDBOptions:          initDBFlags,
		Namespace:              ld.Namespace,
		ParentNode:             ld.ParentNode,
		PgData:                 ld.PgData,
		PodName:                ld.PodName,
		PostInitSQL:            postInitSQL,
		PostInitApplicationSQL: postInitApplicationSQL,
		PostInitTemplateSQL:    postInitTemplateSQL,
	}

	if err := info.Bootstrap(); err != nil {
		return err
	}

	// TODO: use WithActiveInstance otherwise we have no log
	instance := info.GetInstance()
	if err := instance.Startup(); err != nil {
		return err
	}
	defer func() {
		shutdownError := instance.Shutdown(postgres.DefaultShutdownOptions)
		if shutdownError != nil {
			log.Info("Error while shutting down the instance", "err", shutdownError)
		}
	}()

	destinationPool := instance.ConnectionPool()
	defer destinationPool.ShutdownConnections()

	originPool, err := getConnectionPoolerForExternalCluster(ctx, cluster, client, ld.Namespace)
	if err != nil {
		return err
	}
	defer originPool.ShutdownConnections()

	cloneType := cluster.Spec.Bootstrap.InitDB.Import.Type
	switch cloneType {
	case apiv1.MicroserviceSnapshotType:
		return logicalimport.Microservice(ctx, &cluster, destinationPool, originPool)
	case apiv1.MonolithSnapshotType:
		return logicalimport.Monolith(ctx, &cluster, destinationPool, originPool)
	default:
		return fmt.Errorf("unrecognized clone type %s", cloneType)
	}
}

func getConnectionPoolerForExternalCluster(
	ctx context.Context,
	cluster apiv1.Cluster,
	client ctrl.Client,
	namespaceOfNewCluster string,
) (*pool.ConnectionPool, error) {
	externalCluster, ok := cluster.ExternalCluster(cluster.Spec.Bootstrap.InitDB.Import.Source.ExternalCluster)
	if !ok {
		return nil, fmt.Errorf("missing external cluster")
	}

	tmp := externalCluster.DeepCopy()
	delete(tmp.ConnectionParameters, "dbname")

	sourceDBConnectionString, pgpass, err := external.ConfigureConnectionToServer(
		ctx,
		client,
		namespaceOfNewCluster,
		&externalCluster,
	)
	if err != nil {
		return nil, err
	}

	// Unfortunately lib/pq doesn't support the passfile
	// connection option so we must rely on an environment
	// variable.
	if pgpass != "" {
		if err = os.Setenv("PGPASSFILE", pgpass); err != nil {
			return nil, err
		}
	}

	return pool.NewConnectionPool(sourceDBConnectionString), nil
}
