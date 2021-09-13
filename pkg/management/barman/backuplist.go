/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package barman contain the utilities to interact with barman-cloud.
//
// This package is able to download the backup catalog given an object store
// and to find the required backup to recreate a cluster given a certain point
// in time. It can also find the latest successful backup, and this is useful
// to recovery from the last consistent state.
//
// A backup catalog is represented by the Catalog structure, and can be
// created using the NewCatalog function or by downloading it from an
// object store via GetBackupList. A backup catalog is just a sorted
// list of BackupInfo objects.
//
// We also have features to gather all the environment variables required
// for the barman-cloud utilities to work correctly.
//
// The functions which call the barman-cloud utilities (such as GetBackupList)
// require the environment variables to be passed, and the calling code is
// supposed gather them (i.e. via the EnvSetCloudCredentials) before calling
// them.
// A Kubernetes client is required to get the environment variables, as we
// need to download the content from the required secrets, but is not required
// to call barman-cloud.
package barman

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"sort"
	"time"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/catalog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// barmanLog is the log that will be used for interactions with Barman
var barmanLog = log.WithName("barman")

// barmanTimeLayout is the format that is being used to parse
// the backupInfo from barman-cloud-backup-list
const barmanTimeLayout = "Mon Jan 2 15:04:05 2006"

// ParseBarmanCloudBackupList parses the output of barman-cloud-backup-list
func ParseBarmanCloudBackupList(output string) (*catalog.Catalog, error) {
	result := &catalog.Catalog{}
	err := json.Unmarshal([]byte(output), result)
	if err != nil {
		return nil, err
	}

	for idx := range result.List {
		if result.List[idx].BeginTimeString != "" {
			result.List[idx].BeginTime, err = time.Parse(barmanTimeLayout, result.List[idx].BeginTimeString)
			if err != nil {
				return nil, err
			}
		}

		if result.List[idx].EndTimeString != "" {
			result.List[idx].EndTime, err = time.Parse(barmanTimeLayout, result.List[idx].EndTimeString)
			if err != nil {
				return nil, err
			}
		}
	}

	// Sort the list of backups in order of time
	sort.Sort(result)

	return result, nil
}

// GetBackupList returns the catalog reading it from the object store
func GetBackupList(
	barmanConfiguration *v1.BarmanObjectStoreConfiguration,
	serverName string,
	env []string) (*catalog.Catalog, error) {
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	options := []string{"--format", "json"}
	if barmanConfiguration.EndpointURL != "" {
		options = append(options, "--endpoint-url", barmanConfiguration.EndpointURL)
	}
	if barmanConfiguration.Data != nil && barmanConfiguration.Data.Encryption != "" {
		options = append(options, "-e", string(barmanConfiguration.Data.Encryption))
	}
	if barmanConfiguration.S3Credentials != nil {
		options = append(
			options,
			"--cloud-provider",
			"aws-s3")
	}
	if barmanConfiguration.AzureCredentials != nil {
		options = append(
			options,
			"--cloud-provider",
			"azure-blob-storage")
	}
	options = append(options, barmanConfiguration.DestinationPath, serverName)

	cmd := exec.Command("barman-cloud-backup-list", options...) // #nosec G204
	cmd.Env = env
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err := cmd.Run()
	if err != nil {
		barmanLog.Error(err,
			"Can't extract backup id using barman-cloud-backup-list",
			"options", options,
			"stdout", stdoutBuffer.String(),
			"stderr", stderrBuffer.String())
		return nil, err
	}

	backupList, err := ParseBarmanCloudBackupList(stdoutBuffer.String())
	if err != nil {
		barmanLog.Error(err, "Can't parse barman-cloud-backup-list output")
		return nil, err
	}

	return backupList, nil
}
