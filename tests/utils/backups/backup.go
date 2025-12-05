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
	"fmt"
	"os"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"
)

// List gathers the current list of backup in namespace
func List(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) (*apiv1.BackupList, error) {
	backupList := &apiv1.BackupList{}
	err := crudClient.List(
		ctx, backupList, client.InNamespace(namespace),
	)
	return backupList, err
}

// Create creates a Backup resource for a given cluster name
func Create(
	ctx context.Context,
	crudClient client.Client,
	targetBackup apiv1.Backup,
) (*apiv1.Backup, error) {
	obj, err := objects.Create(ctx, crudClient, &targetBackup)
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
func GetVolumeSnapshot(
	ctx context.Context,
	crudClient client.Client,
	namespace, name string,
) (*volumesnapshotv1.VolumeSnapshot, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{}
	err := objects.Get(ctx, crudClient, namespacedName, volumeSnapshot)
	if err != nil {
		return nil, err
	}
	return volumeSnapshot, nil
}

// AssertBackupConditionInClusterStatus check that the backup condition in the Cluster's Status
// eventually returns true
func AssertBackupConditionInClusterStatus(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) {
	ginkgo.By(fmt.Sprintf("waiting for backup condition status in cluster '%v'", clusterName), func() {
		gomega.Eventually(func() (string, error) {
			getBackupCondition, err := GetConditionsInClusterStatus(
				ctx, crudClient,
				namespace, clusterName,
				apiv1.ConditionBackup,
			)
			if err != nil {
				return "", err
			}
			return string(getBackupCondition.Status), nil
		}, 300, 5).Should(gomega.BeEquivalentTo("True"))
	})
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

	_, _, err := run.Run(command)
	return err
}

// GetConditionsInClusterStatus get conditions values as given type from cluster object status
func GetConditionsInClusterStatus(
	ctx context.Context,
	crudClient client.Client,
	namespace,
	clusterName string,
	conditionType apiv1.ClusterConditionType,
) (*metav1.Condition, error) {
	var cluster *apiv1.Cluster
	var err error

	cluster, err = clusterutils.Get(ctx, crudClient, namespace, clusterName)
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

// Execute performs a backup and checks the backup status
func Execute(
	ctx context.Context,
	crudClient client.Client,
	scheme *runtime.Scheme,
	namespace,
	backupFile string,
	onlyTargetStandbys bool,
	timeoutSeconds int,
) *apiv1.Backup {
	backupName, err := yaml.GetResourceNameFromYAML(scheme, backupFile)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Eventually(func() error {
		_, stderr, err := run.Unchecked("kubectl apply -n " + namespace + " -f " + backupFile)
		if err != nil {
			return fmt.Errorf("could not create backup.\nStdErr: %v\nError: %v", stderr, err)
		}
		return nil
	}, 60, objects.PollingTime).Should(gomega.Succeed())
	backupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      backupName,
	}
	backup := &apiv1.Backup{}
	// Verifying backup status
	gomega.Eventually(func() (apiv1.BackupPhase, error) {
		err = crudClient.Get(ctx, backupNamespacedName, backup)
		return backup.Status.Phase, err
	}, timeoutSeconds).Should(gomega.BeEquivalentTo(apiv1.BackupPhaseCompleted))
	gomega.Eventually(func() (string, error) {
		err = crudClient.Get(ctx, backupNamespacedName, backup)
		if err != nil {
			return "", err
		}
		backupStatus := backup.GetStatus()
		return backupStatus.BeginLSN, err
	}, timeoutSeconds).ShouldNot(gomega.BeEmpty())

	var cluster *apiv1.Cluster
	gomega.Eventually(func() error {
		var err error
		cluster, err = clusterutils.Get(ctx, crudClient, namespace, backup.Spec.Cluster.Name)
		return err
	}, timeoutSeconds).ShouldNot(gomega.HaveOccurred())

	backupStatus := backup.GetStatus()
	if cluster.Spec.Backup != nil {
		backupTarget := cluster.Spec.Backup.Target
		if backup.Spec.Target != "" {
			backupTarget = backup.Spec.Target
		}
		switch backupTarget {
		case apiv1.BackupTargetPrimary, "":
			gomega.Expect(backupStatus.InstanceID.PodName).To(gomega.BeEquivalentTo(cluster.Status.TargetPrimary))
		case apiv1.BackupTargetStandby:
			gomega.Expect(backupStatus.InstanceID.PodName).To(gomega.BeElementOf(cluster.Status.InstanceNames))
			if onlyTargetStandbys {
				gomega.Expect(backupStatus.InstanceID.PodName).NotTo(gomega.Equal(cluster.Status.TargetPrimary))
			}
		}
	}

	gomega.Expect(backupStatus.BeginWal).NotTo(gomega.BeEmpty())
	gomega.Expect(backupStatus.EndLSN).NotTo(gomega.BeEmpty())
	gomega.Expect(backupStatus.EndWal).NotTo(gomega.BeEmpty())
	return backup
}

// CreateClusterFromBackupUsingPITR creates a cluster from backup, using the PITR
func CreateClusterFromBackupUsingPITR(
	ctx context.Context,
	crudClient client.Client,
	scheme *runtime.Scheme,
	namespace,
	clusterName,
	backupFilePath,
	targetTime string,
) (*apiv1.Cluster, error) {
	backupName, err := yaml.GetResourceNameFromYAML(scheme, backupFilePath)
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

// CreateClusterFromExternalClusterBackupWithPITROnMinio creates a cluster on Minio, starting from an external cluster
// backup with PITR
func CreateClusterFromExternalClusterBackupWithPITROnMinio(
	ctx context.Context,
	crudClient client.Client,
	namespace,
	externalClusterName,
	sourceClusterName,
	targetTime string,
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
						DestinationPath: "s3://cluster-backups/",
						EndpointURL:     "https://minio-service.minio:9000",
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
