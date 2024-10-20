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

// Package backup provides backup utilities
package backup

import (
	"context"
	"fmt"

	v1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
)

// GetBackupList gathers the current list of backup in namespace
func GetBackupList(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) (*apiv1.BackupList, error) {
	backupList := &apiv1.BackupList{}
	err := crudClient.List(
		ctx, backupList, client.InNamespace(namespace),
	)
	return backupList, err
}

// CreateBackup creates a Backup resource for a given cluster name
func CreateBackup(
	ctx context.Context,
	crudClient client.Client,
	targetBackup apiv1.Backup,
) (*apiv1.Backup, error) {
	obj, err := objects.CreateObject(ctx, crudClient, &targetBackup)
	if err != nil {
		return nil, err
	}
	backup, ok := obj.(*apiv1.Backup)
	if !ok {
		return nil, fmt.Errorf("created object is not of Backup type: %T %v", obj, obj)
	}
	return backup, nil
}

// GetVolumeSnapshot gets a VolumeSnapshot given name and namespace
func GetVolumeSnapshot(
	ctx context.Context,
	crudClient client.Client,
	namespace, name string,
) (*v1.VolumeSnapshot, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	volumeSnapshot := &v1.VolumeSnapshot{}
	err := objects.GetObject(ctx, crudClient, namespacedName, volumeSnapshot)
	if err != nil {
		return nil, err
	}
	return volumeSnapshot, nil
}
