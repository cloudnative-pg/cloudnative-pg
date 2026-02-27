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

package persistentvolumeclaim

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// CreateConfiguration specifies how a PVC should be created
type CreateConfiguration struct {
	Status         PVCStatus
	NodeSerial     int
	Calculator     ExpectedObjectCalculator
	TablespaceName string
	Storage        apiv1.StorageConfiguration
	Source         *corev1.TypedLocalObjectReference
}

// Build spec of a PVC, given its name and the storage configuration
// TODO: this logic eventually should be moved inside reconcile
func Build(
	cluster *apiv1.Cluster,
	configuration *CreateConfiguration,
) (*corev1.PersistentVolumeClaim, error) {
	instanceName := specs.GetInstanceName(cluster.Name, configuration.NodeSerial)
	calculator := configuration.Calculator

	var pvcSpec *corev1.PersistentVolumeClaimSpec
	if configuration.Storage.PersistentVolumeClaimTemplate != nil {
		pvcSpec = &configuration.Storage.PersistentVolumeClaimTemplate.PersistentVolumeClaimSpec
	}

	metadataBuilder := resources.NewPersistentVolumeClaimBuilder().
		BeginMetadata().
		WithNamespacedName(calculator.GetName(instanceName), cluster.Namespace)

	// Apply user-defined metadata first so that operator-managed
	// labels and annotations always take precedence on collision.
	if configuration.Storage.PersistentVolumeClaimTemplate != nil {
		metadataBuilder.
			WithLabels(configuration.Storage.PersistentVolumeClaimTemplate.Metadata.Labels).
			WithAnnotations(configuration.Storage.PersistentVolumeClaimTemplate.Metadata.Annotations)
	}

	builder := metadataBuilder.
		WithAnnotations(map[string]string{
			utils.ClusterSerialAnnotationName: strconv.Itoa(configuration.NodeSerial),
			utils.PVCStatusAnnotationName:     configuration.Status,
		}).
		WithLabels(calculator.GetLabels(instanceName)).
		WithClusterInheritance(cluster).
		EndMetadata().
		WithSpec(pvcSpec).
		WithSource(configuration.Source).
		WithDefaultAccessMode(corev1.ReadWriteOnce)

	// If the customer specified a storage class, let's use it
	if configuration.Storage.StorageClass != nil {
		builder = builder.WithStorageClass(configuration.Storage.StorageClass)
	}

	if configuration.Storage.Size != "" {
		// Insert the storage requirement
		parsedSize, err := resource.ParseQuantity(configuration.Storage.Size)
		if err != nil {
			return nil, ErrorInvalidSize
		}
		builder = builder.WithRequests(corev1.ResourceList{
			"storage": parsedSize,
		})
	}

	pvc := builder.Build()

	if pvc.Spec.Resources.Requests.Storage().IsZero() {
		return nil, ErrorInvalidSize
	}

	return pvc, nil
}
