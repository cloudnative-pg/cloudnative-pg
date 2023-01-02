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

package pvc

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Create create spec of a PVC, given its name and the storage configuration
// TODO: this logic eventually should be moved inside reconcile
func Create(
	storageConfiguration apiv1.StorageConfiguration,
	cluster apiv1.Cluster,
	nodeSerial int,
	role utils.PVCRole,
) (*corev1.PersistentVolumeClaim, error) {
	instanceName := specs.GetInstanceName(cluster.Name, nodeSerial)
	pvcName := GetPVCName(cluster, instanceName, role)

	result := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				utils.InstanceNameLabelName: instanceName,
				utils.PvcRoleLabelName:      string(role),
			},
			Annotations: map[string]string{
				specs.ClusterSerialAnnotationName: strconv.Itoa(nodeSerial),
				StatusAnnotationName:              StatusInitializing,
			},
		},
	}

	// If the customer supplied a spec, let's use it
	if storageConfiguration.PersistentVolumeClaimTemplate != nil {
		storageConfiguration.PersistentVolumeClaimTemplate.DeepCopyInto(&result.Spec)
	}

	// If the customer specified a storage class, let's use it
	if storageConfiguration.StorageClass != nil {
		result.Spec.StorageClassName = storageConfiguration.StorageClass
	}

	if storageConfiguration.Size != "" {
		// Insert the storage requirement
		parsedSize, err := resource.ParseQuantity(storageConfiguration.Size)
		if err != nil {
			return nil, ErrorInvalidSize
		}

		result.Spec.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				"storage": parsedSize,
			},
		}
	}

	if len(result.Spec.AccessModes) == 0 {
		result.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		}
	}

	if result.Spec.Resources.Requests.Storage().IsZero() {
		return nil, ErrorInvalidSize
	}

	return result, nil
}
