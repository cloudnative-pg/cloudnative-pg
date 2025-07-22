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

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/cloudnative-pg/cnpg-i/pkg/backup"
	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BackupResponse is the status of a newly created backup. This is used as a return
// type for the Backup RPC Call
type BackupResponse struct {
	// This field contains a machine-readable ID of the
	// backup that is being taken
	BackupID string

	// This field contains a human-readable name of the
	// backup that is being taken
	BackupName string

	// This field contains the timestamp of the start
	// time of the backup
	StartedAt time.Time

	// This field contains the Unix timestamp of the end
	// time of the backup
	StoppedAt time.Time

	// This field contains the current WAL when the backup was started
	BeginWal string

	// This field contains the current WAL at the end of the backup
	EndWal string

	// This field contains the current LSN record when the backup was started
	BeginLsn string

	// This field contains the current LSN record when the backup has finished
	EndLsn string

	// This field contains the backup label of the backup that have been taken
	BackupLabelFile []byte

	// This field contains the tablespace map of the backup that have been taken
	TablespaceMapFile []byte

	// This field contains the ID of the instance that have been backed up
	InstanceID string

	// This field is set to true for online/hot backups and to false otherwise.
	Online bool

	// This field contains the metadata to be associated with this backup
	Metadata map[string]string
}

func (data *data) Backup(
	ctx context.Context,
	cluster client.Object,
	backupObject client.Object,
	pluginName string,
	parameters map[string]string,
) (*BackupResponse, error) {
	b, err := data.innerBackup(ctx, cluster, backupObject, pluginName, parameters)
	return b, wrapAsPluginErrorIfNeeded(err)
}

func (data *data) innerBackup(
	ctx context.Context,
	cluster client.Object,
	backupObject client.Object,
	pluginName string,
	parameters map[string]string,
) (*BackupResponse, error) {
	contextLogger := log.FromContext(ctx)

	serializedCluster, err := json.Marshal(cluster)
	if err != nil {
		return nil, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			cluster.GetObjectKind().GroupVersionKind().Kind,
			cluster.GetNamespace(), cluster.GetName(),
			err,
		)
	}

	serializedBackup, err := json.Marshal(backupObject)
	if err != nil {
		return nil, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			backupObject.GetObjectKind().GroupVersionKind().Kind,
			backupObject.GetNamespace(), backupObject.GetName(),
			err,
		)
	}

	plugin, err := data.getPlugin(pluginName)
	if err != nil {
		return nil, err
	}

	if !slices.Contains(plugin.PluginCapabilities(), identity.PluginCapability_Service_TYPE_BACKUP_SERVICE) {
		return nil, ErrPluginNotSupportBackup
	}

	if !slices.Contains(plugin.BackupCapabilities(), backup.BackupCapability_RPC_TYPE_BACKUP) {
		return nil, ErrPluginNotSupportBackupEndpoint
	}

	contextLogger = contextLogger.WithValues(
		"pluginName", pluginName,
	)

	request := backup.BackupRequest{
		ClusterDefinition: serializedCluster,
		BackupDefinition:  serializedBackup,
		Parameters:        parameters,
	}

	contextLogger.Trace(
		"Calling Backup endpoint",
		"clusterDefinition", request.ClusterDefinition,
		"parameters", parameters)

	result, err := plugin.BackupClient().Backup(ctx, &request)
	if err != nil {
		contextLogger.Error(err, "Error while calling Backup, failing")
		return nil, err
	}

	return &BackupResponse{
		BackupID:          result.BackupId,
		BackupName:        result.BackupName,
		StartedAt:         time.Unix(result.StartedAt, 0),
		StoppedAt:         time.Unix(result.StoppedAt, 0),
		BeginWal:          result.BeginWal,
		EndWal:            result.EndWal,
		BeginLsn:          result.BeginLsn,
		EndLsn:            result.EndLsn,
		BackupLabelFile:   result.BackupLabelFile,
		TablespaceMapFile: result.TablespaceMapFile,
		InstanceID:        result.InstanceId,
		Online:            result.Online,
		Metadata:          result.Metadata,
	}, nil
}
