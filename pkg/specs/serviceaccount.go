/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateServiceAccount create the ServiceAccount that will be used in every Pod
func CreateServiceAccount(cluster metav1.ObjectMeta, imagePullSecretsNames []string) corev1.ServiceAccount {
	imagePullSecrets := make([]corev1.LocalObjectReference, len(imagePullSecretsNames))

	for idx, name := range imagePullSecretsNames {
		imagePullSecrets[idx] = corev1.LocalObjectReference{Name: name}
	}

	serviceAccount := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		ImagePullSecrets: imagePullSecrets,
	}

	return serviceAccount
}
