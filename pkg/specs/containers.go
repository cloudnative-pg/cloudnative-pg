/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
)

// createBootstrapContainer creates the init container bootstrapping the operator
// executable inside the generated Pods
func createBootstrapContainer(cluster apiv1.Cluster) corev1.Container {
	container := corev1.Container{
		Name:            BootstrapControllerContainerName,
		Image:           configuration.Current.OperatorImageName,
		ImagePullPolicy: cluster.Spec.ImagePullPolicy,
		Command: []string{
			"/manager",
			"bootstrap",
			"/controller/manager",
		},
		VolumeMounts:    createPostgresVolumeMounts(cluster),
		Resources:       cluster.Spec.Resources,
		SecurityContext: CreateContainerSecurityContext(),
	}

	addManagerLoggingOptions(&container)

	return container
}

// addManagerLoggingOptions propagate the logging configuration
// to the manager inside the generated pod.
func addManagerLoggingOptions(container *corev1.Container) {
	if configuration.Current.EnablePodDebugging {
		container.Command = append(container.Command, "--zap-log-level=4")
	}
}

// CreateContainerSecurityContext initializes container security context
func CreateContainerSecurityContext() *corev1.SecurityContext {
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
		ReadOnlyRootFilesystem:   &trueValue,
		AllowPrivilegeEscalation: &falseValue,
	}
}
