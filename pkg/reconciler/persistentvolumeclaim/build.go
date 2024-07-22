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

package persistentvolumeclaim

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
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
	pvcConfig *CreateConfiguration,
	config *configuration.Data,
) (*corev1.PersistentVolumeClaim, error) {
	instanceName := specs.GetInstanceName(cluster.Name, pvcConfig.NodeSerial)
	calculator := pvcConfig.Calculator
	builder := resources.NewPersistentVolumeClaimBuilder(config).
		BeginMetadata().
		WithNamespacedName(calculator.GetName(instanceName), cluster.Namespace).
		WithAnnotations(map[string]string{
			utils.ClusterSerialAnnotationName: strconv.Itoa(pvcConfig.NodeSerial),
			utils.PVCStatusAnnotationName:     pvcConfig.Status,
		}).
		WithLabels(calculator.GetLabels(instanceName)).
		WithClusterInheritance(cluster).
		EndMetadata().
		WithSpec(pvcConfig.Storage.PersistentVolumeClaimTemplate).
		WithSource(pvcConfig.Source).
		WithDefaultAccessMode(corev1.ReadWriteOnce)

	// If the customer specified a storage class, let's use it
	if pvcConfig.Storage.StorageClass != nil {
		builder = builder.WithStorageClass(pvcConfig.Storage.StorageClass)
	}

	if pvcConfig.Storage.Size != "" {
		// Insert the storage requirement
		parsedSize, err := resource.ParseQuantity(pvcConfig.Storage.Size)
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
