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

// AppendCloudProviderOptionsFromConfiguration takes an options array and adds the cloud provider specified
// in the Barman configuration object
func AppendCloudProviderOptionsFromConfiguration(
	options []string,
	barmanConfiguration *v1.BarmanObjectStoreConfiguration,
) ([]string, error) {
	azureInheritFromAzureAD := false
	if barmanConfiguration.Credentials.Azure != nil && barmanConfiguration.Credentials.Azure.InheritFromAzureAD {
		azureInheritFromAzureAD = true
	}
	return appendCloudProviderOptions(options,
		barmanConfiguration.Credentials.AWS != nil,
		barmanConfiguration.Credentials.Azure != nil,
		barmanConfiguration.Credentials.Google != nil,
		azureInheritFromAzureAD,
	)
}

// AppendCloudProviderOptionsFromBackup takes an options array and adds the cloud provider specified
// in the Backup object
func AppendCloudProviderOptionsFromBackup(
	options []string,
	backup *v1.Backup,
) ([]string, error) {
	azureInheritFromAzureAD := false
	if backup.Status.Credentials.Azure != nil && backup.Status.Credentials.Azure.InheritFromAzureAD {
		azureInheritFromAzureAD = true
	}
	return appendCloudProviderOptions(options,
		backup.Status.Credentials.AWS != nil,
		backup.Status.Credentials.Azure != nil,
		backup.Status.Credentials.Google != nil,
		azureInheritFromAzureAD)
}

// appendCloudProviderOptions takes an options array and adds the cloud provider specified as arguments
func appendCloudProviderOptions(
	options []string,
	s3Credentials,
	azureCredentials,
	googleCredentials,
	azureInheritFromAzureAD bool,
) ([]string, error) {
	capabilities, err := barmanCapabilities.CurrentCapabilities()
	if err != nil {
		return nil, err
	}

	switch {
	case s3Credentials:
		if capabilities.HasS3 {
			options = append(
				options,
				"--cloud-provider",
				"aws-s3")
		}
	case azureCredentials:
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

		if !azureInheritFromAzureAD {
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
	case googleCredentials:
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
