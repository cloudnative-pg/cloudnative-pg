/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package barman

import (
	"fmt"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	barmanCapabilities "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman/capabilities"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// AppendCloudProviderOptionsFromConfiguration takes an options array and adds the cloud provider specified
// in the Barman configuration object
func AppendCloudProviderOptionsFromConfiguration(
	options []string,
	barmanConfiguration *v1.BarmanObjectStoreConfiguration,
) ([]string, error) {
	return appendCloudProviderOptions(options,
		barmanConfiguration.S3Credentials != nil,
		barmanConfiguration.AzureCredentials != nil,
		barmanConfiguration.GoogleCredentials != nil)
}

// AppendCloudProviderOptionsFromBackup takes an options array and adds the cloud provider specified
// in the Backup object
func AppendCloudProviderOptionsFromBackup(
	options []string,
	backup *v1.Backup,
) ([]string, error) {
	return appendCloudProviderOptions(options,
		backup.Status.S3Credentials != nil,
		backup.Status.AzureCredentials != nil,
		backup.Status.GoogleCredentials != nil)
}

// appendCloudProviderOptions takes an options array and adds the cloud provider specified as arguments
func appendCloudProviderOptions(
	options []string,
	s3Credentials,
	azureCredentials,
	googleCredentials bool) ([]string, error) {
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
		if capabilities.HasAzure {
			options = append(
				options,
				"--cloud-provider",
				"azure-blob-storage")
		} else {
			err := fmt.Errorf(
				"barman >= 2.13 is required to use Azure object storage, current: %v",
				capabilities.Version)
			log.Error(err, "Barman version not supported")
			return nil, err
		}
	case googleCredentials:
		if capabilities.HasGoogle {
			options = append(
				options,
				"--cloud-provider",
				"google-cloud-storage")
		} else {
			err := fmt.Errorf(
				"barman >= 2.19 is required to use Google Cloud Storage, current: %v",
				capabilities.Version)
			log.Error(err, "Barman version not supported")
			return nil, err
		}
	}

	return options, nil
}
