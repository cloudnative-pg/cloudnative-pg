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

	"github.com/cloudnative-pg/cnpg-i/pkg/wal"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"go.uber.org/multierr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (data *data) ArchiveWAL(
	ctx context.Context,
	cluster client.Object,
	sourceFileName string,
) error {
	return wrapAsPluginErrorIfNeeded(data.innerArchiveWAL(ctx, cluster, sourceFileName))
}

func (data *data) innerArchiveWAL(
	ctx context.Context,
	cluster client.Object,
	sourceFileName string,
) error {
	contextLogger := log.FromContext(ctx)

	serializedCluster, err := json.Marshal(cluster)
	if err != nil {
		return fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			cluster.GetObjectKind().GroupVersionKind().Kind,
			cluster.GetNamespace(), cluster.GetName(),
			err,
		)
	}

	for idx := range data.plugins {
		plugin := data.plugins[idx]

		if !slices.Contains(plugin.WALCapabilities(), wal.WALCapability_RPC_TYPE_ARCHIVE_WAL) {
			continue
		}

		pluginLogger := contextLogger.WithValues("pluginName", plugin.Name())
		request := wal.WALArchiveRequest{
			ClusterDefinition: serializedCluster,
			SourceFileName:    sourceFileName,
		}

		pluginLogger.Trace(
			"Calling ArchiveWAL endpoint",
			"clusterDefinition", request.ClusterDefinition,
			"sourceFile", request.SourceFileName)
		_, err := plugin.WALClient().Archive(ctx, &request)
		if err != nil {
			pluginLogger.Error(err, "Error while calling ArchiveWAL, failing")
			return err
		}
	}

	return nil
}

func (data *data) RestoreWAL(
	ctx context.Context,
	cluster client.Object,
	sourceWALName string,
	destinationFileName string,
) (bool, error) {
	b, err := data.innerRestoreWAL(ctx, cluster, sourceWALName, destinationFileName)
	return b, wrapAsPluginErrorIfNeeded(err)
}

func (data *data) innerRestoreWAL(
	ctx context.Context,
	cluster client.Object,
	sourceWALName string,
	destinationFileName string,
) (bool, error) {
	var errorCollector error

	contextLogger := log.FromContext(ctx)

	serializedCluster, err := json.Marshal(cluster)
	if err != nil {
		return false, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			cluster.GetObjectKind().GroupVersionKind().Kind,
			cluster.GetNamespace(), cluster.GetName(),
			err,
		)
	}

	for idx := range data.plugins {
		plugin := data.plugins[idx]

		if !slices.Contains(plugin.WALCapabilities(), wal.WALCapability_RPC_TYPE_RESTORE_WAL) {
			continue
		}

		pluginLogger := contextLogger.WithValues("pluginName", plugin.Name())
		request := wal.WALRestoreRequest{
			ClusterDefinition:   serializedCluster,
			SourceWalName:       sourceWALName,
			DestinationFileName: destinationFileName,
		}

		pluginLogger.Trace(
			"Calling RestoreWAL endpoint",
			"clusterDefinition", request.ClusterDefinition,
			"sourceWALName", sourceWALName,
			"destinationFileName", destinationFileName,
		)
		if _, err := plugin.WALClient().Restore(ctx, &request); err != nil {
			pluginLogger.Trace("WAL restore via plugin failed, trying next one", "err", err)
			errorCollector = multierr.Append(errorCollector, err)
		} else {
			return true, nil
		}
	}

	return false, errorCollector
}
