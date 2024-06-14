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
	"fmt"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	barmanCapabilities "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/capabilities"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// CloudWalRestoreOptions returns the options needed to execute the barman command successfully
func CloudWalRestoreOptions(
	configuration *v1.BarmanObjectStoreConfiguration,
	clusterName string,
) ([]string, error) {
	var options []string
	if len(configuration.EndpointURL) > 0 {
		options = append(
			options,
			"--endpoint-url",
			configuration.EndpointURL)
	}

	options, err := AppendCloudProviderOptionsFromConfiguration(options, configuration)
	if err != nil {
		return nil, err
	}

	serverName := clusterName
	if len(configuration.ServerName) != 0 {
		serverName = configuration.ServerName
	}

	options = append(options, configuration.DestinationPath, serverName)
	return options, nil
}

// AppendCloudProviderOptionsFromConfiguration takes an options array and adds the cloud provider specified
// in the Barman configuration object
func AppendCloudProviderOptionsFromConfiguration(
	options []string,
	barmanConfiguration *v1.BarmanObjectStoreConfiguration,
) ([]string, error) {
	return appendCloudProviderOptions(options, barmanConfiguration.BarmanCredentials)
}

// AppendCloudProviderOptionsFromBackup takes an options array and adds the cloud provider specified
// in the Backup object
func AppendCloudProviderOptionsFromBackup(
	options []string,
	backup *v1.Backup,
) ([]string, error) {
	return appendCloudProviderOptions(options, backup.Status.BarmanCredentials)
}

// appendCloudProviderOptions takes an options array and adds the cloud provider specified as arguments
func appendCloudProviderOptions(options []string, credentials v1.BarmanCredentials) ([]string, error) {
	capabilities, err := barmanCapabilities.CurrentCapabilities()
	if err != nil {
		return nil, err
	}

	// TODO: evaluate whether to add a separate function for this
	// if supported (Barman 3.10.1 or above) specify `--no-partial` by default
	if !capabilities.HasNoPartialWalRestore {
		options = append(
			options,
			"--no-partial")
	}

	switch {
	case credentials.AWS != nil:
		if capabilities.HasS3 {
			options = append(
				options,
				"--cloud-provider",
				"aws-s3")
		}
	case credentials.Azure != nil:
		if !capabilities.HasAzure {
			err := fmt.Errorf(
				"barman >= 2.13 is required to use Azure object storage, current: %v",
				capabilities.Version)
			log.Error(err, "Barman version not supported")
			return nil, err
		}

		options = append(
			options,
			"--cloud-provider",
			"azure-blob-storage")

		if !credentials.Azure.InheritFromAzureAD {
			break
		}

		if !capabilities.HasAzureManagedIdentity {
			err := fmt.Errorf(
				"barman >= 2.18 is required to use azureInheritFromAzureAD, current: %v",
				capabilities.Version)
			log.Error(err, "Barman version not supported")
			return nil, err
		}

		options = append(
			options,
			"--credential",
			"managed-identity")
	case credentials.Google != nil:
		if !capabilities.HasGoogle {
			err := fmt.Errorf(
				"barman >= 2.19 is required to use Google Cloud Storage, current: %v",
				capabilities.Version)
			log.Error(err, "Barman version not supported")
			return nil, err
		}
		options = append(
			options,
			"--cloud-provider",
			"google-cloud-storage")
	}

	return options, nil
}
