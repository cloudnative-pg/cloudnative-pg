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
	"context"
	"fmt"
	"os/exec"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	barmanCapabilities "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/capabilities"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/catalog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

func executeQueryCommand(
	ctx context.Context,
	barmanCommand string,
	barmanConfiguration *v1.BarmanObjectStoreConfiguration,
	serverName string,
	additionalOptions []string,
	env []string,
) (string, error) {
	contextLogger := log.FromContext(ctx).WithName("barman")

	options := []string{"--format", "json"}

	if barmanConfiguration.EndpointURL != "" {
		options = append(options, "--endpoint-url", barmanConfiguration.EndpointURL)
	}

	options, err := AppendCloudProviderOptionsFromConfiguration(options, barmanConfiguration)
	if err != nil {
		return "", err
	}

	options = append(options, barmanConfiguration.DestinationPath, serverName)
	options = append(options, additionalOptions...)

	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	cmd := exec.Command(barmanCommand, options...) // #nosec G204
	cmd.Env = env
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err = cmd.Run()
	if err != nil {
		contextLogger.Error(err,
			"Can't extract backup id using barman-cloud-backup-list",
			"options", options,
			"stdout", stdoutBuffer.String(),
			"stderr", stderrBuffer.String())
		return "", err
	}

	return stdoutBuffer.String(), nil
}

// GetBackupList returns the catalog reading it from the object store
func GetBackupList(
	ctx context.Context,
	barmanConfiguration *v1.BarmanObjectStoreConfiguration,
	serverName string,
	env []string,
) (*catalog.Catalog, error) {
	contextLogger := log.FromContext(ctx).WithName("barman")

	rawJSON, err := executeQueryCommand(
		ctx,
		barmanCapabilities.BarmanCloudBackupList,
		barmanConfiguration,
		serverName,
		[]string{},
		env,
	)
	if err != nil {
		return nil, err
	}
	backupList, err := catalog.NewCatalogFromBarmanCloudBackupList(rawJSON)
	if err != nil {
		contextLogger.Error(err, "Can't parse barman-cloud-backup-list output")
		return nil, err
	}

	return backupList, nil
}

// GetBackupByName returns the backup data found for a given backup
func GetBackupByName(
	ctx context.Context,
	backupName string,
	serverName string,
	barmanConfiguration *v1.BarmanObjectStoreConfiguration,
	env []string,
) (*catalog.BarmanBackup, error) {
	contextLogger := log.FromContext(ctx)

	rawJSON, err := executeQueryCommand(
		ctx,
		barmanCapabilities.BarmanCloudBackupShow,
		barmanConfiguration,
		serverName,
		[]string{backupName},
		env,
	)
	if err != nil {
		return nil, err
	}

	contextLogger.Debug("raw backup barman object", "rawBarmanObject", rawJSON)

	return catalog.NewBackupFromBarmanCloudBackupShow(rawJSON)
}

// GetLatestBackup returns the latest executed backup
func GetLatestBackup(
	ctx context.Context,
	serverName string,
	barmanConfiguration *v1.BarmanObjectStoreConfiguration,
	env []string,
) (*catalog.BarmanBackup, error) {
	contextLogger := log.FromContext(ctx)
	// Extracting the latest backup using barman-cloud-backup-list
	backupList, err := GetBackupList(ctx, barmanConfiguration, serverName, env)
	if err != nil {
		// Proper logging already happened inside GetBackupList
		return nil, err
	}

	contextLogger.Debug("raw backup list object", "backupList", backupList)

	// We have just made a new backup, if the backup list is empty
	// something is going wrong in the cloud storage
	if backupList.Len() == 0 {
		return nil, fmt.Errorf("no backup found on the remote object storage")
	}

	return backupList.LatestBackupInfo(), nil
}
