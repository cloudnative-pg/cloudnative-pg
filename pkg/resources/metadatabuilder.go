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

package resources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ResourceMetadataBuilder creates a fluent abstraction to interact with the kubernetes resources
type ResourceMetadataBuilder[T any] struct {
	objectMeta    *metav1.ObjectMeta
	parentBuilder T
}

// NewResourceMetadataBuilder makes a ResourceMetadataBuilder starting from the object metadata
func NewResourceMetadataBuilder[T any](objectMeta *metav1.ObjectMeta, parentBuilder T) *ResourceMetadataBuilder[T] {
	return &ResourceMetadataBuilder[T]{
		objectMeta:    objectMeta,
		parentBuilder: parentBuilder,
	}
}

// WithNamespacedName adds a namespace and a name to the resource being built
func (builder *ResourceMetadataBuilder[T]) WithNamespacedName(name, namespace string) *ResourceMetadataBuilder[T] {
	builder.objectMeta.Name = name
	builder.objectMeta.Namespace = namespace
	return builder
}

// WithLabels adds labels to the resource being built
func (builder *ResourceMetadataBuilder[T]) WithLabels(maps ...map[string]string) *ResourceMetadataBuilder[T] {
	for _, labels := range maps {
		inheritLabels(builder.objectMeta, labels)
	}
	return builder
}

// WithAnnotations adds annotations to the resource being built
func (builder *ResourceMetadataBuilder[T]) WithAnnotations(maps ...map[string]string) *ResourceMetadataBuilder[T] {
	for _, annotations := range maps {
		inheritAnnotations(builder.objectMeta, annotations)
	}

	return builder
}

// WithOwnership adds ownership to the resource being built
func (builder *ResourceMetadataBuilder[T]) WithOwnership(
	controller metav1.ObjectMeta,
	controllerTypeMeta metav1.TypeMeta,
) *ResourceMetadataBuilder[T] {
	utils.SetAsOwnedBy(builder.objectMeta, controller, controllerTypeMeta)
	return builder
}

// WithHash adds the hash to the resource being built
func (builder *ResourceMetadataBuilder[T]) WithHash(hashValue string) *ResourceMetadataBuilder[T] {
	setHash(builder.objectMeta, hashValue)
	return builder
}

// WithClusterInheritance adds the cluster inherited data and ownership to the object
func (builder *ResourceMetadataBuilder[T]) WithClusterInheritance(cluster *apiv1.Cluster) *ResourceMetadataBuilder[T] {
	cluster.SetInheritedDataAndOwnership(builder.objectMeta)
	return builder
}

// EndMetadata ends the metadata building framework
func (builder *ResourceMetadataBuilder[T]) EndMetadata() T {
	return builder.parentBuilder
}
