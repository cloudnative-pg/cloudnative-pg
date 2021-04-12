/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
)

// createBootstrapContainer create the init container bootstrapping the operator
// executable inside the generated Pods
func createBootstrapContainer(resources corev1.ResourceRequirements) corev1.Container {
	return corev1.Container{
		Name:  BootstrapControllerContainerName,
		Image: configuration.GetOperatorImageName(),
		Command: []string{
			"/manager",
			"bootstrap",
			"/controller/manager",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "controller",
				MountPath: "/controller",
			},
		},
		Resources: resources,
	}
}
