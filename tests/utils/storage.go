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

package utils

import (
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// GetStorageAllowExpansion returns the boolean value of the 'AllowVolumeExpansion' value of the storage class
func GetStorageAllowExpansion(defaultStorageClass string, env *TestingEnvironment) (*bool, error) {
	storageClass := &storagev1.StorageClass{}
	err := GetObject(env, client.ObjectKey{Name: defaultStorageClass}, storageClass)
	return storageClass.AllowVolumeExpansion, err
}

// IsWalStorageEnabled returns true if 'WalStorage' is being used
func IsWalStorageEnabled(namespace, clusterName string, env *TestingEnvironment) (bool, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      clusterName,
	}
	cluster := &apiv1.Cluster{}
	err := env.Client.Get(env.Ctx, namespacedName, cluster)
	if cluster.Spec.WalStorage == nil {
		return false, err
	}
	return true, err
}

// PvcHasLabels returns true if a PVC contains a given map of labels
func PvcHasLabels(
	pvc corev1.PersistentVolumeClaim,
	labels map[string]string,
) bool {
	pvcLabels := pvc.Labels
	for k, v := range labels {
		val, ok := pvcLabels[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// PvcHasAnnotationKeys returns true if a PVC contains a given keys of annotations
func PvcHasAnnotationKeys(
	pvc corev1.PersistentVolumeClaim,
	annotations []string,
) bool {
	pvcAnnotations := pvc.Annotations
	for _, v := range annotations {
		_, ok := pvcAnnotations[v]
		if !ok {
			return false
		}
	}
	return true
}

// PvcHasAnnotation returns true if a PVC contains a given keys and corresponding of annotations
func PvcHasAnnotation(
	pvc corev1.PersistentVolumeClaim,
	annotations map[string]string,
) bool {
	pvcAnnotations := pvc.Annotations
	for k, v := range annotations {
		value, ok := pvcAnnotations[k]
		if !ok && (v != value) {
			return false
		}
	}
	return true
}
