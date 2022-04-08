/*
Copyright 2019-2022 The CloudNativePG Contributors

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
	"os/exec"
	"path"
	"sync"
	"time"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	barmanCapabilities "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman/capabilities"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman/spool"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/execlog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
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
) (archiver *WALArchiver, err error) {
	contextLog := log.FromContext(ctx)
	var walArchiveSpool *spool.WALSpool

	if walArchiveSpool, err = spool.New(spoolDirectory); err != nil {
		contextLog.Info("Cannot initialize the WAL spool", "spoolDirectory", spoolDirectory)
		return nil, fmt.Errorf("while creating spool directory: %w", err)
	}

	archiver = &WALArchiver{
		cluster: cluster,
		spool:   walArchiveSpool,
		env:     env,
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
	contextLog := log.FromContext(ctx)
	result = make([]WALArchiverResult, len(walNames))

	var waitGroup sync.WaitGroup
	for idx := range walNames {
		waitGroup.Add(1)
		go func(walIndex int) {
			walStatus := &result[walIndex]
			walStatus.WalName = walNames[walIndex]
			walStatus.StartTime = time.Now()
			walStatus.Err = archiver.Archive(walNames[walIndex], options)
			walStatus.EndTime = time.Now()
			if walStatus.Err == nil && walIndex != 0 {
				walStatus.Err = archiver.spool.Touch(walNames[walIndex])
			}

			elapsedWalTime := walStatus.EndTime.Sub(walStatus.StartTime)
			if walStatus.Err != nil {
				contextLog.Warning(
					"Failed archiving WAL: PostgreSQL will retry",
					"walName", walStatus.WalName,
					"startTime", walStatus.StartTime,
					"endTime", walStatus.EndTime,
					"elapsedWalTime", elapsedWalTime,
					"error", walStatus.Err)
			} else {
				contextLog.Info(
					"Archived WAL file",
					"walName", walStatus.WalName,
					"startTime", walStatus.StartTime,
					"endTime", walStatus.EndTime,
					"elapsedWalTime", elapsedWalTime)
			}

			waitGroup.Done()
		}(idx)
	}

	waitGroup.Wait()
	return result
}

// Archive archives a certain WAL file using barman-cloud-wal-archive.
// See archiveWALFileList for the meaning of the parameters
func (archiver *WALArchiver) Archive(walName string, baseOptions []string) error {
	options := make([]string, len(baseOptions), len(baseOptions)+1)
	copy(options, baseOptions)
	options = append(options, walName)

	log.Trace("Executing "+barmanCapabilities.BarmanCloudWalArchive,
		"walName", walName,
		"currentPrimary", archiver.cluster.Status.CurrentPrimary,
		"targetPrimary", archiver.cluster.Status.TargetPrimary,
		"options", options,
	)

	barmanCloudWalArchiveCmd := exec.Command(barmanCapabilities.BarmanCloudWalArchive, options...) // #nosec G204
	barmanCloudWalArchiveCmd.Env = archiver.env

	err := execlog.RunStreaming(barmanCloudWalArchiveCmd, barmanCapabilities.BarmanCloudWalArchive)
	if err != nil {
		log.Error(err, "Error invoking "+barmanCapabilities.BarmanCloudWalArchive,
			"walName", walName,
			"currentPrimary", archiver.cluster.Status.CurrentPrimary,
			"targetPrimary", archiver.cluster.Status.TargetPrimary,
			"options", options,
			"exitCode", barmanCloudWalArchiveCmd.ProcessState.ExitCode(),
		)
		return fmt.Errorf("unexpected failure invoking %s: %w", barmanCapabilities.BarmanCloudWalArchive, err)
	}

	return nil
}

// CheckWalArchive checks if the destinationObjectStore is ready perform archiving.
// Based on this ticket in Barman https://github.com/EnterpriseDB/barman/issues/432
// and its implementation https://github.com/EnterpriseDB/barman/pull/443
// The idea here is to check ONLY if we're archiving the wal files for the first time in the bucket
// since in this case the command barman-cloud-check-wal-archive will fail if the bucket exist and
// contain wal files inside
func (archiver *WALArchiver) CheckWalArchive(ctx context.Context, walFilesList, options []string) error {
	contextLogger := log.FromContext(ctx)

	// If walFileList is empty then, this is a no-op just like the method ArchiveList
	if len(walFilesList) == 0 {
		return nil
	}

	// Get the first wal file from the list
	walName := path.Base(walFilesList[0])
	// We check that we have the first wal file of the first timeline, otherwise, there's nothing to do here
	if walName != "000000010000000000000001" {
		return nil
	}

	contextLogger.Info("barman-cloud-check-wal-archive checking the first wal", "walName", walName)

	// Check barman compatibility
	capabilities, err := barmanCapabilities.CurrentCapabilities()
	if err != nil {
		return err
	}

	if !capabilities.HasCheckWalArchive {
		contextLogger.Warning("barman-cloud-check-wal-archive cannot be used, is recommended to upgrade" +
			" to version 2.18 or above.")
		return nil
	}

	contextLogger.Trace("Executing "+barmanCapabilities.BarmanCloudCheckWalArchive,
		"walName", walName,
		"currentPrimary", archiver.cluster.Status.CurrentPrimary,
		"targetPrimary", archiver.cluster.Status.TargetPrimary,
		"options", options,
	)

	barmanCloudWalArchiveCmd := exec.Command(barmanCapabilities.BarmanCloudCheckWalArchive, options...) // #nosec G204
	barmanCloudWalArchiveCmd.Env = archiver.env

	err = execlog.RunStreaming(barmanCloudWalArchiveCmd, barmanCapabilities.BarmanCloudCheckWalArchive)
	if err != nil {
		contextLogger.Error(err, "Error invoking "+barmanCapabilities.BarmanCloudCheckWalArchive,
			"walName", walName,
			"currentPrimary", archiver.cluster.Status.CurrentPrimary,
			"targetPrimary", archiver.cluster.Status.TargetPrimary,
			"options", options,
			"exitCode", barmanCloudWalArchiveCmd.ProcessState.ExitCode(),
		)
		return fmt.Errorf("unexpected failure invoking %s: %w", barmanCapabilities.BarmanCloudWalArchive, err)
	}

	contextLogger.Trace("barman-cloud-check-wal-archive command execution completed")

	return nil
}
