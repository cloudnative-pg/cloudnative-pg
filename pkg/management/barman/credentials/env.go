/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package credentials is used to build environment for barman cloud commands
package credentials

import (
	"context"
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// EnvSetBackupCloudCredentials sets the AWS environment variables needed for backups
// given the configuration inside the cluster
func EnvSetBackupCloudCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	configuration *apiv1.BarmanObjectStoreConfiguration,
	env []string,
) ([]string, error) {
	if configuration.EndpointCA != nil && configuration.S3Credentials != nil {
		env = append(env, fmt.Sprintf("AWS_CA_BUNDLE=%s", postgres.BarmanBackupEndpointCACertificateLocation))
	} else if configuration.EndpointCA != nil && configuration.AzureCredentials != nil {
		env = append(env, fmt.Sprintf("REQUESTS_CA_BUNDLE=%s", postgres.BarmanBackupEndpointCACertificateLocation))
	}

	return envSetCloudCredentials(ctx, c, namespace, configuration, env)
}

// EnvSetRestoreCloudCredentials sets the AWS environment variables needed for restores
// given the configuration inside the cluster
func EnvSetRestoreCloudCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	configuration *apiv1.BarmanObjectStoreConfiguration,
	env []string,
) ([]string, error) {
	if configuration.EndpointCA != nil && configuration.S3Credentials != nil {
		env = append(env, fmt.Sprintf("AWS_CA_BUNDLE=%s", postgres.BarmanRestoreEndpointCACertificateLocation))
	} else if configuration.EndpointCA != nil && configuration.AzureCredentials != nil {
		env = append(env, fmt.Sprintf("REQUESTS_CA_BUNDLE=%s", postgres.BarmanRestoreEndpointCACertificateLocation))
	}
	return envSetCloudCredentials(ctx, c, namespace, configuration, env)
}

// envSetCloudCredentials sets the AWS environment variables given the configuration
// inside the cluster
func envSetCloudCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	configuration *apiv1.BarmanObjectStoreConfiguration,
	env []string,
) (envs []string, err error) {
	if configuration.S3Credentials != nil {
		return envSetAWSCredentials(ctx, c, namespace, configuration, env)
	}

	if configuration.GoogleCredentials != nil {
		return envSetGoogleCredentials(ctx, c, namespace, configuration.GoogleCredentials, env)
	}

	return envSetAzureCredentials(ctx, c, namespace, configuration, env)
}

// envSetAWSCredentials sets the AWS environment variables given the configuration
// inside the cluster
func envSetAWSCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	configuration *apiv1.BarmanObjectStoreConfiguration,
	env []string,
) ([]string, error) {
	// check if AWS credentials are defined
	if configuration.S3Credentials == nil {
		return nil, fmt.Errorf("missing S3 credentials")
	}

	if configuration.S3Credentials.InheritFromIAMRole {
		return env, nil
	}

	var accessKeyIDSecret corev1.Secret
	var secretAccessKeySecret corev1.Secret

	// Get access key ID
	if configuration.S3Credentials.AccessKeyIDReference == nil {
		return nil, fmt.Errorf("missing access key ID")
	}
	secretName := configuration.S3Credentials.AccessKeyIDReference.Name
	secretKey := configuration.S3Credentials.AccessKeyIDReference.Key
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, &accessKeyIDSecret)
	if err != nil {
		return nil, fmt.Errorf("while getting access key ID secret: %w", err)
	}

	accessKeyID, ok := accessKeyIDSecret.Data[secretKey]
	if !ok {
		return nil, fmt.Errorf("missing key inside access key ID secret")
	}

	// Get secret access key
	if configuration.S3Credentials.SecretAccessKeyReference == nil {
		return nil, fmt.Errorf("missing secret access key")
	}
	secretName = configuration.S3Credentials.SecretAccessKeyReference.Name
	secretKey = configuration.S3Credentials.SecretAccessKeyReference.Key
	err = c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, &secretAccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("while getting secret access key secret: %w", err)
	}

	secretAccessKey, ok := secretAccessKeySecret.Data[secretKey]
	if !ok {
		return nil, fmt.Errorf("missing key inside secret access key secret")
	}

	env = append(env, fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", accessKeyID))
	env = append(env, fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", secretAccessKey))

	return env, nil
}

// envSetAzureCredentials sets the AWS environment variables given the configuration
// inside the cluster
func envSetAzureCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	configuration *apiv1.BarmanObjectStoreConfiguration,
	env []string,
) ([]string, error) {
	// check if Azure credentials are defined
	if configuration.AzureCredentials == nil {
		return nil, fmt.Errorf("missing Azure credentials")
	}

	var storageAccountSecret corev1.Secret

	// Get storage account name
	if configuration.AzureCredentials.StorageAccount != nil {
		storageAccountName := configuration.AzureCredentials.StorageAccount.Name
		storageAccountKey := configuration.AzureCredentials.StorageAccount.Key
		err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: storageAccountName}, &storageAccountSecret)
		if err != nil {
			return nil, fmt.Errorf("while getting access key ID secret: %w", err)
		}

		storageAccount, ok := storageAccountSecret.Data[storageAccountKey]
		if !ok {
			return nil, fmt.Errorf("missing key inside storage account name secret")
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_ACCOUNT=%s", storageAccount))
	}

	// Get the storage key
	if configuration.AzureCredentials.StorageKey != nil {
		var storageKeySecret corev1.Secret
		storageKeyName := configuration.AzureCredentials.StorageKey.Name
		storageKeyKey := configuration.AzureCredentials.StorageKey.Key

		err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: storageKeyName}, &storageKeySecret)
		if err != nil {
			return nil, fmt.Errorf("while getting access key ID secret: %w", err)
		}

		storageKey, ok := storageKeySecret.Data[storageKeyKey]
		if !ok {
			return nil, fmt.Errorf("missing key inside storage key secret")
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_KEY=%s", storageKey))
	}

	// Get the SAS token
	if configuration.AzureCredentials.StorageSasToken != nil {
		var storageSasTokenSecret corev1.Secret
		storageSasTokenName := configuration.AzureCredentials.StorageSasToken.Name
		storageSasTokenKey := configuration.AzureCredentials.StorageSasToken.Key

		err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: storageSasTokenName}, &storageSasTokenSecret)
		if err != nil {
			return nil, fmt.Errorf("while getting storage SAS token secret: %w", err)
		}

		storageKey, ok := storageSasTokenSecret.Data[storageSasTokenKey]
		if !ok {
			return nil, fmt.Errorf("missing key inside storage SAS token secret")
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_SAS_TOKEN=%s", storageKey))
	}

	if configuration.AzureCredentials.ConnectionString != nil {
		var connectionStringSecret corev1.Secret
		connectionStringName := configuration.AzureCredentials.ConnectionString.Name
		connectionStringKey := configuration.AzureCredentials.ConnectionString.Key

		err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: connectionStringName}, &connectionStringSecret)
		if err != nil {
			return nil, fmt.Errorf("while getting storage SAS token secret: %w", err)
		}

		storageKey, ok := connectionStringSecret.Data[connectionStringKey]
		if !ok {
			return nil, fmt.Errorf("missing key inside connection string secret")
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_CONNECTION_STRING=%s", storageKey))
	}

	return env, nil
}

func envSetGoogleCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	googleCredentials *apiv1.GoogleCredentials,
	env []string,
) ([]string, error) {
	var applicationCredentialsContent []byte

	if googleCredentials.GKEEnvironment &&
		googleCredentials.ApplicationCredentials == nil {
		return env, reconcileGoogleCredentials(googleCredentials, applicationCredentialsContent)
	}

	var applicationCredentialsSecret corev1.Secret

	secretName := googleCredentials.ApplicationCredentials.Name
	secretKey := googleCredentials.ApplicationCredentials.Key
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, &applicationCredentialsSecret)
	if err != nil {
		return nil, fmt.Errorf("while getting application credentials secret: %w", err)
	}

	applicationCredentialsContent, ok := applicationCredentialsSecret.Data[secretKey]
	if !ok {
		return nil, fmt.Errorf("missing key `%v` in application credentials secret", secretKey)
	}

	if err := reconcileGoogleCredentials(googleCredentials, applicationCredentialsContent); err != nil {
		return nil, err
	}

	env = append(env, "GOOGLE_APPLICATION_CREDENTIALS=/controller/.application_credentials.json")

	return env, nil
}

func reconcileGoogleCredentials(
	googleCredentials *apiv1.GoogleCredentials,
	applicationCredentialsContent []byte,
) error {
	credentialsPath := "/controller/.application_credentials.json"

	if googleCredentials == nil {
		return fileutils.RemoveFile(credentialsPath)
	}

	_, err := fileutils.WriteFileAtomic(credentialsPath, applicationCredentialsContent, 0o600)

	return err
}
