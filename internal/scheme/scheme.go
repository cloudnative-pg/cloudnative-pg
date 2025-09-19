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

package scheme

import (
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// Builder contains the fluent methods to build a schema
type Builder struct {
	scheme *runtime.Scheme
}

// New creates a new builder
func New() *Builder {
	return &Builder{scheme: runtime.NewScheme()}
}

// WithClientGoScheme adds the kubernetes/scheme
func (b *Builder) WithClientGoScheme() *Builder {
	_ = clientgoscheme.AddToScheme(b.scheme)

	return b
}

// WithAPIV1 adds the v1 scheme
func (b *Builder) WithAPIV1() *Builder {
	_ = apiv1.AddToScheme(b.scheme)

	return b
}

// WithMonitoringV1 adds prometheus-operator scheme
func (b *Builder) WithMonitoringV1() *Builder {
	_ = monitoringv1.AddToScheme(b.scheme)

	return b
}

// WithAPIExtensionV1 adds apiextensions/v1
func (b *Builder) WithAPIExtensionV1() *Builder {
	_ = apiextensionsv1.AddToScheme(b.scheme)

	return b
}

// WithVolumeSnapshotV1 adds volumesnapshotv1
func (b *Builder) WithVolumeSnapshotV1() *Builder {
	_ = volumesnapshotv1.AddToScheme(b.scheme)

	return b
}

// Build returns the built scheme
func (b *Builder) Build() *runtime.Scheme {
	return b.scheme
}

// BuildWithAllKnownScheme registers all the API used by the manager
func BuildWithAllKnownScheme() *runtime.Scheme {
	return New().
		WithAPIV1().
		WithClientGoScheme().
		WithMonitoringV1().
		WithAPIExtensionV1().
		WithVolumeSnapshotV1().
		Build()

	// +kubebuilder:scaffold:scheme
}
