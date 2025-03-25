/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

// Package servicespec contains various utilities to deal with Service Specs
package servicespec

import (
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Builder enables users to create a serviceTemplate starting from a baseline
// and adding patches
type Builder struct {
	status apiv1.ServiceTemplateSpec
}

// New creates a new empty serviceTemplate builder
func New() *Builder {
	return NewFrom(nil)
}

// NewFrom creates a serviceTemplate builder from a certain Service template
func NewFrom(serviceTemplate *apiv1.ServiceTemplateSpec) *Builder {
	if serviceTemplate == nil {
		serviceTemplate = &apiv1.ServiceTemplateSpec{}
	}
	return &Builder{
		status: *serviceTemplate,
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

// WithServiceType adds a service type to the current status
func (builder *Builder) WithServiceType(serviceType corev1.ServiceType, overwrite bool) *Builder {
	if overwrite || builder.status.Spec.Type == "" {
		builder.status.Spec.Type = serviceType
	}
	return builder
}

// WithServicePort adds a port to the current service
func (builder *Builder) WithServicePort(value *corev1.ServicePort) *Builder {
	for idx, port := range builder.status.Spec.Ports {
		if port.Name == value.Name {
			builder.status.Spec.Ports[idx] = *value
			return builder
		}
	}

	builder.status.Spec.Ports = append(builder.status.Spec.Ports, *value)
	return builder
}

// WithServicePortNoOverwrite adds a ServicePort to the current service if no ServicePort that matches the name
// or port value is found
func (builder *Builder) WithServicePortNoOverwrite(value *corev1.ServicePort) *Builder {
	for _, port := range builder.status.Spec.Ports {
		if port.Name == value.Name || port.Port == value.Port {
			return builder
		}
	}

	return builder.WithServicePort(value)
}

// SetPGBouncerSelector overwrites the selectors field with the PgbouncerNameLabel selector.
func (builder *Builder) SetPGBouncerSelector(name string) *Builder {
	return builder.SetSelectors(map[string]string{utils.PgbouncerNameLabel: name})
}

// SetSelectors overwrites the selector fields
func (builder *Builder) SetSelectors(selectors map[string]string) *Builder {
	builder.status.Spec.Selector = selectors
	return builder
}

// Build gets the final ServiceTemplate
func (builder *Builder) Build() *apiv1.ServiceTemplateSpec {
	return &builder.status
}
