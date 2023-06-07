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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetStorageAllowExpansion returns the boolean value of the 'AllowVolumeExpansion' value of the storage class
func GetStorageAllowExpansion(defaultStorageClass string, env *TestingEnvironment) (*bool, error) {
	storageClass := &storagev1.StorageClass{}
	err := GetObject(env, client.ObjectKey{Name: defaultStorageClass}, storageClass)
	return storageClass.AllowVolumeExpansion, err
}

// IsWalStorageEnabled returns true if 'WalStorage' is being used
func IsWalStorageEnabled(namespace, clusterName string, env *TestingEnvironment) (bool, error) {
	cluster, err := env.GetCluster(namespace, clusterName)
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

// ObjectHasAnnotations returns true if the object has the passed annotations
func ObjectHasAnnotations(
	object client.Object,
	annotations []string,
) bool {
	objectAnnotations := object.GetAnnotations()
	for _, v := range annotations {
		_, ok := objectAnnotations[v]
		if !ok {
			return false
		}
	}
	return true
}

// ObjectMatchesAnnotations returns true if the object has the passed annotations key/value
func ObjectMatchesAnnotations(
	object client.Object,
	annotations map[string]string,
) bool {
	objectAnnotations := object.GetAnnotations()
	for k, v := range annotations {
		value, ok := objectAnnotations[k]
		if !ok && (v != value) {
			return false
		}
	}
	return true
}
