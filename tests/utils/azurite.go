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

	apiv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	utils2 "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
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

func newAzureConfigurationFromEnv() AzureConfiguration {
	return AzureConfiguration{
		StorageAccount: os.Getenv("AZURE_STORAGE_ACCOUNT"),
		StorageKey:     os.Getenv("AZURE_STORAGE_KEY"),
		BlobContainer:  os.Getenv("AZURE_BLOB_CONTAINER"),
	}
}

// CreateCertificateSecretsOnAzurite will create secrets for Azurite deployment
func CreateCertificateSecretsOnAzurite(
	namespace,
	clusterName,
	azuriteCaSecName,
	azuriteTLSSecName string,
	env *TestingEnvironment,
) error {
	// create CA certificates
	_, caPair, err := CreateSecretCA(namespace, clusterName, azuriteCaSecName, true, env)
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
	err = env.Client.Create(env.Ctx, serverSecret)
	if err != nil {
		return err
	}
	return nil
}

// CreateStorageCredentialsOnAzurite will create credentials for Azurite
func CreateStorageCredentialsOnAzurite(namespace string, env *TestingEnvironment) error {
	azuriteSecrets := getStorageCredentials(namespace)
	return env.Client.Create(env.Ctx, &azuriteSecrets)
}

// InstallAzurite will set up Azurite in defined namespace and creates service
func InstallAzurite(namespace string, env *TestingEnvironment) error {
	azuriteDeployment := getAzuriteDeployment(namespace)
	err := env.Client.Create(env.Ctx, &azuriteDeployment)
	if err != nil {
		return err
	}
	// Wait for the Azurite pod to be ready
	deploymentNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      "azurite",
	}
	deployment := &apiv1.Deployment{}
	err = env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
	if err != nil {
		return err
	}
	err = DeploymentWaitForReady(env, deployment, 300)
	if err != nil {
		return err
	}
	azuriteService := getAzuriteService(namespace)
	err = env.Client.Create(env.Ctx, &azuriteService)
	return err
}

// InstallAzCli will install Az cli
func InstallAzCli(namespace string, env *TestingEnvironment) error {
	azCLiPod := getAzuriteClientPod(namespace)
	err := PodCreateAndWaitForReady(env, &azCLiPod, 180)
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
func getAzuriteDeployment(namespace string) apiv1.Deployment {
	replicas := int32(1)
	seccompProfile := &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}

	azuriteDeployment := apiv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azurite",
			Namespace: namespace,
			Labels:    map[string]string{"app": "azurite"},
		},
		Spec: apiv1.DeploymentSpec{
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
	namespace,
	externalClusterName,
	sourceClusterName,
	targetTime,
	storageCredentialsSecretName,
	azStorageAccount,
	azBlobContainer string,
	env *TestingEnvironment,
) (*v1.Cluster, error) {
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	destinationPath := fmt.Sprintf("https://%v.blob.core.windows.net/%v/",
		azStorageAccount, azBlobContainer)

	restoreCluster := &v1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalClusterName,
			Namespace: namespace,
		},
		Spec: v1.ClusterSpec{
			Instances: 3,

			StorageConfiguration: v1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClassName,
			},

			PostgresConfiguration: v1.PostgresConfiguration{
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

			Bootstrap: &v1.BootstrapConfiguration{
				Recovery: &v1.BootstrapRecovery{
					Source: sourceClusterName,
					RecoveryTarget: &v1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},

			ExternalClusters: []v1.ExternalCluster{
				{
					Name: sourceClusterName,
					BarmanObjectStore: &v1.BarmanObjectStoreConfiguration{
						DestinationPath: destinationPath,
						BarmanCredentials: v1.BarmanCredentials{
							Azure: &v1.AzureCredentials{
								StorageAccount: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
										Name: storageCredentialsSecretName,
									},
									Key: "ID",
								},
								StorageKey: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
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
	cluster, ok := obj.(*v1.Cluster)
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
) (*v1.Cluster, error) {
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	DestinationPath := fmt.Sprintf("https://azurite:10000/storageaccountname/%v", sourceClusterName)

	restoreCluster := &v1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalClusterName,
			Namespace: namespace,
		},
		Spec: v1.ClusterSpec{
			Instances: 3,

			StorageConfiguration: v1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClassName,
			},

			PostgresConfiguration: v1.PostgresConfiguration{
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

			Bootstrap: &v1.BootstrapConfiguration{
				Recovery: &v1.BootstrapRecovery{
					Source: sourceClusterName,
					RecoveryTarget: &v1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},

			ExternalClusters: []v1.ExternalCluster{
				{
					Name: sourceClusterName,
					BarmanObjectStore: &v1.BarmanObjectStoreConfiguration{
						DestinationPath: DestinationPath,
						EndpointCA: &v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "azurite-ca-secret",
							},
							Key: "ca.crt",
						},
						BarmanCredentials: v1.BarmanCredentials{
							Azure: &v1.AzureCredentials{
								ConnectionString: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
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
	cluster, ok := obj.(*v1.Cluster)
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
	clusterName,
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

// GetClusterPrimary gets the primary pod of a cluster
func (env TestingEnvironment) GetClusterPrimary(namespace string, clusterName string) (*corev1.Pod, error) {
	podList := &corev1.PodList{}

	err := GetObjectList(&env, podList, client.InNamespace(namespace),
		client.MatchingLabels{
			utils2.ClusterLabelName:             clusterName,
			utils2.ClusterInstanceRoleLabelName: specs.ClusterRoleLabelPrimary,
		},
	)
	if err != nil {
		return &corev1.Pod{}, err
	}
	if len(podList.Items) > 0 {
		// if there are multiple, get the one without deletion timestamp
		for _, pod := range podList.Items {
			if pod.DeletionTimestamp == nil {
				return &pod, nil
			}
		}
		err = fmt.Errorf("all pod with primary role has deletion timestamp")
		return &(podList.Items[0]), err
	}
	err = fmt.Errorf("no primary found")
	return &corev1.Pod{}, err
}
