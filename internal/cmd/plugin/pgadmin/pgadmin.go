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

package pgadmin

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"text/template"

	"github.com/sethvargo/go-password/password"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

var configurationTemplate = template.Must(template.New("servers.json").Parse(`
{
	"Servers": {
		"1": {
			"Name": "{{ .ClusterName }}",
			"Group": "Servers",
			"Host": "{{ .ClusterName }}-rw",
			"Port": 5432,
			"MaintenanceDB": "{{ .ApplicationDatabaseOwnerName }}",
			"Username": "{{ .ApplicationDatabaseOwnerName }}",
			"UseSSHTunnel": 0,
			"TunnelPort": "22",
			"TunnelAuthentication": 0,
			"KerberosAuthentication": false,
			{{ if eq .Mode "desktop" }}
			"PasswordExecCommand": "cat /secret/password",
			{{ end }}
			"ConnectionParameters": {
				"sslmode": "prefer",
				"connect_timeout": 10,
				"sslcert": "<STORAGE_DIR>/.postgresql/postgresql.crt",
				"sslkey": "<STORAGE_DIR>/.postgresql/postgresql.key"
			}
		}
	}
}
`))

// Mode is the current pgadmin mode
type Mode string

const (
	// ModeServer means server mode; this is password protected
	// with no secrets included
	ModeServer = Mode("server")

	// ModeDesktop means desktop mode; this is not password protected,
	// and the `app` secret is mounted within the Pod
	ModeDesktop = Mode("desktop")
)

type command struct {
	ClusterName                   string
	ApplicationDatabaseSecretName string
	ApplicationDatabaseOwnerName  string

	DeploymentName  string
	ConfigMapName   string
	ServiceName     string
	SecretName      string
	PgadminUsername string
	PgadminPassword string
	Mode            Mode
	PgadminImage    string

	dryRun bool
}

// newCommand initialize pgadmin deployment options
func newCommand(
	cluster *apiv1.Cluster,
	mode Mode,
	dryRun bool,
	pgadminImage string,
) (*command, error) {
	const defaultPgadminUsername = "user@pgadmin.com"

	clusterName := cluster.Name
	result := &command{
		ClusterName:                   clusterName,
		ApplicationDatabaseSecretName: cluster.GetApplicationSecretName(),
		ApplicationDatabaseOwnerName:  cluster.GetApplicationDatabaseOwner(),
		dryRun:                        dryRun,
		DeploymentName:                fmt.Sprintf("%s-pgadmin4", clusterName),
		ConfigMapName:                 fmt.Sprintf("%s-pgadmin4", clusterName),
		ServiceName:                   fmt.Sprintf("%s-pgadmin4", clusterName),
		SecretName:                    fmt.Sprintf("%s-pgadmin4", clusterName),
		Mode:                          mode,
		PgadminImage:                  pgadminImage,
	}

	pgadminPassword, err := password.Generate(32, 10, 0, false, true)
	if err != nil {
		return nil, err
	}

	result.PgadminPassword = pgadminPassword
	result.PgadminUsername = defaultPgadminUsername

	return result, nil
}

func (cmd *command) execute(ctx context.Context) error {
	configMap, err := cmd.generateConfigMap()
	if err != nil {
		return err
	}
	deployment := cmd.generateDeployment()
	service := cmd.generateService()
	secret := cmd.generateSecret()

	objectList := []client.Object{configMap, deployment, service, secret}
	return plugin.CreateAndGenerateObjects(ctx, objectList, cmd.dryRun)
}

func (cmd *command) generateSecret() *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmd.SecretName,
			Namespace: plugin.Namespace,
		},
		StringData: map[string]string{
			"username": cmd.PgadminUsername,
			"password": cmd.PgadminPassword,
		},
	}
}

func (cmd *command) generateConfigMap() (*corev1.ConfigMap, error) {
	buffer := new(bytes.Buffer)
	if err := configurationTemplate.Execute(buffer, cmd); err != nil {
		return nil, err
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmd.ConfigMapName,
			Namespace: plugin.Namespace,
		},
		Data: map[string]string{
			"servers.json": buffer.String(),
		},
	}, nil
}

func (cmd *command) generateDeployment() *appsv1.Deployment {
	const (
		pgAdminCfgVolumeName      = "pgadmin-cfg"
		pgAdminCfgVolumePath      = "/config"
		pgAdminPassFileVolumeName = "app-secret"
		pgAdminPassFileVolumePath = "/secret"
	)

	serverMode := "True"
	if cmd.Mode == ModeDesktop {
		serverMode = "False"
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmd.DeploymentName,
			Namespace: plugin.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": cmd.DeploymentName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": cmd.DeploymentName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: cmd.PgadminImage,
							Name:  "pgadmin4",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 80,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "PGADMIN_SERVER_JSON_FILE",
									Value: path.Join(pgAdminCfgVolumePath, "servers.json"),
								},
								{
									Name:  "PGADMIN_CONFIG_SERVER_MODE",
									Value: serverMode,
								},
								{
									Name:  "PGADMIN_CONFIG_MASTER_PASSWORD_REQUIRED",
									Value: "False",
								},
								{
									Name: "PGADMIN_DEFAULT_EMAIL",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											Key: "username",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: cmd.SecretName,
											},
										},
									},
								},
								{
									Name: "PGADMIN_DEFAULT_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											Key: "password",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: cmd.SecretName,
											},
										},
									},
								},
								{
									Name:  "PGADMIN_DISABLE_POSTFIX",
									Value: "True",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      pgAdminCfgVolumeName,
									MountPath: pgAdminCfgVolumePath,
								},
								{
									Name:      pgAdminPassFileVolumeName,
									MountPath: pgAdminPassFileVolumePath,
								},
								{
									Name:      "tmp",
									MountPath: "/tmp",
								},
								{
									Name:      "home",
									MountPath: "/home/pgadmin",
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt32(80),
									},
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: pgAdminCfgVolumeName,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: cmd.ConfigMapName,
									},
								},
							},
						},
						{
							Name: pgAdminPassFileVolumeName,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: cmd.ApplicationDatabaseSecretName,
								},
							},
						},
						{
							Name: "home",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									Medium:    corev1.StorageMediumMemory,
									SizeLimit: ptr.To(resource.MustParse("100Mi")),
								},
							},
						},
					},
				},
			},
		},
	}
}

func (cmd *command) generateService() *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmd.ServiceName,
			Namespace: plugin.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       80,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt32(80),
				},
			},
			Selector: map[string]string{
				"app": cmd.DeploymentName,
			},
		},
	}
}
