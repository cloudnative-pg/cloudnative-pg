/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package barman

import (
	"bytes"
	"os/exec"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// DeleteBackupsByPolicy executes a command that deletes backups, given the Barman object store configuration,
// the retention policies, the server name and the environment variables
func DeleteBackupsByPolicy(backupConfig *v1.BackupConfiguration, serverName string, env []string) error {
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	barmanConfiguration := backupConfig.BarmanObjectStore
	var options []string
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

	cmd := exec.Command("barman-cloud-backup-delete", options...) // #nosec G204
	cmd.Env = env
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err = cmd.Run()
	if err != nil {
		barmanLog.Error(err,
			"Can't extract backup id using barman-cloud-backup-delete",
			"options", options,
			"stdout", stdoutBuffer.String(),
			"stderr", stderrBuffer.String())
		return err
	}

	barmanLog.Info("Barman Cloud Status: ", "stdout", stdoutBuffer.String())

	return nil
}
