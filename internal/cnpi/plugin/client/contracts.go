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

	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin"
)

// Metadata expose the metadata as discovered
// from a plugin
type Metadata struct {
	Name                 string
	Version              string
	Capabilities         []string
	OperatorCapabilities []string
	WALCapabilities      []string
	BackupCapabilities   []string
}

// Loader describes a struct capable of generating a plugin Client
type Loader interface {
	// LoadPluginClient creates a new plugin client, loading the plugins that are required
	// by this cluster
	LoadPluginClient(ctx context.Context) (Client, error)
}

// Client describes a set of behaviour needed to properly handle all the plugin client expected features
type Client interface {
	Connection
	ClusterCapabilities
	ClusterReconcilerHooks
	LifecycleCapabilities
	WalCapabilities
	BackupCapabilities
}

// Connection describes a set of behaviour needed to properly handle the plugin connections
type Connection interface {
	// Load connect to the plugin with the specified name
	Load(ctx context.Context, name string) error

	// Close closes the connection to every loaded plugin
	Close(ctx context.Context)

	// MetadataList exposes the metadata of the loaded plugins
	MetadataList() []Metadata
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
		oldObject client.Object,
		newObject client.Object,
	) (field.ErrorList, error)
}

// ReconcilerHookResult is the result of a reconciliation loop
type ReconcilerHookResult struct {
	Result             ctrl.Result
	Err                error
	StopReconciliation bool
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
	// This call is a no-op if there's no plugin implementing WAL archiving
	RestoreWAL(
		ctx context.Context,
		cluster client.Object,
		sourceWALName string,
		destinationFileName string,
	) error
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
