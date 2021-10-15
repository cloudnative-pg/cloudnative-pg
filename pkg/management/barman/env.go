/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package barman

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"

	"github.com/blang/semver"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// EnvSetCloudCredentials sets the AWS environment variables given the configuration
// inside the cluster
func EnvSetCloudCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	configuration *apiv1.BarmanObjectStoreConfiguration,
	env []string,
) ([]string, error) {
	if configuration.S3Credentials != nil {
		return envSetAWSCredentials(ctx, c, namespace, configuration, env)
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
	var accessKeyIDSecret corev1.Secret
	var secretAccessKeySecret corev1.Secret

	// Get access key ID
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

var barmanCloudVersionRegex = regexp.MustCompile("barman-cloud.* (?P<Version>.*)")

// GetBarmanCloudVersion retrieves a barman-cloud subcommand version, e.g. barman-cloud-backup, barman-cloud-wal-archive
func GetBarmanCloudVersion(command string) (*semver.Version, error) {
	cmd := exec.Command(command, "--version") // #nosec G204
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("while checking %s version: %w", command, err)
	}
	if cmd.ProcessState.ExitCode() != 0 {
		return nil, fmt.Errorf("exit code different from zero checking %s version", command)
	}

	matches := barmanCloudVersionRegex.FindStringSubmatch(string(out))
	version, err := semver.ParseTolerant(matches[1])
	if err != nil {
		return nil, fmt.Errorf("while parsing %s version: %w", command, err)
	}

	return &version, err
}
