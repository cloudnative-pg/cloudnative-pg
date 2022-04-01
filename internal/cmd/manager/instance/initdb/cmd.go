/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package initdb implements the "instance init" subcommand of the operator
package initdb

import (
	"os"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
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

	cmd := &cobra.Command{
		Use: "init [options]",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			}

			return initSubCommand(info)
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

	return cmd
}

func initSubCommand(info postgres.InitInfo) error {
	err := info.VerifyPGData()
	if err != nil {
		return err
	}

	err = info.Bootstrap()
	if err != nil {
		log.Error(err, "Error while bootstrapping data directory")
		return err
	}

	return nil
}
