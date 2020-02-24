/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
)

// CreateServiceAccount create the serviceaccount that will be used in every Pod
func CreateServiceAccount(cluster v1alpha1.Cluster) corev1.ServiceAccount {
	var imagePullSecrets []corev1.LocalObjectReference

	if cluster.GetImagePullSecret() != "" {
		imagePullSecrets = append(imagePullSecrets, corev1.LocalObjectReference{
			Name: cluster.GetImagePullSecret(),
		})
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
