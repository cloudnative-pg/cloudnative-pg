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

package client

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	restore "github.com/cloudnative-pg/cnpg-i/pkg/restore/job"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// ErrNoPluginSupportsRestoreJobHooksCapability is raised when no plugin supports the restore job hooks capability
var ErrNoPluginSupportsRestoreJobHooksCapability = errors.New("no plugin supports the restore job hooks capability")

func (data *data) Restore(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) (*restore.RestoreResponse, error) {
	backup.EnsureGVKIsPresent()
	cluster.EnsureGVKIsPresent()

	for idx := range data.plugins {
		plugin := data.plugins[idx]

		if !slices.Contains(plugin.RestoreJobHooksCapabilities(), restore.RestoreJobHooksCapability_KIND_RESTORE) {
			continue
		}

		clusterDefinition, err := json.Marshal(cluster)
		if err != nil {
			return nil, err
		}
		backupDefinition, err := json.Marshal(backup)
		if err != nil {
			return nil, err
		}
		request := restore.RestoreRequest{
			ClusterDefinition: clusterDefinition,
			BackupDefinition:  backupDefinition,
		}
		res, err := plugin.RestoreJobHooksClient().Restore(ctx, &request)
		if err != nil {
			return nil, err
		}
		return res, nil
	}

	return nil, ErrNoPluginSupportsRestoreJobHooksCapability
}
