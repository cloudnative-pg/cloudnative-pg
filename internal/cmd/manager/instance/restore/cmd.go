/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package restore implements the "instance restore" subcommand of the operator
package restore

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// NewCmd creates the "restore" subcommand
func NewCmd() *cobra.Command {
	var pwFile string
	var pgData string
	var clusterName string
	var namespace string
	var recoveryTarget string

	cmd := &cobra.Command{
		Use:           "restore [flags]",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			info := postgres.InitInfo{
				PgData:         pgData,
				PasswordFile:   pwFile,
				ClusterName:    clusterName,
				Namespace:      namespace,
				RecoveryTarget: recoveryTarget,
			}

			return restoreSubCommand(ctx, info)
		},
	}

	cmd.Flags().StringVar(&pwFile, "pw-file", "",
		"The file containing the PostgreSQL superuser password to use during the init phase")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and the Pod in k8s")
	cmd.Flags().StringVar(&recoveryTarget, "target", "", "The recovery target in the form of "+
		"PostgreSQL options")

	return cmd
}

func restoreSubCommand(ctx context.Context, info postgres.InitInfo) error {
	status, err := fileutils.FileExists(info.PgData)
	if err != nil {
		log.Error(err, "Error while checking for an existent PGData")
		return err
	}
	if status {
		log.Info("PGData already exists, can't restore over an existing folder")
		return fmt.Errorf("pgdata already existent")
	}

	err = info.VerifyConfiguration()
	if err != nil {
		log.Error(err, "Configuration not valid",
			"info", info)
		return err
	}

	err = info.Restore(ctx)
	if err != nil {
		log.Error(err, "Error while restoring a backup")
		return err
	}

	return nil
}
