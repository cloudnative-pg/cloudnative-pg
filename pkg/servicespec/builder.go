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

// Package servicespec contains various utilities to deal with Service Specs
package servicespec

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

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
func (builder *Builder) WithServiceType(serviceType corev1.ServiceType) *Builder {
	builder.status.Spec.Type = serviceType
	return builder
}

// WithPorts adds a port to the current status
func (builder *Builder) WithPorts(port int) *Builder {
	builder.status.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "pgbouncer",
			Protocol:   corev1.ProtocolTCP,
			TargetPort: intstr.FromInt(port),
			Port:       int32(port),
		},
	}
	return builder
}

// WithSelector adds a selector to the current status
func (builder *Builder) WithSelector(name string) *Builder {
	builder.status.Spec.Selector = map[string]string{
		utils.PgbouncerNameLabel: name,
	}

	return builder
}

// WithName adds a name to the current status
func (builder *Builder) WithName(name string) *Builder {
	builder.status.ObjectMeta.Name = name
	return builder
}

// WithNamespace sets a namespace to the current status
func (builder *Builder) WithNamespace(ns string) *Builder {
	builder.status.ObjectMeta.Namespace = ns
	return builder
}

// Build gets the final ServiceTemplate
func (builder *Builder) Build() *apiv1.ServiceTemplateSpec {
	return &builder.status
}
