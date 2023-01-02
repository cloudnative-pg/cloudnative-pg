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

// CreateConfiguration specifies how a PVC should be created
type CreateConfiguration struct {
	Status     Status
	NodeSerial int
	Role       utils.PVCRole
	Storage    apiv1.StorageConfiguration
}

// Create spec of a PVC, given its name and the storage configuration
// TODO: this logic eventually should be moved inside reconcile
func Create(
	cluster apiv1.Cluster,
	configuration *CreateConfiguration,
) (*corev1.PersistentVolumeClaim, error) {
	instanceName := specs.GetInstanceName(cluster.Name, configuration.NodeSerial)
	pvcName := GetPVCName(cluster, instanceName, configuration.Role)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				utils.InstanceNameLabelName: instanceName,
				utils.PvcRoleLabelName:      string(configuration.Role),
			},
			Annotations: map[string]string{
				specs.ClusterSerialAnnotationName: strconv.Itoa(configuration.NodeSerial),
				StatusAnnotationName:              configuration.Status,
			},
		},
	}

	// If the customer supplied a spec, let's use it
	if configuration.Storage.PersistentVolumeClaimTemplate != nil {
		configuration.Storage.PersistentVolumeClaimTemplate.DeepCopyInto(&pvc.Spec)
	}

	// If the customer specified a storage class, let's use it
	if configuration.Storage.StorageClass != nil {
		pvc.Spec.StorageClassName = configuration.Storage.StorageClass
	}

	if configuration.Storage.Size != "" {
		// Insert the storage requirement
		parsedSize, err := resource.ParseQuantity(configuration.Storage.Size)
		if err != nil {
			return nil, ErrorInvalidSize
		}

		pvc.Spec.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				"storage": parsedSize,
			},
		}
	}

	if len(pvc.Spec.AccessModes) == 0 {
		pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		}
	}

	if pvc.Spec.Resources.Requests.Storage().IsZero() {
		return nil, ErrorInvalidSize
	}

	return pvc, nil
}
