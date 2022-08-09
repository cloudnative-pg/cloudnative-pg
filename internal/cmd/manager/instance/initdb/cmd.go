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

// Package initdb implements the "instance init" subcommand of the operator
package initdb

import (
	"context"
	"os"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
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
	var podName string
	var postInitSQLStr string
	var postInitApplicationSQLStr string
	var postInitTemplateSQLStr string
	var postInitApplicationSQLRefsFolder string

	cmd := &cobra.Command{
		Use: "init [options]",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			initDBFlags, err := shellquote.Split(initDBFlagsString)
			if err != nil {
				log.Error(err, "Error while parsing initdb flags")
				return err
			}

			postInitSQL, err := shellquote.Split(postInitSQLStr)
			if err != nil {
				log.Error(err, "Error while parsing post init SQL queries")
				return err
			}

			postInitApplicationSQL, err := shellquote.Split(postInitApplicationSQLStr)
			if err != nil {
				log.Error(err, "Error while parsing post init template SQL queries")
				return err
			}

			postInitTemplateSQL, err := shellquote.Split(postInitTemplateSQLStr)
			if err != nil {
				log.Error(err, "Error while parsing post init template SQL queries")
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
				PodName:                podName,
				PostInitSQL:            postInitSQL,
				PostInitApplicationSQL: postInitApplicationSQL,
				PostInitTemplateSQL:    postInitTemplateSQL,
				// if the value to postInitApplicationSQLRefsFolder is empty,
				// bootstrap will do nothing for post init application SQL refs.
				PostInitApplicationSQLRefsFolder: postInitApplicationSQLRefsFolder,
			}

			return initSubCommand(ctx, info)
		},
	}

	cmd.Flags().StringVar(&appDBName, "app-db-name", "app",
		"The name of the application containing the database")
	cmd.Flags().StringVar(&appUser, "app-user", "app",
		"The name of the application user")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&initDBFlagsString, "initdb-flags", "", "The list of flags to be passed "+
		"to initdb while creating the initial database")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and the pod in k8s")
	cmd.Flags().StringVar(&parentNode, "parent-node", "", "The origin node")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The pod name to "+
		"be checked against the cluster state")
	cmd.Flags().StringVar(&postInitSQLStr, "post-init-sql", "", "The list of SQL queries to be "+
		"executed to configure the new instance")
	cmd.Flags().StringVar(&postInitApplicationSQLStr, "post-init-application-sql", "", "The list of SQL queries to be "+
		"executed inside application database right after the database is created")
	cmd.Flags().StringVar(&postInitTemplateSQLStr, "post-init-template-sql", "", "The list of SQL queries to be "+
		"executed inside template1 database to configure the new instance")
	cmd.Flags().StringVar(&postInitApplicationSQLRefsFolder, "post-init-application-sql-refs-folder",
		"", "The folder contains a set of SQL files to be executed in alphabetical order "+
			"against the application database immediately after its creationd")

	return cmd
}

func initSubCommand(ctx context.Context, info postgres.InitInfo) error {
	err := info.VerifyPGData()
	if err != nil {
		return err
	}

	err = info.Bootstrap(ctx)
	if err != nil {
		log.Error(err, "Error while bootstrapping data directory")
		return err
	}

	return nil
}
