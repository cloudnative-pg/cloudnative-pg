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

package barman

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	barmanCapabilities "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/capabilities"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/catalog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// DeleteBackupsByPolicy executes a command that deletes backups, given the Barman object store configuration,
// the retention policies, the server name and the environment variables
func DeleteBackupsByPolicy(backupConfig *v1.BackupConfiguration, serverName string, env []string) error {
	capabilities, err := barmanCapabilities.CurrentCapabilities()
	if err != nil {
		return err
	}

	if !capabilities.HasRetentionPolicy {
		err := fmt.Errorf(
			"barman >= 2.14 is required to use retention policy, current: %v",
			capabilities.Version)
		barmanLog.Error(err, "Failed applying backup retention policies")
		return err
	}

	barmanConfiguration := backupConfig.BarmanObjectStore
	var options []string
	if barmanConfiguration.EndpointURL != "" {
		options = append(options, "--endpoint-url", barmanConfiguration.EndpointURL)
	}

	options, err = AppendCloudProviderOptionsFromConfiguration(options, barmanConfiguration)
	if err != nil {
		return err
	}

	parsedPolicy, err := utils.ParsePolicy(backupConfig.RetentionPolicy)
	if err != nil {
		return err
	}

	options = append(
		options,
		"--retention-policy",
		parsedPolicy,
		barmanConfiguration.DestinationPath,
		serverName)

	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	cmd := exec.Command(barmanCapabilities.BarmanCloudBackupDelete, options...) // #nosec G204
	cmd.Env = env
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err = cmd.Run()
	if err != nil {
		barmanLog.Error(err,
			"Error invoking "+barmanCapabilities.BarmanCloudBackupDelete,
			"options", options,
			"stdout", stdoutBuffer.String(),
			"stderr", stderrBuffer.String())
		return err
	}

	return nil
}

// DeleteBackupsNotInCatalog deletes all Backup objects pointing to the given cluster that are not
// present in the backup anymore
func DeleteBackupsNotInCatalog(
	ctx context.Context,
	cli client.Client,
	cluster *v1.Cluster,
	catalog catalog.Catalog,
) error {
	// We had two options:
	//
	// A. quicker
	// get policy checker function
	// get all backups in the namespace for this cluster
	// check with policy checker function if backup should be deleted, then delete it if true
	//
	// B. more precise
	// get the catalog (GetBackupList)
	// get all backups in the namespace for this cluster
	// go through all backups and delete them if not in the catalog
	//
	// 1: all backups in the bucket should be also in the cluster
	// 2: all backups in the cluster should be in the bucket
	//
	// A can violate 1 and 2
	// A + B can still violate 2
	// B satisfies 1 and 2

	// We chose to go with B

	backups := v1.BackupList{}
	err := cli.List(ctx, &backups, client.InNamespace(cluster.GetNamespace()))
	if err != nil {
		return fmt.Errorf("while getting backups: %w", err)
	}

	var errors []error
	for id, backup := range backups.Items {
		if backup.Spec.Cluster.Name != cluster.GetName() ||
			backup.Status.Phase != v1.BackupPhaseCompleted ||
			!useSameBackupLocation(&backup.Status, cluster) {
			continue
		}
		var found bool
		for _, barmanBackup := range catalog {
			if backup.Status.BackupID == barmanBackup.ID {
				found = true
				break
			}
		}
		// here we could add further checks, e.g. if the backup is not found but would still
		// be in the retention policy we could either not delete it or update it is status
		if !found {
			err := cli.Delete(ctx, &backups.Items[id])
			if err != nil {
				errors = append(errors, fmt.Errorf(
					"while deleting backup %s/%s: %w",
					backup.Namespace,
					backup.Name,
					err,
				))
			}
		}
	}

	if errors != nil {
		return fmt.Errorf("got errors while deleting Backups not in the cluster: %v", errors)
	}
	return nil
}

// useSameBackupLocation checks whether the given backup was taken using the same configuration as provided
func useSameBackupLocation(backup *v1.BackupStatus, cluster *v1.Cluster) bool {
	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		return false
	}
	configuration := cluster.Spec.Backup.BarmanObjectStore
	return backup.EndpointURL == configuration.EndpointURL &&
		backup.DestinationPath == configuration.DestinationPath &&
		(backup.ServerName == configuration.ServerName ||
			// if not specified we use the cluster name as server name
			(configuration.ServerName == "" && backup.ServerName == cluster.Name)) &&
		reflect.DeepEqual(backup.S3Credentials, configuration.S3Credentials) &&
		reflect.DeepEqual(backup.AzureCredentials, configuration.AzureCredentials) &&
		reflect.DeepEqual(backup.GoogleCredentials, configuration.GoogleCredentials)
}
