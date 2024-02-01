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
}

// Client describes a set of behaviour needed to properly handle all the plugin client expected features
type Client interface {
	Connection
	ClusterCapabilities
	PodCapabilities
	LifecycleCapabilities
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

// PodCapabilities describes a set of behaviour needed to implement the Pod capabilities
type PodCapabilities interface {
	// MutatePod calls the loaded plugins to help to enhance
	// a PostgreSQL instance Pod definition
	MutatePod(
		ctx context.Context,
		cluster client.Object,
		object client.Object,
		mutatedObject client.Object,
	) error
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
