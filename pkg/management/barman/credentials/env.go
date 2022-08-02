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

// Package credentials is used to build environment for barman cloud commands
package credentials

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
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
	if configuration.EndpointCA != nil && configuration.BarmanCredentials.AWS != nil {
		env = append(env, fmt.Sprintf("AWS_CA_BUNDLE=%s", postgres.BarmanBackupEndpointCACertificateLocation))
	} else if configuration.EndpointCA != nil && configuration.BarmanCredentials.Azure != nil {
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
	if configuration.EndpointCA != nil && configuration.BarmanCredentials.AWS != nil {
		env = append(env, fmt.Sprintf("AWS_CA_BUNDLE=%s", postgres.BarmanRestoreEndpointCACertificateLocation))
	} else if configuration.EndpointCA != nil && configuration.BarmanCredentials.Azure != nil {
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
	if configuration.BarmanCredentials.AWS != nil {
		return envSetAWSCredentials(ctx, c, namespace, configuration.BarmanCredentials.AWS, env)
	}

	if configuration.BarmanCredentials.Google != nil {
		return envSetGoogleCredentials(ctx, c, namespace, configuration.BarmanCredentials.Google, env)
	}

	return envSetAzureCredentials(ctx, c, namespace, configuration, env)
}

// envSetAWSCredentials sets the AWS environment variables given the configuration
// inside the cluster
func envSetAWSCredentials(
	ctx context.Context,
	client client.Client,
	namespace string,
	s3credentials *apiv1.S3Credentials,
	env []string,
) ([]string, error) {
	// check if AWS credentials are defined
	if s3credentials == nil {
		return nil, fmt.Errorf("missing S3 credentials")
	}

	if s3credentials.InheritFromIAMRole {
		return env, nil
	}

	// Get access key ID
	if s3credentials.AccessKeyIDReference == nil {
		return nil, fmt.Errorf("missing access key ID")
	}
	accessKeyID, accessKeyErr := extractValueFromSecret(
		ctx,
		client,
		s3credentials.AccessKeyIDReference,
		namespace,
	)
	if accessKeyErr != nil {
		return nil, accessKeyErr
	}

	// Get secret access key
	if s3credentials.SecretAccessKeyReference == nil {
		return nil, fmt.Errorf("missing secret access key")
	}
	secretAccessKey, secretAccessErr := extractValueFromSecret(
		ctx,
		client,
		s3credentials.SecretAccessKeyReference,
		namespace,
	)
	if secretAccessErr != nil {
		return nil, secretAccessErr
	}

	if s3credentials.RegionReference != nil {
		region, regionErr := extractValueFromSecret(
			ctx,
			client,
			s3credentials.RegionReference,
			namespace,
		)
		if regionErr != nil {
			return nil, regionErr
		}
		env = append(env, fmt.Sprintf("AWS_DEFAULT_REGION=%s", region))
	}

	// Get session token secret
	if s3credentials.SessionToken != nil {
		sessionKey, sessErr := extractValueFromSecret(
			ctx,
			client,
			s3credentials.SessionToken,
			namespace,
		)
		if sessErr != nil {
			return nil, sessErr
		}
		env = append(env, fmt.Sprintf("AWS_SESSION_TOKEN=%s", sessionKey))
	}

	env = append(env, fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", accessKeyID))
	env = append(env, fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", secretAccessKey))

	return env, nil
}

// envSetAzureCredentials sets the Azure environment variables given the configuration
// inside the cluster
func envSetAzureCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	configuration *apiv1.BarmanObjectStoreConfiguration,
	env []string,
) ([]string, error) {
	// check if Azure credentials are defined
	if configuration.BarmanCredentials.Azure == nil {
		return nil, fmt.Errorf("missing Azure credentials")
	}

	if configuration.BarmanCredentials.Azure.InheritFromAzureAD {
		return env, nil
	}

	// Get storage account name
	if configuration.BarmanCredentials.Azure.StorageAccount != nil {
		storageAccount, err := extractValueFromSecret(
			ctx,
			c,
			configuration.BarmanCredentials.Azure.StorageAccount,
			namespace,
		)
		if err != nil {
			return nil, err
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_ACCOUNT=%s", storageAccount))
	}

	// Get the storage key
	if configuration.BarmanCredentials.Azure.StorageKey != nil {
		storageKey, err := extractValueFromSecret(
			ctx,
			c,
			configuration.BarmanCredentials.Azure.StorageKey,
			namespace,
		)
		if err != nil {
			return nil, err
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_KEY=%s", storageKey))
	}

	// Get the SAS token
	if configuration.BarmanCredentials.Azure.StorageSasToken != nil {
		storageSasToken, err := extractValueFromSecret(
			ctx,
			c,
			configuration.BarmanCredentials.Azure.StorageSasToken,
			namespace,
		)
		if err != nil {
			return nil, err
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_SAS_TOKEN=%s", storageSasToken))
	}

	if configuration.BarmanCredentials.Azure.ConnectionString != nil {
		connString, err := extractValueFromSecret(
			ctx,
			c,
			configuration.BarmanCredentials.Azure.ConnectionString,
			namespace,
		)
		if err != nil {
			return nil, err
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_CONNECTION_STRING=%s", connString))
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

	applicationCredentialsContent, err := extractValueFromSecret(
		ctx,
		c,
		googleCredentials.ApplicationCredentials,
		namespace,
	)
	if err != nil {
		return nil, err
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

func extractValueFromSecret(
	ctx context.Context,
	c client.Client,
	secretReference *apiv1.SecretKeySelector,
	namespace string,
) ([]byte, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretReference.Name}, secret)
	if err != nil {
		return nil, fmt.Errorf("while getting secret %s: %w", secretReference.Name, err)
	}

	value, ok := secret.Data[secretReference.Key]
	if !ok {
		return nil, fmt.Errorf("missing key %s, inside secret %s", secretReference.Key, secretReference.Name)
	}

	return value, nil
}
