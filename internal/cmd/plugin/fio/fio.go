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

// Package fio implements the kubectl-cnpg fio sub-command
package fio

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

type fioCommand struct {
	name             string
	storageClassName string
	pvcSize          string
	fioCommandArgs   []string
	dryRun           bool
}

const (
	fioKeyWord = "fio"
	fioImage   = "docker.io/wallnerryan/fiotools-aio:v2"
)

var jobExample = `
  # Dry-run command with default values"
  kubectl-cnpg fio <fio-name> --dry-run

  # Create a fio job with default values.
  kubectl-cnpg fio <fio-name>

  # Dry-run command with given values and clusterName "cluster-example"
  kubectl-cnpg fio <fio-name> -n <namespace> --storageClass <name> --pvcSize <size> --dry-run

  # Create a job with given values and clusterName "cluster-example"
  kubectl-cnpg fio <fio-name> -n <namespace> --storageClass <name> --pvcSize <size>
`

// newFioCommand initialize fio deployment options
func newFioCommand(
	name string,
	storageClassName string,
	pvcSize string,
	dryRun bool,
	fioCommandArgs []string,
) *fioCommand {
	fioArgs := &fioCommand{
		name:             name,
		storageClassName: storageClassName,
		dryRun:           dryRun,
		fioCommandArgs:   fioCommandArgs,
		pvcSize:          pvcSize,
	}
	return fioArgs
}

func (cmd *fioCommand) execute(ctx context.Context) error {
	pvc, err := cmd.generatePVCObject()
	if err != nil {
		return err
	}
	configMap := cmd.generateConfigMapObject()
	deployment := cmd.generateFioDeployment(cmd.name)
	objectList := []client.Object{pvc, configMap, deployment}

	return plugin.CreateAndGenerateObjects(ctx, objectList, cmd.dryRun)
}

// CreatePVC creates spec of a PVC, given its name and the storage configuration
func (cmd *fioCommand) generatePVCObject() (*corev1.PersistentVolumeClaim, error) {
	result := &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmd.name,
			Namespace: plugin.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "",
		},
	}

	if cmd.pvcSize != "" {
		parsedSize, err := resource.ParseQuantity(cmd.pvcSize)
		if err != nil {
			return nil, err
		}

		result.Spec.Resources = corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				"storage": parsedSize,
			},
		}
	}

	if cmd.storageClassName != "" {
		result.Spec.StorageClassName = &cmd.storageClassName
	}
	return result, nil
}

// createConfigMap creates spec of configmap.
func (cmd *fioCommand) generateConfigMapObject() *corev1.ConfigMap {
	result := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmd.name,
			Namespace: plugin.Namespace,
		},
		Data: map[string]string{
			"job": `[read]
    direct=1
    bs=8k
    size=1G
    time_based=1
    runtime=60
    ioengine=libaio
    iodepth=32
    end_fsync=1
    log_avg_msec=1000
    directory=/data
    rw=read
    write_bw_log=read
    write_lat_log=read
    write_iops_log=read`,
		},
	}
	return result
}

func getSecurityContext() *corev1.SecurityContext {
	runAs := int64(10001)
	sc := &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		RunAsNonRoot:             ptr.To(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				"ALL",
			},
		},
		ReadOnlyRootFilesystem: ptr.To(true),
	}
	if utils.HaveSecurityContextConstraints() {
		return sc
	}

	sc.RunAsUser = &runAs
	sc.RunAsGroup = &runAs
	sc.SeccompProfile = &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}

	return sc
}

func getPodSecurityContext() *corev1.PodSecurityContext {
	if utils.HaveSecurityContextConstraints() {
		return &corev1.PodSecurityContext{}
	}
	runAs := int64(10001)
	return &corev1.PodSecurityContext{
		FSGroup: &runAs,
	}
}

// createFioDeployment creates spec of deployment.
func (cmd *fioCommand) generateFioDeployment(deploymentName string) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: plugin.Namespace,
		},

		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     fioKeyWord,
					"app.kubernetes.io/instance": deploymentName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":     fioKeyWord,
						"app.kubernetes.io/instance": deploymentName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  fioKeyWord,
							Image: fioImage,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8000,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "JOBFILES",
									Value: "/job/job.fio",
								},
								{
									Name:  "PLOTNAME",
									Value: "job",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
								},
								{
									Name:      "job",
									MountPath: "/job",
								},
								{
									Name:      "tmp",
									MountPath: "/tmp/fio-data",
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.IntOrString{
											IntVal: 8000,
										},
									},
								},
								InitialDelaySeconds: 60,
								PeriodSeconds:       10,
							},
							SecurityContext: getSecurityContext(),
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"memory": resource.MustParse("100M"),
									"cpu":    resource.MustParse("1"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: deploymentName,
								},
							},
						},
						{
							Name: "job",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: deploymentName,
									},
									Items: []corev1.KeyToPath{
										{
											Key:  "job",
											Path: "job.fio",
										},
									},
								},
							},
						},
						{
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									TopologyKey: "kubernetes.io/hostname",
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "app",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{"fio"},
											},
										},
									},
								},
							},
						},
					},
					NodeSelector:    map[string]string{},
					SecurityContext: getPodSecurityContext(),
				},
			},
		},
	}
}
