/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package initdb implements the "instance init" subcommand of the operator
package initdb

import (
	"fmt"
	"os"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// NewCmd generates the "init" subcommand
func NewCmd() *cobra.Command {
	var pwFile string
	var appDBName string
	var appUser string
	var appPwFile string
	var pgData string
	var parentNode string
	var podName string
	var clusterName string
	var namespace string
	var initDBFlagsString string
	var postInitSQLStr string

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

			info := postgres.InitInfo{
				PgData:                  pgData,
				PasswordFile:            pwFile,
				ApplicationDatabase:     appDBName,
				ApplicationUser:         appUser,
				ApplicationPasswordFile: appPwFile,
				ParentNode:              parentNode,
				ClusterName:             clusterName,
				Namespace:               namespace,
				InitDBOptions:           initDBFlags,
				PodName:                 podName,
				PostInitSQL:             postInitSQL,
			}

			return initSubCommand(info)
		},
	}

	cmd.Flags().StringVar(&pwFile, "pw-file", "",
		"The file containing the PostgreSQL superuser password to be used during the init phase")
	cmd.Flags().StringVar(&appDBName, "app-db-name", "app",
		"The name of the application containing the database")
	cmd.Flags().StringVar(&appUser, "app-user", "app",
		"The name of the application user")
	cmd.Flags().StringVar(&appPwFile, "app-pw-file", "",
		"The file that stores the password for the application user")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	cmd.Flags().StringVar(&parentNode, "parent-node", "", "The origin node")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().StringVar(&initDBFlagsString, "initdb-flags", "", "The list of flags to be passed "+
		"to initdb while creating the initial database")
	cmd.Flags().StringVar(&postInitSQLStr, "post-init-sql", "", "The list of SQL queries to be "+
		"executed to configure the new instance")
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of this pod, to "+
		"be checked against the cluster state")

	return cmd
}

func initSubCommand(info postgres.InitInfo) error {
	status, err := fileutils.FileExists(info.PgData)
	if err != nil {
		log.Error(err, "Error while checking for an existent PGData")
		return err
	}
	if status {
		log.Info("PGData already exists, no need to init")
		return fmt.Errorf("PGData already exists")
	}

	err = info.VerifyConfiguration()
	if err != nil {
		log.Error(err, "Configuration not valid",
			"info", info)
		return err
	}

	err = info.Bootstrap()
	if err != nil {
		log.Error(err, "Error while bootstrapping data directory")
		return err
	}

	return nil
}
