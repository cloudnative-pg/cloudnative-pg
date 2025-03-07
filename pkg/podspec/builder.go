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

// Package podspec contains various utilities to deal with Pod Specs
package podspec

import (
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// Builder enables to user to create a PodTemplate starting from a baseline
// and adding patches
type Builder struct {
	status apiv1.PodTemplateSpec
}

// New creates a new empty podTemplate builder
func New() *Builder {
	return NewFrom(nil)
}

// NewFrom creates a podTemplate builder from a certain Pod template
func NewFrom(podTemplate *apiv1.PodTemplateSpec) *Builder {
	if podTemplate == nil {
		podTemplate = &apiv1.PodTemplateSpec{}
	}
	return &Builder{
		status: *podTemplate,
	}
}

// WithAnnotation adds an annotation to the current status
func (builder *Builder) WithAnnotation(name, value string) *Builder {
	if builder.status.ObjectMeta.Annotations == nil {
		builder.status.ObjectMeta.Annotations = make(map[string]string)
	}

	builder.status.ObjectMeta.Annotations[name] = value

	return builder
}

// WithLabel adds a label to the current status
func (builder *Builder) WithLabel(name, value string) *Builder {
	if builder.status.ObjectMeta.Labels == nil {
		builder.status.ObjectMeta.Labels = make(map[string]string)
	}

	builder.status.ObjectMeta.Labels[name] = value

	return builder
}

// WithVolume adds a volume to the current podTemplate, replacing the current
// definition if present
func (builder *Builder) WithVolume(volume *corev1.Volume) *Builder {
	for idx, value := range builder.status.Spec.Volumes {
		if value.Name == volume.Name {
			builder.status.Spec.Volumes[idx] = *volume
			return builder
		}
	}

	builder.status.Spec.Volumes = append(builder.status.Spec.Volumes, *volume)
	return builder
}

// WithSecurityContext adds a securityContext to the current podTemplate is nil.
// If `overwrite` is true the securityContext is overwritten even when it's not empty
func (builder *Builder) WithSecurityContext(
	securityCtx *corev1.PodSecurityContext,
	overwrite bool,
) *Builder {
	if overwrite || builder.status.Spec.SecurityContext == nil {
		builder.status.Spec.SecurityContext = securityCtx
	}
	return builder
}

// WithContainer ensures that in the current status there is a container
// with the passed name
func (builder *Builder) WithContainer(name string) *Builder {
	for _, value := range builder.status.Spec.Containers {
		if value.Name == name {
			return builder
		}
	}

	builder.status.Spec.Containers = append(builder.status.Spec.Containers,
		corev1.Container{
			Name: name,
		})
	return builder
}

// WithContainerImage ensures that, if in the current status there is
// a container with the passed name and the image is empty, the image will be
// set to the one passed.
// If `overwrite` is true the image is overwritten even when it's not empty
func (builder *Builder) WithContainerImage(name, image string, overwrite bool) *Builder {
	builder.WithContainer(name)

	for idx, value := range builder.status.Spec.Containers {
		if value.Name == name {
			if overwrite || value.Image == "" {
				builder.status.Spec.Containers[idx].Image = image
			}
		}
	}

	return builder
}

// WithContainerVolumeMount ensure that the passed the volume mount exist in
// the current status, overriding the present one when needed
func (builder *Builder) WithContainerVolumeMount(
	name string, volumeMount *corev1.VolumeMount, overwrite bool,
) *Builder {
	builder.WithContainer(name)

	for idxContainer, container := range builder.status.Spec.Containers {
		if container.Name == name {
			for idxMount, mount := range container.VolumeMounts {
				if mount.Name == volumeMount.Name {
					if overwrite {
						builder.status.Spec.Containers[idxContainer].VolumeMounts[idxMount] = *volumeMount
					}
					return builder
				}
			}

			builder.status.Spec.Containers[idxContainer].VolumeMounts = append(
				builder.status.Spec.Containers[idxContainer].VolumeMounts,
				*volumeMount)
		}
	}

	return builder
}

// WithContainerEnv add the provided EnvVar to a container
func (builder *Builder) WithContainerEnv(name string, env corev1.EnvVar, overwrite bool) *Builder {
	builder.WithContainer(name)

	for idxContainer, container := range builder.status.Spec.Containers {
		if container.Name == name {
			for idx, envVar := range container.Env {
				if envVar.Name == env.Name {
					if overwrite {
						builder.status.Spec.Containers[idxContainer].Env[idx] = env
					}
					return builder
				}
			}

			builder.status.Spec.Containers[idxContainer].Env = append(builder.status.Spec.Containers[idxContainer].Env, env)
			return builder
		}
	}

	return builder
}

// WithServiceAccountName add the provided ServiceAccountName
func (builder *Builder) WithServiceAccountName(name string, overwrite bool) *Builder {
	if builder.status.Spec.ServiceAccountName == name || !overwrite {
		return builder
	}

	builder.status.Spec.ServiceAccountName = name

	return builder
}

// WithLivenessProbe add the provided liveness probe to a container
func (builder *Builder) WithLivenessProbe(name string, livenessProbe *corev1.Probe, overwrite bool) *Builder {
	builder.WithContainer(name)

	for idxContainer, container := range builder.status.Spec.Containers {
		if container.Name == name {
			if container.LivenessProbe == nil || overwrite {
				builder.status.Spec.Containers[idxContainer].LivenessProbe = livenessProbe
			}
			return builder
		}
	}

	return builder
}

// WithReadinessProbe add the provided readiness probe to a container
func (builder *Builder) WithReadinessProbe(name string, readinessProbe *corev1.Probe, overwrite bool) *Builder {
	builder.WithContainer(name)

	for idxContainer, container := range builder.status.Spec.Containers {
		if container.Name == name {
			if container.ReadinessProbe == nil || overwrite {
				builder.status.Spec.Containers[idxContainer].ReadinessProbe = readinessProbe
			}
			return builder
		}
	}

	return builder
}

// WithContainerCommand ensures that, if in the current status there is
// a container with the passed name and the command is empty, the command will be
// set to the one passed.
// If `overwrite` is true the command is overwritten even when it's not empty
func (builder *Builder) WithContainerCommand(name string, command []string, overwrite bool) *Builder {
	builder.WithContainer(name)

	for idx, value := range builder.status.Spec.Containers {
		if value.Name == name {
			if overwrite || len(value.Command) == 0 {
				builder.status.Spec.Containers[idx].Command = command
			}
		}
	}

	return builder
}

// WithContainerPort ensures that, if in the current status there is
// a container with the passed name the passed container port will be
// added, possibly overriding the one already present with the same name
func (builder *Builder) WithContainerPort(name string, value *corev1.ContainerPort) *Builder {
	builder.WithContainer(name)

	for idxContainer, container := range builder.status.Spec.Containers {
		if container.Name == name {
			for idxPort, port := range container.Ports {
				if port.Name == value.Name {
					builder.status.Spec.Containers[idxContainer].Ports[idxPort] = *value
					return builder
				}
			}

			builder.status.Spec.Containers[idxContainer].Ports = append(
				builder.status.Spec.Containers[idxContainer].Ports,
				*value)
		}
	}

	return builder
}

// WithContainerSecurityContext ensures that, if in the current status there is
// a container with the passed name and the securityContext is empty, the securityContext will be
// set to the one passed.
// If `overwrite` is true the command is overwritten even when it's not empty
func (builder *Builder) WithContainerSecurityContext(
	name string,
	ctx *corev1.SecurityContext,
	overwrite bool,
) *Builder {
	builder.WithContainer(name)

	for idx, value := range builder.status.Spec.Containers {
		if value.Name == name {
			if overwrite || value.SecurityContext == nil {
				builder.status.Spec.Containers[idx].SecurityContext = ctx
			}
		}
	}

	return builder
}

// WithInitContainer ensures that in the current status there is an init container
// with the passed name
func (builder *Builder) WithInitContainer(name string) *Builder {
	for _, value := range builder.status.Spec.InitContainers {
		if value.Name == name {
			return builder
		}
	}

	builder.status.Spec.InitContainers = append(builder.status.Spec.InitContainers,
		corev1.Container{
			Name: name,
		})
	return builder
}

// WithInitContainerImage ensures that, if in the current status there is
// an init container with the passed name and the image is empty, the image will be
// set to the one passed.
// If `overwrite` is true the image is overwritten even when it's not empty
func (builder *Builder) WithInitContainerImage(name, image string, overwrite bool) *Builder {
	builder.WithInitContainer(name)

	for idx, value := range builder.status.Spec.InitContainers {
		if value.Name == name {
			if overwrite || value.Image == "" {
				builder.status.Spec.InitContainers[idx].Image = image
			}
		}
	}

	return builder
}

// WithInitContainerVolumeMount ensure that the passed the volume mount exist in
// the current status, overriding the present one when needed
func (builder *Builder) WithInitContainerVolumeMount(
	name string, volumeMount *corev1.VolumeMount, overwrite bool,
) *Builder {
	builder.WithInitContainer(name)

	for idxContainer, container := range builder.status.Spec.InitContainers {
		if container.Name == name {
			for idxMount, mount := range container.VolumeMounts {
				if mount.Name == volumeMount.Name {
					if overwrite {
						builder.status.Spec.InitContainers[idxContainer].VolumeMounts[idxMount] = *volumeMount
					}
					return builder
				}
			}

			builder.status.Spec.InitContainers[idxContainer].VolumeMounts = append(
				builder.status.Spec.InitContainers[idxContainer].VolumeMounts,
				*volumeMount)
		}
	}

	return builder
}

func (builder *Builder) WithInitContainerResources(
	name string,
	resources corev1.ResourceRequirements,
) *Builder {
	builder.WithInitContainer(name)

	for idx, value := range builder.status.Spec.InitContainers {
		if value.Name == name {
			builder.status.Spec.InitContainers[idx].Resources = resources
		}
	}

	return builder
}

// WithInitContainerCommand ensures that, if in the current status there is
// an init container with the passed name and the command is empty, the command will be
// set to the one passed.
// If `overwrite` is true the command is overwritten even when it's not empty
func (builder *Builder) WithInitContainerCommand(name string, command []string, overwrite bool) *Builder {
	builder.WithInitContainer(name)

	for idx, value := range builder.status.Spec.InitContainers {
		if value.Name == name {
			if overwrite || len(value.Command) == 0 {
				builder.status.Spec.InitContainers[idx].Command = command
			}
		}
	}

	return builder
}

// WithInitContainerSecurityContext ensures that, if in the current status there is
// an init container with the passed name and the securityContext is empty, the securityContext will be
// set to the one passed.
// If `overwrite` is true the securityContext is overwritten even when it's not empty
func (builder *Builder) WithInitContainerSecurityContext(
	name string,
	ctx *corev1.SecurityContext,
	overwrite bool,
) *Builder {
	builder.WithInitContainer(name)

	for idx, value := range builder.status.Spec.InitContainers {
		if value.Name == name {
			if overwrite || value.SecurityContext == nil {
				builder.status.Spec.InitContainers[idx].SecurityContext = ctx
			}
		}
	}
	return builder
}

// Build gets the final Pod template
func (builder *Builder) Build() *apiv1.PodTemplateSpec {
	return &builder.status
}
