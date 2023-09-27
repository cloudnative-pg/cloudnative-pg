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

package utils

import (
	"encoding/json"
	"fmt"
	"os"

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/gomega" // nolint
)

// ExecuteBackup performs a backup and checks the backup status
func ExecuteBackup(
	namespace,
	backupFile string,
	onlyTargetStandbys bool,
	timeoutSeconds int,
	env *TestingEnvironment,
) {
	backupName, err := env.GetResourceNameFromYAML(backupFile)
	Expect(err).ToNot(HaveOccurred())
	Eventually(func() error {
		_, stderr, err := RunUnchecked("kubectl apply -n " + namespace + " -f " + backupFile)
		if err != nil {
			return fmt.Errorf("could not create backup.\nStdErr: %v\nError: %v", stderr, err)
		}
		return nil
	}, RetryTimeout, PollingTime).Should(BeNil())
	backupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      backupName,
	}
	backup := &apiv1.Backup{}
	// Verifying backup status
	Eventually(func() (apiv1.BackupPhase, error) {
		err = env.Client.Get(env.Ctx, backupNamespacedName, backup)
		return backup.Status.Phase, err
	}, timeoutSeconds).Should(BeEquivalentTo(apiv1.BackupPhaseCompleted))
	Eventually(func() (string, error) {
		err = env.Client.Get(env.Ctx, backupNamespacedName, backup)
		if err != nil {
			return "", err
		}
		backupStatus := backup.GetStatus()
		return backupStatus.BeginLSN, err
	}, timeoutSeconds).ShouldNot(BeEmpty())

	var cluster *apiv1.Cluster
	Eventually(func() error {
		var err error
		cluster, err = env.GetCluster(namespace, backup.Spec.Cluster.Name)
		return err
	}, timeoutSeconds).ShouldNot(HaveOccurred())

	backupStatus := backup.GetStatus()
	if cluster.Spec.Backup != nil {
		backupTarget := cluster.Spec.Backup.Target
		if backup.Spec.Target != "" {
			backupTarget = backup.Spec.Target
		}
		switch backupTarget {
		case apiv1.BackupTargetPrimary, "":
			Expect(backupStatus.InstanceID.PodName).To(BeEquivalentTo(cluster.Status.TargetPrimary))
		case apiv1.BackupTargetStandby:
			Expect(backupStatus.InstanceID.PodName).To(BeElementOf(cluster.Status.InstanceNames))
			if onlyTargetStandbys {
				Expect(backupStatus.InstanceID.PodName).NotTo(Equal(cluster.Status.TargetPrimary))
			}
		}
	}

	Expect(backupStatus.BeginWal).NotTo(BeEmpty())
	Expect(backupStatus.EndLSN).NotTo(BeEmpty())
	Expect(backupStatus.EndWal).NotTo(BeEmpty())
}

// CreateClusterFromBackupUsingPITR creates a cluster from backup, using the PITR
func CreateClusterFromBackupUsingPITR(
	namespace,
	clusterName,
	backupFilePath,
	targetTime string,
	env *TestingEnvironment,
) (*apiv1.Cluster, error) {
	backupName, err := env.GetResourceNameFromYAML(backupFilePath)
	if err != nil {
		return nil, err
	}
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	restoreCluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
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
					Backup: &apiv1.BackupSource{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: backupName,
						},
					},
					RecoveryTarget: &apiv1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},
		},
	}
	obj, err := CreateObject(env, restoreCluster)
	if err != nil {
		return nil, err
	}
	cluster, ok := obj.(*apiv1.Cluster)
	if !ok {
		return nil, fmt.Errorf("created object is not of type cluster: %T, %v", obj, obj)
	}
	return cluster, nil
}

// CreateClusterFromExternalClusterBackupWithPITROnAzure creates a cluster on Azure, starting from an external cluster
// backup with PITR
func CreateClusterFromExternalClusterBackupWithPITROnAzure(
	namespace,
	externalClusterName,
	sourceClusterName,
	targetTime,
	storageCredentialsSecretName,
	azStorageAccount string,
	env *TestingEnvironment,
) (*apiv1.Cluster, error) {
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	destinationPath := fmt.Sprintf("https://%v.blob.core.windows.net/%v/", azStorageAccount, sourceClusterName)

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
					Database: "appdb",
					Owner:    "appuser",
					Source:   sourceClusterName,
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
	obj, err := CreateObject(env, restoreCluster)
	if err != nil {
		return nil, err
	}
	cluster, ok := obj.(*apiv1.Cluster)
	if !ok {
		return nil, fmt.Errorf("created object is not of type cluster: %T, %v", obj, obj)
	}
	return cluster, nil
}

// CreateClusterFromExternalClusterBackupWithPITROnMinio creates a cluster on Minio, starting from an external cluster
// backup with PITR
func CreateClusterFromExternalClusterBackupWithPITROnMinio(
	namespace,
	externalClusterName,
	sourceClusterName,
	targetTime string,
	env *TestingEnvironment,
) (*apiv1.Cluster, error) {
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")

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
					Database: "appdb",
					Owner:    "appuser",
					Source:   sourceClusterName,
					RecoveryTarget: &apiv1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},

			ExternalClusters: []apiv1.ExternalCluster{
				{
					Name: sourceClusterName,
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						DestinationPath: "s3://cluster-backups/",
						EndpointURL:     "https://minio-service:9000",
						EndpointCA: &apiv1.SecretKeySelector{
							LocalObjectReference: apiv1.LocalObjectReference{
								Name: "minio-server-ca-secret",
							},
							Key: "ca.crt",
						},
						BarmanCredentials: apiv1.BarmanCredentials{
							AWS: &apiv1.S3Credentials{
								AccessKeyIDReference: &apiv1.SecretKeySelector{
									LocalObjectReference: apiv1.LocalObjectReference{
										Name: "backup-storage-creds",
									},
									Key: "ID",
								},
								SecretAccessKeyReference: &apiv1.SecretKeySelector{
									LocalObjectReference: apiv1.LocalObjectReference{
										Name: "backup-storage-creds",
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
	obj, err := CreateObject(env, restoreCluster)
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
	namespace,
	externalClusterName,
	sourceClusterName,
	targetTime string,
	env *TestingEnvironment,
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
					Database: "appdb",
					Owner:    "appuser",
					Source:   sourceClusterName,
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
	obj, err := CreateObject(env, restoreCluster)
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
func ComposeAzBlobListAzuriteCmd(clusterName string, path string) string {
	return fmt.Sprintf("az storage blob list --container-name %v --query \"[?contains(@.name, \\`%v\\`)].name\" "+
		"--connection-string $AZURE_CONNECTION_STRING",
		clusterName, path)
}

// ComposeAzBlobListCmd builds the Azure storage blob list command
func ComposeAzBlobListCmd(azStorageAccount, azStorageKey, clusterName string, path string) string {
	return fmt.Sprintf("az storage blob list --account-name %v  "+
		"--account-key %v  "+
		"--container-name %v --query \"[?contains(@.name, \\`%v\\`)].name\"",
		azStorageAccount, azStorageKey, clusterName, path)
}

// CountFilesOnAzureBlobStorage counts files on Azure Blob storage
func CountFilesOnAzureBlobStorage(
	azStorageAccount string,
	azStorageKey string,
	clusterName string,
	path string,
) (int, error) {
	azBlobListCmd := ComposeAzBlobListCmd(azStorageAccount, azStorageKey, clusterName, path)
	out, _, err := RunUnchecked(azBlobListCmd)
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
	clusterName string,
	path string,
) (int, error) {
	azBlobListCmd := ComposeAzBlobListAzuriteCmd(clusterName, path)
	out, _, err := RunUnchecked(fmt.Sprintf("kubectl exec -n %v az-cli "+
		"-- /bin/bash -c '%v'", namespace, azBlobListCmd))
	if err != nil {
		return -1, err
	}
	var arr []string
	err = json.Unmarshal([]byte(out), &arr)
	return len(arr), err
}

// GetConditionsInClusterStatus get conditions values as given type from cluster object status
func GetConditionsInClusterStatus(
	namespace,
	clusterName string,
	env *TestingEnvironment,
	conditionType apiv1.ClusterConditionType,
) (*metav1.Condition, error) {
	var cluster *apiv1.Cluster
	var err error

	cluster, err = env.GetCluster(namespace, clusterName)
	if err != nil {
		return nil, err
	}

	for _, cond := range cluster.Status.Conditions {
		if cond.Type == string(conditionType) {
			return &cond, nil
		}
	}

	return nil, fmt.Errorf("no condition matching requested type found: %v", conditionType)
}

// CreateOnDemandBackupViaKubectlPlugin uses the kubectl plugin to create a backup
func CreateOnDemandBackupViaKubectlPlugin(
	namespace,
	clusterName,
	backupName string,
	target apiv1.BackupTarget,
	method apiv1.BackupMethod,
) error {
	command := fmt.Sprintf("kubectl cnpg backup %v -n %v", clusterName, namespace)

	if backupName != "" {
		command = fmt.Sprintf("%v --backup-name %v", command, backupName)
	}
	if target != "" {
		command = fmt.Sprintf("%v --backup-target %v", command, target)
	}
	if method != "" {
		command = fmt.Sprintf("%v --method %v", command, method)
	}

	_, _, err := Run(command)
	return err
}

// CreateOnDemandBackup creates a Backup resource for a given cluster name
func CreateOnDemandBackup(
	namespace,
	clusterName,
	backupName string,
	target apiv1.BackupTarget,
	method apiv1.BackupMethod,
	env *TestingEnvironment,
) (*apiv1.Backup, error) {
	targetBackup := &apiv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupName,
			Namespace: namespace,
		},
		Spec: apiv1.BackupSpec{
			Cluster: apiv1.LocalObjectReference{
				Name: clusterName,
			},
		},
	}

	if target != "" {
		targetBackup.Spec.Target = target
	}
	if method != "" {
		targetBackup.Spec.Method = method
	}

	obj, err := CreateObject(env, targetBackup)
	if err != nil {
		return nil, err
	}
	backup, ok := obj.(*apiv1.Backup)
	if !ok {
		return nil, fmt.Errorf("created object is not of Backup type: %T %v", obj, obj)
	}
	return backup, nil
}

// GetVolumeSnapshot gets a VolumeSnapshot given name and namespace
func (env TestingEnvironment) GetVolumeSnapshot(
	namespace,
	name string,
) (*volumesnapshot.VolumeSnapshot, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	volumeSnapshot := &volumesnapshot.VolumeSnapshot{}
	err := GetObject(&env, namespacedName, volumeSnapshot)
	if err != nil {
		return nil, err
	}
	return volumeSnapshot, nil
}
