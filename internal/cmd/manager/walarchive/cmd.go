/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package walarchive implement the wal-archive command
package walarchive

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache"
	cacheClient "github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache/client"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman"
	barmanCapabilities "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman/capabilities"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/execlog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// NewCmd creates the new cobra command
func NewCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:           "wal-archive [name]",
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			contextLog := log.WithName("wal-archive")
			err := run(contextLog, args)
			if err != nil {
				contextLog.Error(err, "failed to run wal-archive command")
				return err
			}
			return nil
		},
	}

	return &cmd
}

func run(contextLog log.Logger, args []string) error {
	walName := args[0]

	var cluster *apiv1.Cluster
	var err error

	cluster, err = cacheClient.GetCluster()
	if err != nil {
		contextLog.Error(err, "Error while getting cluster from cache")
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		// Backup not configured, skipping WAL
		contextLog.Info("Backup not configured, skip WAL archiving",
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
		)
		return nil
	}

	options, err := barmanCloudWalArchiveOptions(*cluster, cluster.Name, walName)
	if err != nil {
		contextLog.Error(err, "while getting barman-cloud-wal-archive options")
		return err
	}

	env, err := cacheClient.GetEnv(cache.WALArchiveKey)
	if err != nil {
		contextLog.Error(err, "Error while getting environment from cache")
		return fmt.Errorf("failed to get envs: %w", err)
	}

	contextLog.Trace("Executing "+barmanCapabilities.BarmanCloudWalArchive,
		"walName", walName,
		"currentPrimary", cluster.Status.CurrentPrimary,
		"targetPrimary", cluster.Status.TargetPrimary,
		"options", options,
	)

	barmanCloudWalArchiveCmd := exec.Command(barmanCapabilities.BarmanCloudWalArchive, options...) // #nosec G204
	barmanCloudWalArchiveCmd.Env = env

	err = execlog.RunStreaming(barmanCloudWalArchiveCmd, barmanCapabilities.BarmanCloudWalArchive)
	if err != nil {
		contextLog.Error(err, "Error invoking "+barmanCapabilities.BarmanCloudWalArchive,
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
			"options", options,
			"exitCode", barmanCloudWalArchiveCmd.ProcessState.ExitCode(),
		)
		return fmt.Errorf("unexpected failure invoking %s: %w", barmanCapabilities.BarmanCloudWalArchive, err)
	}

	contextLog.Info("Archived WAL file",
		"walName", walName,
		"currentPrimary", cluster.Status.CurrentPrimary,
		"targetPrimary", cluster.Status.TargetPrimary,
	)

	return nil
}

func barmanCloudWalArchiveOptions(
	cluster apiv1.Cluster,
	clusterName string,
	walName string,
) ([]string, error) {
	configuration := cluster.Spec.Backup.BarmanObjectStore

	var options []string
	if configuration.Wal != nil {
		if len(configuration.Wal.Compression) != 0 {
			options = append(
				options,
				fmt.Sprintf("--%v", configuration.Wal.Compression))
		}
		if len(configuration.Wal.Encryption) != 0 {
			options = append(
				options,
				"-e",
				string(configuration.Wal.Encryption))
		}
	}
	if len(configuration.EndpointURL) > 0 {
		options = append(
			options,
			"--endpoint-url",
			configuration.EndpointURL)
	}

	options, err := barman.AppendCloudProviderOptionsFromConfiguration(options, configuration)
	if err != nil {
		return nil, err
	}

	serverName := clusterName
	if len(configuration.ServerName) != 0 {
		serverName = configuration.ServerName
	}
	options = append(
		options,
		configuration.DestinationPath,
		serverName,
		walName)
	return options, nil
}
