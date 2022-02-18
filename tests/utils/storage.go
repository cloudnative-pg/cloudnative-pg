/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	v1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetStorageAllowExpansion returns the boolean value of the 'AllowVolumeExpansion' value of the storage class
func GetStorageAllowExpansion(defaultStorageClass string, env *TestingEnvironment) (*bool, error) {
	storageClass := &v1.StorageClass{}
	err := env.Client.Get(env.Ctx, client.ObjectKey{Name: defaultStorageClass}, storageClass)
	return storageClass.AllowVolumeExpansion, err
}
