/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package backups

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/deployments"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
)

const (
	azuriteImage       = "mcr.microsoft.com/azure-storage/azurite"
	azuriteClientImage = "mcr.microsoft.com/azure-cli"
)

// AzureConfiguration contains the variables needed to run the azure test environment correctly
type AzureConfiguration struct {
	StorageAccount string
	StorageKey     string
	BlobContainer  string
}

// NewAzureConfigurationFromEnv creates a new AzureConfiguration from the environment variables
func NewAzureConfigurationFromEnv() AzureConfiguration {
	return AzureConfiguration{
		StorageAccount: os.Getenv("AZURE_STORAGE_ACCOUNT"),
		StorageKey:     os.Getenv("AZURE_STORAGE_KEY"),
		BlobContainer:  os.Getenv("AZURE_BLOB_CONTAINER"),
	}
}

// CreateCertificateSecretsOnAzurite will create secrets for Azurite deployment
func CreateCertificateSecretsOnAzurite(
	ctx context.Context,
	crudClient client.Client,
	namespace,
	clusterName,
	azuriteCaSecName,
	azuriteTLSSecName string,
) error {
	// create CA certificates
	_, caPair, err := secrets.CreateSecretCA(
		ctx, crudClient,
		namespace, clusterName, azuriteCaSecName,
		true,
	)
	if err != nil {
		return err
	}
	// sign and create secret using CA certificate and key
	serverPair, err := caPair.CreateAndSignPair("azurite", certs.CertTypeServer,
		[]string{"azurite.internal.mydomain.net, azurite.default.svc, azurite.default,"},
	)
	if err != nil {
		return err
	}
	serverSecret := serverPair.GenerateCertificateSecret(namespace, azuriteTLSSecName)
	err = crudClient.Create(ctx, serverSecret)
	if err != nil {
		return err
	}
	return nil
}

// CreateStorageCredentialsOnAzurite will create credentials for Azurite
func CreateStorageCredentialsOnAzurite(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) error {
	azuriteSecrets := getStorageCredentials(namespace)
	return crudClient.Create(ctx, &azuriteSecrets)
}

// InstallAzurite will set up Azurite in defined namespace and creates service
func InstallAzurite(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) error {
	azuriteDeployment := getAzuriteDeployment(namespace)
	err := crudClient.Create(ctx, &azuriteDeployment)
	if err != nil {
		return err
	}
	// Wait for the Azurite pod to be ready
	deploymentNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      "azurite",
	}
	deployment := &appsv1.Deployment{}
	err = crudClient.Get(ctx, deploymentNamespacedName, deployment)
	if err != nil {
		return err
	}
	err = deployments.WaitForReady(ctx, crudClient, deployment, 300)
	if err != nil {
		return err
	}
	azuriteService := getAzuriteService(namespace)
	err = crudClient.Create(ctx, &azuriteService)
	return err
}

// InstallAzCli will install Az cli
func InstallAzCli(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) error {
	azCLiPod := getAzuriteClientPod(namespace)
	err := pods.CreateAndWaitForReady(ctx, crudClient, &azCLiPod, 180)
	if err != nil {
		return err
	}
	return nil
}

// getAzuriteClientPod get the cli client pod/home/zeus/src/cloudnative-pg/pkg
func getAzuriteClientPod(namespace string) corev1.Pod {
	seccompProfile := &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}

	cliClientPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "az-cli",
			Labels:    map[string]string{"run": "az-cli"},
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "az-cli",
					Image: azuriteClientImage,
					Args:  []string{"/bin/bash", "-c", "sleep 500000"},
					Env: []corev1.EnvVar{
						{
							Name: "AZURE_CONNECTION_STRING",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "azurite",
									},
									Key: "AZURE_CONNECTION_STRING",
								},
							},
						},
						{
							Name:  "REQUESTS_CA_BUNDLE",
							Value: "/etc/ssl/certs/rootCA.pem",
						},
						{
							Name:  "HOME",
							Value: "/azurite",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "cert",
							MountPath: "/etc/ssl/certs",
						},
						{
							Name:      "azurite",
							MountPath: "/azurite",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						SeccompProfile:           seccompProfile,
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "azurite-ca-secret",
							Items: []corev1.KeyToPath{
								{
									Key:  "ca.crt",
									Path: "rootCA.pem",
								},
							},
						},
					},
				},
				{
					Name: "azurite",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: seccompProfile,
			},
		},
	}
	return cliClientPod
}

// getAzuriteService get the service for Azurite
func getAzuriteService(namespace string) corev1.Service {
	azuriteService := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azurite",
			Labels:    map[string]string{"app": "azurite"},
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     10000,
					Protocol: "TCP",
					TargetPort: intstr.IntOrString{
						IntVal: 10000,
					},
				},
			},
			Selector: map[string]string{"app": "azurite"},
		},
	}
	return azuriteService
}

// getAzuriteDeployment get the deployment for Azurite
func getAzuriteDeployment(namespace string) appsv1.Deployment {
	replicas := int32(1)
	seccompProfile := &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}

	azuriteDeployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azurite",
			Namespace: namespace,
			Labels:    map[string]string{"app": "azurite"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "azurite"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "azurite"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image:   azuriteImage,
							Name:    "azurite",
							Command: []string{"azurite"},
							Args: []string{
								"--skipApiVersionCheck",
								"-l", "/data", "--cert", "/etc/ssl/certs/azurite.pem",
								"--key", "/etc/ssl/certs/azurite-key.pem",
								"--oauth", "basic", "--blobHost", "0.0.0.0",
							},
							Env: []corev1.EnvVar{
								{
									Name: "AZURITE_ACCOUNTS",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "azurite",
											},
											Key: "AZURITE_ACCOUNTS",
										},
									},
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 10000,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/data",
									Name:      "data-volume",
								},
								{
									MountPath: "/etc/ssl/certs",
									Name:      "cert",
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								SeccompProfile:           seccompProfile,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data-volume",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "cert",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "azurite-tls-secret",
									Items: []corev1.KeyToPath{
										{
											Key:  "tls.crt",
											Path: "azurite.pem",
										},
										{
											Key:  "tls.key",
											Path: "azurite-key.pem",
										},
									},
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: seccompProfile,
					},
				},
			},
		},
	}
	return azuriteDeployment
}

// getStorageCredentials get storageCredentials for Azurite
func getStorageCredentials(namespace string) corev1.Secret {
	azuriteStorageSecrets := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "azurite",
		},
		StringData: map[string]string{
			"AZURITE_ACCOUNTS": "storageaccountname:c3RvcmFnZWFjY291bnRrZXk=",
			"AZURE_CONNECTION_STRING": "DefaultEndpointsProtocol=https;AccountName=storageaccountname;" +
				"AccountKey=c3RvcmFnZWFjY291bnRrZXk=;BlobEndpoint=https://azurite:10000/storageaccountname;",
		},
	}
	return azuriteStorageSecrets
}

// CreateClusterFromExternalClusterBackupWithPITROnAzure creates a cluster on Azure, starting from an external cluster
// backup with PITR
func CreateClusterFromExternalClusterBackupWithPITROnAzure(
	ctx context.Context,
	crudClient client.Client,
	namespace,
	externalClusterName,
	sourceClusterName,
	targetTime,
	storageCredentialsSecretName,
	azStorageAccount,
	azBlobContainer string,
) (*apiv1.Cluster, error) {
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	destinationPath := fmt.Sprintf("https://%v.blob.core.windows.net/%v/",
		azStorageAccount, azBlobContainer)

	restoreCluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalClusterName,
			Namespace: namespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances: 3,

			StorageConfiguration: apiv1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClassName,
			},

			PostgresConfiguration: apiv1.PostgresConfiguration{
				Parameters: map[string]string{
					"log_checkpoints":             "on",
					"log_lock_waits":              "on",
					"log_min_duration_statement":  "1000",
					"log_statement":               "ddl",
					"log_temp_files":              "1024",
					"log_autovacuum_min_duration": "1s",
					"log_replication_commands":    "on",
				},
			},

			Bootstrap: &apiv1.BootstrapConfiguration{
				Recovery: &apiv1.BootstrapRecovery{
					Source: sourceClusterName,
					RecoveryTarget: &apiv1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},

			ExternalClusters: []apiv1.ExternalCluster{
				{
					Name: sourceClusterName,
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						DestinationPath: destinationPath,
						BarmanCredentials: apiv1.BarmanCredentials{
							Azure: &apiv1.AzureCredentials{
								StorageAccount: &apiv1.SecretKeySelector{
									LocalObjectReference: apiv1.LocalObjectReference{
										Name: storageCredentialsSecretName,
									},
									Key: "ID",
								},
								StorageKey: &apiv1.SecretKeySelector{
									LocalObjectReference: apiv1.LocalObjectReference{
										Name: storageCredentialsSecretName,
									},
									Key: "KEY",
								},
							},
						},
					},
				},
			},
		},
	}
	obj, err := objects.Create(ctx, crudClient, restoreCluster)
	if err != nil {
		return nil, err
	}
	cluster, ok := obj.(*apiv1.Cluster)
	if !ok {
		return nil, fmt.Errorf("created object is not of type cluster: %T, %v", obj, obj)
	}
	return cluster, nil
}

// CreateClusterFromExternalClusterBackupWithPITROnAzurite creates a cluster with Azurite, starting from an external
// cluster backup with PITR
func CreateClusterFromExternalClusterBackupWithPITROnAzurite(
	ctx context.Context,
	crudClient client.Client,
	namespace,
	externalClusterName,
	sourceClusterName,
	targetTime string,
) (*apiv1.Cluster, error) {
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	DestinationPath := fmt.Sprintf("https://azurite:10000/storageaccountname/%v", sourceClusterName)

	restoreCluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalClusterName,
			Namespace: namespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances: 3,

			StorageConfiguration: apiv1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClassName,
			},

			PostgresConfiguration: apiv1.PostgresConfiguration{
				Parameters: map[string]string{
					"log_checkpoints":             "on",
					"log_lock_waits":              "on",
					"log_min_duration_statement":  "1000",
					"log_statement":               "ddl",
					"log_temp_files":              "1024",
					"log_autovacuum_min_duration": "1s",
					"log_replication_commands":    "on",
				},
			},

			Bootstrap: &apiv1.BootstrapConfiguration{
				Recovery: &apiv1.BootstrapRecovery{
					Source: sourceClusterName,
					RecoveryTarget: &apiv1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},

			ExternalClusters: []apiv1.ExternalCluster{
				{
					Name: sourceClusterName,
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						DestinationPath: DestinationPath,
						EndpointCA: &apiv1.SecretKeySelector{
							LocalObjectReference: apiv1.LocalObjectReference{
								Name: "azurite-ca-secret",
							},
							Key: "ca.crt",
						},
						BarmanCredentials: apiv1.BarmanCredentials{
							Azure: &apiv1.AzureCredentials{
								ConnectionString: &apiv1.SecretKeySelector{
									LocalObjectReference: apiv1.LocalObjectReference{
										Name: "azurite",
									},
									Key: "AZURE_CONNECTION_STRING",
								},
							},
						},
					},
				},
			},
		},
	}
	obj, err := objects.Create(ctx, crudClient, restoreCluster)
	if err != nil {
		return nil, err
	}
	cluster, ok := obj.(*apiv1.Cluster)
	if !ok {
		return nil, fmt.Errorf("created object is not of type cluster: %T, %v", obj, obj)
	}
	return cluster, nil
}

// ComposeAzBlobListAzuriteCmd builds the Azure storage blob list command for Azurite
func ComposeAzBlobListAzuriteCmd(clusterName, path string) string {
	return fmt.Sprintf("az storage blob list --container-name %v --query \"[?contains(@.name, \\`%v\\`)].name\" "+
		"--connection-string $AZURE_CONNECTION_STRING",
		clusterName, path)
}

// ComposeAzBlobListCmd builds the Azure storage blob list command
func ComposeAzBlobListCmd(
	configuration AzureConfiguration,
	clusterName,
	path string,
) string {
	return fmt.Sprintf("az storage blob list --account-name %v  "+
		"--account-key %v  "+
		"--container-name %v  "+
		"--prefix %v/  "+
		"--query \"[?contains(@.name, \\`%v\\`)].name\"",
		configuration.StorageAccount, configuration.StorageKey, configuration.BlobContainer, clusterName, path)
}

// CountFilesOnAzureBlobStorage counts files on Azure Blob storage
func CountFilesOnAzureBlobStorage(
	configuration AzureConfiguration,
	clusterName,
	path string,
) (int, error) {
	azBlobListCmd := ComposeAzBlobListCmd(configuration, clusterName, path)
	out, _, err := run.Unchecked(azBlobListCmd)
	if err != nil {
		return -1, err
	}
	var arr []string
	err = json.Unmarshal([]byte(out), &arr)
	return len(arr), err
}

// CountFilesOnAzuriteBlobStorage counts files on Azure Blob storage. using Azurite
func CountFilesOnAzuriteBlobStorage(
	namespace,
	clusterName,
	path string,
) (int, error) {
	azBlobListCmd := ComposeAzBlobListAzuriteCmd(clusterName, path)
	out, _, err := run.Unchecked(fmt.Sprintf("kubectl exec -n %v az-cli "+
		"-- /bin/bash -c '%v'", namespace, azBlobListCmd))
	if err != nil {
		return -1, err
	}
	var arr []string
	err = json.Unmarshal([]byte(out), &arr)
	return len(arr), err
}

// verifySASTokenWriteActivity returns true if the given token has RW permissions,
// otherwise it returns false
func verifySASTokenWriteActivity(containerName string, id string, key string) bool {
	_, _, err := run.Unchecked(fmt.Sprintf("az storage container create "+
		"--name %v --account-name %v "+
		"--sas-token %v", containerName, id, key))

	return err == nil
}

// CreateSASTokenCredentials generates Secrets for the Azure Blob Storage
func CreateSASTokenCredentials(
	ctx context.Context,
	crudClient client.Client,
	namespace, id, key string,
) error {
	// Adding 24 hours to the current time
	date := time.Now().UTC().Add(time.Hour * 24)
	// Creating date time format for az command
	expiringDate := fmt.Sprintf("%v"+"-"+"%d"+"-"+"%v"+"T"+"%v"+":"+"%v"+"Z",
		date.Year(),
		date.Month(),
		date.Day(),
		date.Hour(),
		date.Minute())

	out, _, err := run.Run(fmt.Sprintf(
		// SAS Token at Blob Container level does not currently work in Barman Cloud
		// https://github.com/EnterpriseDB/barman/issues/388
		// we will use SAS Token at Storage Account level
		// ( "az storage container generate-sas --account-name %v "+
		// "--name %v "+
		// "--https-only --permissions racwdl --auth-mode key --only-show-errors "+
		// "--expiry \"$(date -u -d \"+4 hours\" '+%%Y-%%m-%%dT%%H:%%MZ')\"",
		// id, blobContainerName )
		"az storage account generate-sas --account-name %v "+
			"--https-only --permissions cdlruwap --account-key %v "+
			"--resource-types co --services b --expiry %v -o tsv",
		id, key, expiringDate))
	if err != nil {
		return err
	}
	SASTokenRW := strings.TrimRight(out, "\n")

	out, _, err = run.Run(fmt.Sprintf(
		"az storage account generate-sas --account-name %v "+
			"--https-only --permissions lr --account-key %v "+
			"--resource-types co --services b --expiry %v -o tsv",
		id, key, expiringDate))
	if err != nil {
		return err
	}

	SASTokenRO := strings.TrimRight(out, "\n")
	isReadWrite := verifySASTokenWriteActivity("restore-cluster-sas", id, SASTokenRO)
	if isReadWrite {
		return fmt.Errorf("expected token to be ready only")
	}

	_, err = secrets.CreateObjectStorageSecret(
		ctx, crudClient,
		namespace, "backup-storage-creds-sas",
		id, SASTokenRW,
	)
	if err != nil {
		return err
	}

	_, err = secrets.CreateObjectStorageSecret(ctx, crudClient,
		namespace, "restore-storage-creds-sas",
		id, SASTokenRO,
	)
	if err != nil {
		return err
	}

	return nil
}
