/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package specs contains the specification of the K8s resources
// generated by the Cloud Native PostgreSQL operator
package specs

import (
	"fmt"
	"os"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
)

const (
	// ClusterSerialAnnotationName is the name of the annotation containing the
	// serial number of the node
	ClusterSerialAnnotationName = "k8s.enterprisedb.io/nodeSerial"

	// ClusterRoleLabelName label is applied to Pods to mark primary ones
	ClusterRoleLabelName = "role"

	// ClusterRoleLabelPrimary is written in labels to represent primary servers
	ClusterRoleLabelPrimary = "primary"

	// ClusterRoleLabelReplica is written in labels to represent replica servers
	ClusterRoleLabelReplica = "replica"

	// ClusterLabelName label is applied to Pods to link them to the owning
	// cluster
	ClusterLabelName = "postgresql"

	// PostgresContainerName is the name of the container executing PostgreSQL
	// inside one Pod
	PostgresContainerName = "postgres"

	// BootstrapControllerContainerName is the name of the container copying the bootstrap
	// controller inside the Pod file system
	BootstrapControllerContainerName = "bootstrap-controller"

	// PgDataPath is the path to PGDATA variable
	PgDataPath = "/var/lib/postgresql/data/pgdata"
)

func createPostgresVolumes(cluster apiv1.Cluster, podName string) []corev1.Volume {
	result := []corev1.Volume{
		{
			Name: "pgdata",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: podName,
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
			Name: "controller",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "socket",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	if cluster.ShouldCreateApplicationDatabase() {
		result = append(result,
			corev1.Volume{
				Name: "app-secret",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cluster.GetApplicationSecretName(),
					},
				},
			},
		)
	}

	return result
}

// createPostgresContainers create the PostgreSQL containers that are
// used for every instance
func createPostgresContainers(
	cluster apiv1.Cluster,
	podName string,
) []corev1.Container {
	containers := []corev1.Container{
		{
			Name:  PostgresContainerName,
			Image: cluster.GetImageName(),
			Env: []corev1.EnvVar{
				{
					Name:  "PGDATA",
					Value: PgDataPath,
				},
				{
					Name:  "POD_NAME",
					Value: podName,
				},
				{
					Name:  "NAMESPACE",
					Value: cluster.Namespace,
				},
				{
					Name:  "CLUSTER_NAME",
					Value: cluster.Name,
				},
				{
					Name:  "PGPORT",
					Value: "5432",
				},
				{
					Name:  "PGHOST",
					Value: "/var/run/postgresql",
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "pgdata",
					MountPath: "/var/lib/postgresql/data",
				},
				{
					Name:      "controller",
					MountPath: "/controller",
				},
				{
					Name:      "superuser-secret",
					MountPath: "/etc/superuser-secret",
				},
				{
					Name:      "socket",
					MountPath: "/var/run/postgresql",
				},
			},
			ReadinessProbe: &corev1.Probe{
				TimeoutSeconds: 5,
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: url.PathReady,
						Port: intstr.FromInt(url.Port),
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
						Path: url.PathHealth,
						Port: intstr.FromInt(url.Port),
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
				"/controller/manager",
				"instance",
				"run",
				"-pw-file", "/etc/superuser-secret/password",
			},
			Resources: cluster.Spec.Resources,
			Ports: []corev1.ContainerPort{
				{
					ContainerPort: 5432,
					Protocol:      "TCP",
				},
			},
		},
	}

	podDebugActive, err := strconv.ParseBool(os.Getenv("POD_DEBUG"))
	if podDebugActive && err != nil {
		containers[0].Env = append(containers[0].Env, corev1.EnvVar{
			Name:  "DEBUG",
			Value: "1",
		})
	}

	return containers
}

// CreateAffinitySection creates the affinity sections for Pods, given the configuration
// from the user
func CreateAffinitySection(clusterName string, config apiv1.AffinityConfiguration) *corev1.Affinity {
	// We have no anti affinity section if the user don't have it configured
	if config.EnablePodAntiAffinity != nil && !(*config.EnablePodAntiAffinity) {
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
									Key:      ClusterLabelName,
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
func CreatePostgresSecurityContext(postgresUser, postgresGroup int64) *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsUser:  &postgresUser,
		RunAsGroup: &postgresGroup,
		FSGroup:    &postgresGroup,
	}
}

// PodWithExistingStorage create a new instance with an existing storage
func PodWithExistingStorage(cluster apiv1.Cluster, nodeSerial int32) *corev1.Pod {
	podName := fmt.Sprintf("%s-%v", cluster.Name, nodeSerial)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ClusterLabelName: cluster.Name,
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
				createBootstrapContainer(cluster.Spec.Resources),
			},
			Containers:         createPostgresContainers(cluster, podName),
			Volumes:            createPostgresVolumes(cluster, podName),
			SecurityContext:    CreatePostgresSecurityContext(cluster.GetPostgresUID(), cluster.GetPostgresGID()),
			Affinity:           CreateAffinitySection(cluster.Name, cluster.Spec.Affinity),
			ServiceAccountName: cluster.Name,
			NodeSelector:       cluster.Spec.Affinity.NodeSelector,
		},
	}

	return pod
}
