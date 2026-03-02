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

// Package initdb implements the "instance init" subcommand of the operator
package initdb

import (
	"context"
	"os"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// NewCmd generates the "init" subcommand
func NewCmd() *cobra.Command {
	var appDBName string
	var appUser string
	var clusterName string
	var initDBFlagsString string
	var namespace string
	var parentNode string
	var pgData string
	var pgWal string
	var podName string
	var postInitSQLStr string
	var postInitApplicationSQLStr string
	var postInitTemplateSQLStr string
	var postInitSQLRefsFolder string
	var postInitApplicationSQLRefsFolder string
	var postInitTemplateSQLRefsFolder string

	cmd := &cobra.Command{
		Use: "init [options]",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return management.WaitForGetCluster(cmd.Context(), ctrl.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			contextLogger := log.FromContext(ctx)

			initDBFlags, err := shellquote.Split(initDBFlagsString)
			if err != nil {
				contextLogger.Error(err, "Error while parsing initdb flags")
				return err
			}

			postInitSQL, err := shellquote.Split(postInitSQLStr)
			if err != nil {
				contextLogger.Error(err, "Error while parsing post init SQL queries")
				return err
			}

			postInitApplicationSQL, err := shellquote.Split(postInitApplicationSQLStr)
			if err != nil {
				contextLogger.Error(err, "Error while parsing post init template SQL queries")
				return err
			}

			postInitTemplateSQL, err := shellquote.Split(postInitTemplateSQLStr)
			if err != nil {
				contextLogger.Error(err, "Error while parsing post init template SQL queries")
				return err
			}

			info := postgres.InitInfo{
				ApplicationDatabase:    appDBName,
				ApplicationUser:        appUser,
				ClusterName:            clusterName,
				InitDBOptions:          initDBFlags,
				Namespace:              namespace,
				ParentNode:             parentNode,
				PgData:                 pgData,
				PgWal:                  pgWal,
				PodName:                podName,
				PostInitSQL:            postInitSQL,
				PostInitApplicationSQL: postInitApplicationSQL,
				PostInitTemplateSQL:    postInitTemplateSQL,
				// If the value for an SQLRefsFolder is empty,
				// bootstrap will do nothing for that specific PostInit option.
				PostInitApplicationSQLRefsFolder: postInitApplicationSQLRefsFolder,
				PostInitTemplateSQLRefsFolder:    postInitTemplateSQLRefsFolder,
				PostInitSQLRefsFolder:            postInitSQLRefsFolder,
			}

			return initSubCommand(ctx, info)
		},
		PostRunE: func(cmd *cobra.Command, _ []string) error {
			if err := istio.TryInvokeQuitEndpoint(cmd.Context()); err != nil {
				return err
			}

			return linkerd.TryInvokeShutdownEndpoint(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&appDBName, "app-db-name", "",
		"The name of the application containing the database")
	cmd.Flags().StringVar(&appUser, "app-user", "",
		"The name of the application user")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&initDBFlagsString, "initdb-flags", "", "The list of flags to be passed "+
		"to initdb while creating the initial database")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and the pod in k8s")
	cmd.Flags().StringVar(&parentNode, "parent-node", "", "The origin node")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	cmd.Flags().StringVar(&pgWal, "pg-wal", "", "the PGWAL to be created")
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The pod name to "+
		"be checked against the cluster state")
	cmd.Flags().StringVar(&postInitSQLStr, "post-init-sql", "",
		"The list of SQL queries to be executed to configure the new instance")
	cmd.Flags().StringVar(&postInitApplicationSQLStr, "post-init-application-sql", "",
		"The list of SQL queries to be executed inside application database right after the database is created")
	cmd.Flags().StringVar(&postInitTemplateSQLStr, "post-init-template-sql", "",
		"The list of SQL queries to be executed inside template1 database to configure the new instance")
	cmd.Flags().StringVar(&postInitSQLRefsFolder, "post-init-sql-refs-folder",
		"", "The folder contains a set of SQL files to be executed in alphabetical order")
	cmd.Flags().StringVar(&postInitApplicationSQLRefsFolder, "post-init-application-sql-refs-folder",
		"", "The folder contains a set of SQL files to be executed in alphabetical order "+
			"against the application database immediately after its creation")
	cmd.Flags().StringVar(&postInitTemplateSQLRefsFolder, "post-init-template-sql-refs-folder",
		"", "The folder contains a set of SQL files to be executed in alphabetical order")
	return cmd
}

func initSubCommand(ctx context.Context, info postgres.InitInfo) error {
	contextLogger := log.FromContext(ctx)
	typedClient, err := management.NewControllerRuntimeClient()
	if err != nil {
		return err
	}
	cluster, err := info.LoadCluster(ctx, typedClient)
	if err != nil {
		return err
	}
	// If the user specified an existing directory, we will reuse it.
	reuseDirectory := cluster.Spec.Bootstrap != nil &&
		cluster.Spec.Bootstrap.InitDB != nil &&
		cluster.Spec.Bootstrap.InitDB.ReuseExistingDirectory
	//reuseDirectory := true
	if !reuseDirectory {
		err := info.EnsureTargetDirectoriesDoNotExist(ctx)
		if err != nil {
			return err
		}
	}

	err = info.Bootstrap(ctx, reuseDirectory)
	if err != nil {
		contextLogger.Error(err, "Error while bootstrapping data directory")
		return err
	}

	return nil
}
