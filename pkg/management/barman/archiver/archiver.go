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

// Package archiver manages the WAL archiving process
package archiver

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/spool"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/execlog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/plugin-barman-cloud/pkg/walarchive"
)

const (
	// CheckEmptyWalArchiveFile is the name of the file in the PGDATA that,
	// if present, requires the WAL archiver to check that the backup object
	// store is empty.
	CheckEmptyWalArchiveFile = ".check-empty-wal-archive"
)

// WALArchiver is a structure containing every info need to archive a set of WAL files
// using barman-cloud-wal-archive
type WALArchiver struct {
	// The cluster for which we are archiving
	cluster *apiv1.Cluster

	// The spool of WAL files to be archived in parallel
	spool *spool.WALSpool

	// The environment that should be used to invoke barman-cloud-wal-archive
	env []string

	pgDataDirectory string

	// this should become a grpc interface
	barmanArchiver *walarchive.BarmanArchiver
}

// WALArchiverResult contains the result of the archival of one WAL
type WALArchiverResult struct {
	// The WAL that have been archived
	WalName string

	// If not nil, this is the error that has been detected
	Err error

	// The time when we started barman-cloud-wal-archive
	StartTime time.Time

	// The time when end barman-cloud-wal-archive ended
	EndTime time.Time
}

// New creates a new WAL archiver
func New(
	ctx context.Context,
	cluster *apiv1.Cluster,
	env []string,
	spoolDirectory string,
	pgDataDirectory string,
) (archiver *WALArchiver, err error) {
	contextLog := log.FromContext(ctx)
	var walArchiveSpool *spool.WALSpool

	if walArchiveSpool, err = spool.New(spoolDirectory); err != nil {
		contextLog.Info("Cannot initialize the WAL spool", "spoolDirectory", spoolDirectory)
		return nil, fmt.Errorf("while creating spool directory: %w", err)
	}

	archiver = &WALArchiver{
		cluster:         cluster,
		spool:           walArchiveSpool,
		env:             env,
		pgDataDirectory: pgDataDirectory,
		barmanArchiver: &walarchive.BarmanArchiver{
			Env:          env,
			RunStreaming: execlog.RunStreaming,
			Touch:        walArchiveSpool.Touch,
			RemoveEmptyFileArchive: func() error {
				// Removes the `.check-empty-wal-archive` file inside PGDATA after the
				// first successful archival of a WAL file.
				filePath := path.Join(archiver.pgDataDirectory, CheckEmptyWalArchiveFile)
				if err := fileutils.RemoveFile(filePath); err != nil {
					return fmt.Errorf("error while deleting the check WAL file flag: %w", err)
				}
				return nil
			},
		},
	}
	return archiver, nil
}

// DeleteFromSpool checks if a WAL file is in the spool and, if it is, remove it
func (archiver *WALArchiver) DeleteFromSpool(walName string) (hasBeenDeleted bool, err error) {
	var isContained bool

	// this code assumes the wal-archive command is run at most once at each instant,
	// given that PostgreSQL will call it sequentially without overlapping
	isContained, err = archiver.spool.Contains(walName)
	if !isContained || err != nil {
		return false, err
	}

	return true, archiver.spool.Remove(walName)
}

// ArchiveList archives a list of WAL files in parallel
func (archiver *WALArchiver) ArchiveList(
	ctx context.Context,
	walNames []string,
	options []string,
) (result []WALArchiverResult) {
	res := archiver.barmanArchiver.ArchiveList(ctx, walNames, options)
	for _, re := range res {
		result = append(result, WALArchiverResult{
			WalName:   re.WalName,
			Err:       re.Err,
			StartTime: re.StartTime,
			EndTime:   re.EndTime,
		})
	}
	return result
}

// IsCheckWalArchiveFlagFilePresent returns true if the file CheckEmptyWalArchiveFile is present in the PGDATA directory
func (archiver *WALArchiver) IsCheckWalArchiveFlagFilePresent(ctx context.Context, pgDataDirectory string) bool {
	contextLogger := log.FromContext(ctx)
	filePath := filepath.Join(pgDataDirectory, CheckEmptyWalArchiveFile)

	exists, err := fileutils.FileExists(filePath)
	if err != nil {
		contextLogger.Error(err, "error while checking for the existence of the CheckEmptyWalArchiveFile")
	}
	// If the check empty wal archive file doesn't exist this it's a no-op
	if !exists {
		contextLogger.Debug("WAL check flag file not found, skipping check")
		return false
	}

	return exists
}

// CheckWalArchiveDestination checks if the destinationObjectStore is ready perform archiving.
// Based on this ticket in Barman https://github.com/EnterpriseDB/barman/issues/432
// and its implementation https://github.com/EnterpriseDB/barman/pull/443
// The idea here is to check ONLY if we're archiving the wal files for the first time in the bucket
// since in this case the command barman-cloud-check-wal-archive will fail if the bucket exist and
// contain wal files inside
func (archiver *WALArchiver) CheckWalArchiveDestination(ctx context.Context, options []string) error {
	return archiver.barmanArchiver.CheckWalArchiveDestination(ctx, options)
}

// BarmanCloudCheckWalArchiveOptions create the options needed for the `barman-cloud-check-wal-archive`
// command.
func (archiver *WALArchiver) BarmanCloudCheckWalArchiveOptions(
	cluster *apiv1.Cluster,
	clusterName string,
) ([]string, error) {
	configuration := cluster.Spec.Backup.BarmanObjectStore

	var options []string
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
