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

	restore "github.com/cloudnative-pg/cnpg-i/pkg/restore/job"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"
	contextutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils/context"
)

// Client describes a set of behaviour needed to properly handle all the plugin client expected features
type Client interface {
	Connection
	ClusterCapabilities
	ClusterReconcilerHooks
	LifecycleCapabilities
	WalCapabilities
	BackupCapabilities
	RestoreJobHooksCapabilities
	PostgresConfigurationCapabilities
	MetricsCapabilities
}

// SetPluginClientInContext records the plugin client in the given context
func SetPluginClientInContext(ctx context.Context, client Client) context.Context {
	return context.WithValue(ctx, contextutils.PluginClientKey, client)
}

// GetPluginClientFromContext gets the current plugin client from the context
func GetPluginClientFromContext(ctx context.Context) Client {
	v := ctx.Value(contextutils.PluginClientKey)
	if v == nil {
		return nil
	}

	cli, ok := v.(Client)
	if !ok {
		return nil
	}

	return cli
}

// Connection describes a set of behaviour needed to properly handle the plugin connections
type Connection interface {
	// Close closes the connection to every loaded plugin
	Close(ctx context.Context)

	// MetadataList exposes the metadata of the loaded plugins
	MetadataList() []connection.Metadata

	HasPlugin(pluginName string) bool
}

// ClusterCapabilities describes a set of behaviour needed to implement the Cluster capabilities
type ClusterCapabilities interface {
	// MutateCluster calls the loaded plugisn to help to enhance
	// a cluster definition
	MutateCluster(
		ctx context.Context,
		object client.Object,
		mutatedObject client.Object,
	) error

	// ValidateClusterCreate calls all the loaded plugin to check if a cluster definition
	// is correct
	ValidateClusterCreate(
		ctx context.Context,
		object client.Object,
	) (field.ErrorList, error)

	// ValidateClusterUpdate calls all the loaded plugin to check if a cluster can
	// be changed from a value to another
	ValidateClusterUpdate(
		ctx context.Context,
		cluster client.Object,
		mutatedCluster client.Object,
	) (field.ErrorList, error)

	// SetStatusInCluster returns a map of [pluginName]: statuses to be assigned to the cluster
	SetStatusInCluster(ctx context.Context, cluster client.Object) (map[string]string, error)
}

// ReconcilerHookResult is the result of a reconciliation loop
type ReconcilerHookResult struct {
	Result             ctrl.Result `json:"result"`
	Err                error       `json:"err"`
	StopReconciliation bool        `json:"stopReconciliation"`
	Identifier         string      `json:"identifier"`
}

// ClusterReconcilerHooks decsribes a set of behavior needed to enhance
// the login of the Cluster reconcicliation loop
type ClusterReconcilerHooks interface {
	// PreReconcile is executed after we get the resources and update the status
	PreReconcile(
		ctx context.Context,
		cluster client.Object,
		object client.Object,
	) ReconcilerHookResult

	// PostReconcile is executed at the end of the reconciliation loop
	PostReconcile(
		ctx context.Context,
		cluster client.Object,
		object client.Object,
	) ReconcilerHookResult
}

// LifecycleCapabilities describes a set of behaviour needed to implement the Lifecycle capabilities
type LifecycleCapabilities interface {
	// LifecycleHook notifies the registered plugins of a given event for a given object
	LifecycleHook(
		ctx context.Context,
		operationVerb plugin.OperationVerb,
		cluster client.Object,
		object client.Object,
	) (client.Object, error)
}

// WalCapabilities describes a set of behavior needed to archive and recover WALs
type WalCapabilities interface {
	// ArchiveWAL calls the loaded plugins to archive a WAL file.
	// This call is a no-op if there's no plugin implementing WAL archiving
	ArchiveWAL(
		ctx context.Context,
		cluster client.Object,
		sourceFileName string,
	) error

	// RestoreWAL calls the loaded plugins to archive a WAL file.
	// This call returns a boolean indicating if the WAL was restored
	// by a plugin and the occurred error.
	RestoreWAL(
		ctx context.Context,
		cluster client.Object,
		sourceWALName string,
		destinationFileName string,
	) (bool, error)
}

// BackupCapabilities describes a set of behaviour needed to backup
// a PostgreSQL cluster
type BackupCapabilities interface {
	// Backup takes a backup via a cnpg-i plugin
	Backup(
		ctx context.Context,
		cluster client.Object,
		backupObject client.Object,
		pluginName string,
		parameters map[string]string,
	) (*BackupResponse, error)
}

// RestoreJobHooksCapabilities describes a set of behaviour needed to run the Restore
type RestoreJobHooksCapabilities interface {
	Restore(ctx context.Context, cluster gvkEnsurer) (*restore.RestoreResponse, error)
}
