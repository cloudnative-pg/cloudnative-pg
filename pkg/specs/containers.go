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

package specs

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
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
		SecurityContext: CreateContainerSecurityContext(cluster.GetSeccompProfile()),
	}

	addManagerLoggingOptions(cluster, &container)

	return container
}

// addManagerLoggingOptions propagate the logging configuration
// to the manager inside the generated pod.
func addManagerLoggingOptions(cluster apiv1.Cluster, container *corev1.Container) {
	if cluster.Spec.LogLevel != "" {
		container.Command = append(container.Command, fmt.Sprintf("--log-level=%s", cluster.Spec.LogLevel))
	}
	container.Command = append(container.Command, log.GetFieldsRemapFlags()...)
}

// CreateContainerSecurityContext initializes container security context. It applies the seccomp profile if supported.
func CreateContainerSecurityContext(seccompProfile *corev1.SeccompProfile) *corev1.SecurityContext {
	trueValue := true
	falseValue := false

	if !utils.HaveSeccompSupport() {
		seccompProfile = nil
	}

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
		SeccompProfile:           seccompProfile,
	}
}
