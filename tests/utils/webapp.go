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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// DefaultWebapp returns a struct representing a
func DefaultWebapp(namespace string, name string, rootCASecretName string, tlsSecretName string) corev1.Pod {
	var secretMode int32 = 0o600
	seccompProfile := &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}

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
					Image: "ghcr.io/cloudnative-pg/webtest:1.6.0",
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
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						SeccompProfile:           seccompProfile,
					},
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: seccompProfile,
			},
		},
	}
}
