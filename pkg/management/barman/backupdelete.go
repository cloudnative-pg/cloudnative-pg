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
	"fmt"
	"os/exec"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	barmanCapabilities "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/capabilities"
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

	if barmanConfiguration.Data != nil && barmanConfiguration.Data.Encryption != "" {
		options = append(options, "-e", string(barmanConfiguration.Data.Encryption))
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
