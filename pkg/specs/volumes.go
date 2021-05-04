/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
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
			Name: "scratch-data",
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

func createPostgresVolumeMounts(cluster apiv1.Cluster) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "pgdata",
			MountPath: "/var/lib/postgresql/data",
		},
		{
			Name:      "scratch-data",
			MountPath: "/run",
		},
		{
			Name:      "superuser-secret",
			MountPath: "/etc/superuser-secret",
		},
		{
			Name:      "scratch-data",
			MountPath: "/controller",
		},
	}

	if cluster.ShouldCreateApplicationDatabase() {
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "app-secret",
				MountPath: "/etc/app-secret",
			},
		)
	}

	return volumeMounts
}
