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

// Package barman contains the utilities to interact with barman-cloud.
//
// This package is able to download the backup catalog, given an object store,
// and to find the required backup to recreate a cluster, given a certain point
// in time. It can also delete backups according to barman object store configuration and retention policies,
// and find the latest successful backup. This is useful to recovery from the last consistent state.
// We detect the possible commands to be executed, fulfilling the barman capabilities,
// and define an interface for building commands.
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
	barmanCapabilities "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman/capabilities"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/catalog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// barmanLog is the log that will be used for interactions with Barman
var barmanLog = log.WithName("barman")

// barmanTimeLayout is the format that is being used to parse
// the backupInfo from barman-cloud-backup-list
const (
	barmanTimeLayout = "Mon Jan 2 15:04:05 2006"
)

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
	env []string,
) (*catalog.Catalog, error) {
	options := []string{"--format", "json"}

	if barmanConfiguration.EndpointURL != "" {
		options = append(options, "--endpoint-url", barmanConfiguration.EndpointURL)
	}

	if barmanConfiguration.Data != nil && barmanConfiguration.Data.Encryption != "" {
		options = append(options, "-e", string(barmanConfiguration.Data.Encryption))
	}

	options, err := AppendCloudProviderOptionsFromConfiguration(options, barmanConfiguration)
	if err != nil {
		return nil, err
	}

	options = append(options, barmanConfiguration.DestinationPath, serverName)

	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	cmd := exec.Command(barmanCapabilities.BarmanCloudBackupList, options...) // #nosec G204
	cmd.Env = env
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err = cmd.Run()
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
