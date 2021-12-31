/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultWebapp returns a struct representing a
func DefaultWebapp(namespace string, name string, rootCASecretName string, tlsSecretName string) corev1.Pod {
	var secretMode int32 = 0o600
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "secret-volume-root-ca",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName:  rootCASecretName,
							DefaultMode: &secretMode,
						},
					},
				},
				{
					Name: "secret-volume-tls",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName:  tlsSecretName,
							DefaultMode: &secretMode,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  name,
					Image: "quay.io/leonardoce/webtest:1.3.0",
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 8080,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "secret-volume-root-ca",
							MountPath: "/etc/secrets/ca",
						},
						{
							Name:      "secret-volume-tls",
							MountPath: "/etc/secrets/tls",
						},
					},
				},
			},
		},
	}
}
