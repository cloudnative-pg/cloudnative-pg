/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
)

// createBootstrapContainer creates the init container bootstrapping the operator
// executable inside the generated Pods
func createBootstrapContainer(
	resources corev1.ResourceRequirements,
	postgresUser,
	postgresGroup int64,
) corev1.Container {
	return corev1.Container{
		Name:  BootstrapControllerContainerName,
		Image: configuration.Current.OperatorImageName,
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
		Resources:       resources,
		SecurityContext: CreateContainerSecurityContext(postgresUser, postgresGroup),
	}
}

// CreateContainerSecurityContext initializes container security context
func CreateContainerSecurityContext(postgresUser, postgresGroup int64) *corev1.SecurityContext {
	trueValue := true
	falseValue := false

	return &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				"ALL",
			},
		},
		Privileged:               &falseValue,
		RunAsNonRoot:             &trueValue,
		ReadOnlyRootFilesystem:   &falseValue, // TODO set to true in CNP-293
		AllowPrivilegeEscalation: &falseValue,
	}
}
