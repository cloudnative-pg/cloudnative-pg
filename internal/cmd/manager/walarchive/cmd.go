/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package walarchive implement the wal-archive command
package walarchive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache"
	cacheClient "github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache/client"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman/archiver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

const (
	// SpoolDirectory is the directory where we spool the WAL files that
	// were pre-archived in parallel
	SpoolDirectory = postgres.ScratchDataDirectory + "/wal-archive-spool"
)

// NewCmd creates the new cobra command
func NewCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:           "wal-archive [name]",
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			contextLog := log.WithName("wal-archive")
			ctx := log.IntoContext(cobraCmd.Context(), contextLog)

			err := run(ctx, args)
			if err != nil {
				contextLog.Error(err, "failed to run wal-archive command")
				return err
			}
			return nil
		},
	}

	return &cmd
}

func run(ctx context.Context, args []string) error {
	startTime := time.Now()
	contextLog := log.FromContext(ctx)
	walName := args[0]

	var cluster *apiv1.Cluster
	var err error

	if cluster, err = cacheClient.GetCluster(); err != nil {
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

	maxParallel := 1
	if cluster.Spec.Backup.BarmanObjectStore.Wal != nil {
		maxParallel = cluster.Spec.Backup.BarmanObjectStore.Wal.MaxParallel
	}

	// Get environment from cache
	var env []string
	env, err = cacheClient.GetEnv(cache.WALArchiveKey)
	if err != nil {
		return fmt.Errorf("failed to get envs: %w", err)
	}

	// Create the archiver
	var walArchiver *archiver.WALArchiver
	if walArchiver, err = archiver.New(ctx, cluster, env, SpoolDirectory); err != nil {
		return fmt.Errorf("while creating the archiver: %w", err)
	}

	// Step 1: check if this WAL file has not been already archived
	var isDeletedFromSpool bool
	isDeletedFromSpool, err = walArchiver.DeleteFromSpool(walName)
	if err != nil {
		return fmt.Errorf("while testing the existence of the WAL file in the spool directory: %w", err)
	}
	if isDeletedFromSpool {
		contextLog.Info("Archived WAL file (parallel)",
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary)
		return nil
	}

	// Step 3: gather the WAL files names to archive
	walFilesList := gatherWALFilesToArchive(ctx, walName, maxParallel)

	options, err := barmanCloudWalArchiveOptions(cluster, cluster.Name)
	if err != nil {
		log.Error(err, "while getting barman-cloud-wal-archive options")
		return err
	}

	// Step 4: archive the WAL files in parallel
	uploadStartTime := time.Now()
	walStatus := walArchiver.ArchiveList(ctx, walFilesList, options)
	if len(walStatus) > 1 {
		contextLog.Info("Completed archive command (parallel)",
			"walsCount", len(walStatus),
			"startTime", startTime,
			"uploadStartTime", uploadStartTime,
			"uploadTotalTime", time.Since(uploadStartTime),
			"totalTime", time.Since(startTime))
	}

	// We return only the first error to PostgreSQL, because the first error
	// is the one raised by the file that PostgreSQL has requested to archive.
	// The other errors are related to WAL files that were pre-archived as
	// a performance optimization and are just logged
	return walStatus[0].Err
}

// gatherWALFilesToArchive reads from the archived status the list of WAL files
// that can be archived in parallel way.
// `requestedWALFile` is the name of the file whose archiving was requested by
// PostgreSQL, and that file is always the first of the list and is always included.
// `parallel` is the maximum number of WALs that we can archive in parallel
func gatherWALFilesToArchive(ctx context.Context, requestedWALFile string, parallel int) (walList []string) {
	contextLog := log.FromContext(ctx)
	pgWalDirectory := path.Join(os.Getenv("PGDATA"), "pg_wal")
	archiveStatusPath := path.Join(pgWalDirectory, "archive_status")
	noMoreWALFilesNeeded := errors.New("no more files needed")

	// slightly more optimized, but equivalent to:
	// walList = []string{requestedWALFile}
	walList = make([]string, 1, 1+parallel)
	walList[0] = requestedWALFile

	err := filepath.WalkDir(archiveStatusPath, func(path string, d os.DirEntry, err error) error {
		// If err is set, it means the current path is a directory and the readdir raised an error
		// The only available option here is to skip the path and log the error.
		if err != nil {
			contextLog.Error(err, "failed reading path", "path", path)
			return filepath.SkipDir
		}

		if len(walList) >= parallel {
			return noMoreWALFilesNeeded
		}

		// We don't process directories beside the archive status path
		if d.IsDir() {
			// We want to proceed exploring the archive status folder
			if path == archiveStatusPath {
				return nil
			}

			return filepath.SkipDir
		}

		// We only process ready files
		if !strings.HasSuffix(path, ".ready") {
			return nil
		}

		walFileName := strings.TrimSuffix(filepath.Base(path), ".ready")

		// We are already archiving the requested WAL file,
		// and we need to avoid archiving it twice.
		// requestedWALFile is usually "pg_wal/wal_file_name" and
		// we compare it with the path we read
		if strings.HasSuffix(requestedWALFile, walFileName) {
			return nil
		}

		walList = append(walList, filepath.Join("pg_wal", walFileName))
		return nil
	})

	// In this point err must be nil or noMoreWALFilesNeeded, if it is something different
	// there is a programming error
	if err != nil && err != noMoreWALFilesNeeded {
		contextLog.Error(err, "unexpected error while reading the list of WAL files to archive")
	}

	return walList
}

func barmanCloudWalArchiveOptions(
	cluster *apiv1.Cluster,
	clusterName string,
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
		serverName)
	return options, nil
}
