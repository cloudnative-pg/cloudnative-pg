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
	apiv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
)

const (
	azuriteImage       = "mcr.microsoft.com/azure-storage/azurite"
	azuriteClientImage = "mcr.microsoft.com/azure-cli"
)

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

// getAzuriteClientPod get the cli client pod
func getAzuriteClientPod(namespace string) corev1.Pod {
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
						AllowPrivilegeEscalation: pointer.Bool(false),
						SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
						RunAsNonRoot:             pointer.Bool(false),
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
				RunAsNonRoot:   pointer.Bool(false),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
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
								AllowPrivilegeEscalation: pointer.Bool(false),
								SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
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
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
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
