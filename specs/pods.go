/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
)

const (
	// ClusterSerialAnnotationName is the name of the annotation containing the
	// serial number of the node
	ClusterSerialAnnotationName = "postgresql.k8s.2ndq.io/node_serial"
)

// CreateMasterPod create a new mater instance in a Pod
func CreateMasterPod(cluster v1alpha1.Cluster, nodeSerial int32) *corev1.Pod {
	podName := fmt.Sprintf("%s-%v", cluster.Name, nodeSerial)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"postgresql": cluster.Name,
				"role":       "master",
			},
			Annotations: map[string]string{
				ClusterSerialAnnotationName: strconv.Itoa(int(nodeSerial)),
			},
			Name:      podName,
			Namespace: cluster.Namespace,
		},
		Spec: corev1.PodSpec{
			Hostname:  podName,
			Subdomain: cluster.GetServiceAnyName(),
			InitContainers: []corev1.Container{
				{
					Name:  "bootstrap-instance",
					Image: cluster.GetImageName(),
					Env: []corev1.EnvVar{
						{
							Name:  "PGDATA",
							Value: "/var/lib/postgresql/data/pgdata",
						},
					},
					Command: []string{
						"/pgk",
						"init",
						"-pw-file", "/etc/superuser-secret/password",
						"-app-db-name", cluster.Spec.ApplicationConfiguration.Database,
						"-app-user", cluster.Spec.ApplicationConfiguration.Owner,
						"-app-pw-file", "/etc/app-secret/password",
						"-hba-rules-file", "/etc/configuration/postgresHBA",
						"-postgresql-config-file", "/etc/configuration/postgresConfiguration",
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "pgdata",
							MountPath: "/var/lib/postgresql/data",
						},
						{
							Name:      "config",
							MountPath: "/etc/configuration",
						},
						{
							Name:      "superuser-secret",
							MountPath: "/etc/superuser-secret",
						},
						{
							Name:      "app-secret",
							MountPath: "/etc/app-secret",
						},
					},
				},
			},
			Containers:         createPostgresContainers(cluster),
			ImagePullSecrets:   createImagePullSecrets(cluster),
			Volumes:            createPostgresVolumes(cluster, podName),
			Affinity:           CreateAffinitySection(cluster.Name, cluster.Spec.Affinity),
			SecurityContext:    CreatePostgresSecurityContext(),
			ServiceAccountName: cluster.Name,
		},
	}

	return pod
}

func createImagePullSecrets(cluster v1alpha1.Cluster) []corev1.LocalObjectReference {
	var result []corev1.LocalObjectReference

	if len(cluster.GetImagePullSecret()) == 0 {
		return result
	}

	result = append(result, corev1.LocalObjectReference{
		Name: cluster.GetImagePullSecret(),
	})

	return result
}

func createPostgresVolumes(cluster v1alpha1.Cluster, podName string) []corev1.Volume {
	return []corev1.Volume{
		{
			Name:         "pgdata",
			VolumeSource: createVolumeSource(cluster, podName),
		},
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cluster.Name,
					},
				},
			},
		},
		{
			Name: "superuser-secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cluster.GetSuperuserSecretName(),
				},
			},
		},
		{
			Name: "app-secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cluster.GetApplicationSecretName(),
				},
			},
		},
	}
}

// createVolumeSource create the VolumeSource environment that is used
// when starting a container
func createVolumeSource(cluster v1alpha1.Cluster, podName string) corev1.VolumeSource {
	var pgDataVolumeSource corev1.VolumeSource
	if cluster.IsUsingPersistentStorage() {
		pgDataVolumeSource = corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: podName,
			},
		}
	} else {
		pgDataVolumeSource = corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
	}

	return pgDataVolumeSource
}

// createPostgresContainers create the PostgreSQL containers that are
// used for every intance
func createPostgresContainers(cluster v1alpha1.Cluster) []corev1.Container {
	return []corev1.Container{
		{
			Name:  "postgres",
			Image: cluster.GetImageName(),
			Env: []corev1.EnvVar{
				{
					Name:  "PGDATA",
					Value: "/var/lib/postgresql/data/pgdata",
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "pgdata",
					MountPath: "/var/lib/postgresql/data",
				},
			},
			ReadinessProbe: &corev1.Probe{
				TimeoutSeconds: 5,
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/readyz",
						Port: intstr.FromInt(8000),
					},
				},
			},
			// From K8s 1.17 and newer, startup probes will be available for
			// all users and not just protected from feature gates. For now
			// let's use the LivenessProbe. When we will drop support for K8s
			// 1.16, we'll configure a StartupProbe and this will lead to a
			// better LivenessProbe (without InitialDelaySeconds).
			LivenessProbe: &corev1.Probe{
				InitialDelaySeconds: cluster.GetMaxStartDelay(),
				TimeoutSeconds:      5,
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz",
						Port: intstr.FromInt(8000),
					},
				},
			},
			Lifecycle: &corev1.Lifecycle{
				PreStop: &corev1.Handler{
					Exec: &corev1.ExecAction{
						Command: []string{
							"pg_ctl",
							"stop",
							"-m",
							"smart",
							"-t",
							strconv.Itoa(int(cluster.GetMaxStopDelay())),
						},
					},
				},
			},
			Command: []string{
				"/pgk",
				"run",
				"-app-db-name", cluster.Spec.ApplicationConfiguration.Database,
			},
			Resources: cluster.Spec.Resources,
		},
	}
}

// CreateAffinitySection creates the affinity sections for Pods, given the configuration
// from the user
func CreateAffinitySection(clusterName string, config v1alpha1.AffinityConfiguration) *corev1.Affinity {
	// We have no anti affinity section if the user don't have it configured
	if !config.EnablePodAntiAffinity {
		return nil
	}

	topologyKey := config.TopologyKey
	if len(topologyKey) == 0 {
		topologyKey = "kubernetes.io/hostname"
	}

	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "postgresql",
									Operator: metav1.LabelSelectorOpIn,
									Values: []string{
										clusterName,
									},
								},
							},
						},
						TopologyKey: topologyKey,
					},
				},
			},
		},
	}
}

// CreatePostgresSecurityContext defines the security context under which
// the PostgreSQL containers are running
func CreatePostgresSecurityContext() *corev1.PodSecurityContext {
	postgresUser := int64(999)
	postgresGroup := int64(999)

	return &corev1.PodSecurityContext{
		RunAsUser:  &postgresUser,
		RunAsGroup: &postgresGroup,
		FSGroup:    &postgresGroup,
	}
}

// JoinReplicaInstance create a new PostgreSQL node, copying the contents from another Pod
func JoinReplicaInstance(cluster v1alpha1.Cluster, nodeSerial int32) *corev1.Pod {
	podName := fmt.Sprintf("%s-%v", cluster.Name, nodeSerial)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"postgresql": cluster.Name,
			},
			Annotations: map[string]string{
				ClusterSerialAnnotationName: strconv.Itoa(int(nodeSerial)),
			},
			Name:      podName,
			Namespace: cluster.Namespace,
		},
		Spec: corev1.PodSpec{
			Hostname:  podName,
			Subdomain: cluster.GetServiceAnyName(),
			InitContainers: []corev1.Container{
				{
					Name:  "bootstrap-replica",
					Image: cluster.GetImageName(),
					Env: []corev1.EnvVar{
						{
							Name:  "PGDATA",
							Value: "/var/lib/postgresql/data/pgdata",
						},
					},
					Command: []string{
						"/pgk",
						"join",
						"-parent-node", cluster.GetServiceReadWriteName(),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "pgdata",
							MountPath: "/var/lib/postgresql/data",
						},
						{
							Name:      "config",
							MountPath: "/etc/configuration",
						},
						{
							Name:      "superuser-secret",
							MountPath: "/etc/superuser-secret",
						},
						{
							Name:      "app-secret",
							MountPath: "/etc/app-secret",
						},
					},
				},
			},
			Containers:         createPostgresContainers(cluster),
			ImagePullSecrets:   createImagePullSecrets(cluster),
			Volumes:            createPostgresVolumes(cluster, podName),
			Affinity:           CreateAffinitySection(cluster.Name, cluster.Spec.Affinity),
			SecurityContext:    CreatePostgresSecurityContext(),
			ServiceAccountName: cluster.Name,
		},
	}

	return pod
}
