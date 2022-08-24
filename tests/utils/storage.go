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
