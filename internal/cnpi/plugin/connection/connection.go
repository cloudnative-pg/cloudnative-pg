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

package connection

import (
	"context"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/cloudnative-pg/cnpg-i/pkg/backup"
	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	"github.com/cloudnative-pg/cnpg-i/pkg/metrics"
	"github.com/cloudnative-pg/cnpg-i/pkg/operator"
	postgresClient "github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	restore "github.com/cloudnative-pg/cnpg-i/pkg/restore/job"
	"github.com/cloudnative-pg/cnpg-i/pkg/wal"
	"google.golang.org/grpc"
)

// defaultNetworkCallTimeout is the timeout applied by default to every GRPC
// call to a plugin in a different Pod
const defaultNetworkCallTimeout = 30 * time.Second

// Protocol represents a way to connect to a plugin
type Protocol interface {
	Dial(ctx context.Context) (Handler, error)
}

// Handler represent a plugin connection
type Handler interface {
	grpc.ClientConnInterface
	io.Closer
}

// Interface exposes the methods that allow the user to access to the features
// of a plugin
type Interface interface {
	Name() string
	Metadata() Metadata

	LifecycleClient() lifecycle.OperatorLifecycleClient
	OperatorClient() operator.OperatorClient
	WALClient() wal.WALClient
	BackupClient() backup.BackupClient
	ReconcilerHooksClient() reconciler.ReconcilerHooksClient
	RestoreJobHooksClient() restore.RestoreJobHooksClient
	PostgresClient() postgresClient.PostgresClient
	MetricsClient() metrics.MetricsClient

	PluginCapabilities() []identity.PluginCapability_Service_Type
	OperatorCapabilities() []operator.OperatorCapability_RPC_Type
	WALCapabilities() []wal.WALCapability_RPC_Type
	LifecycleCapabilities() []*lifecycle.OperatorLifecycleCapabilities
	BackupCapabilities() []backup.BackupCapability_RPC_Type
	ReconcilerCapabilities() []reconciler.ReconcilerHooksCapability_Kind
	RestoreJobHooksCapabilities() []restore.RestoreJobHooksCapability_Kind
	PostgresCapabilities() []postgresClient.PostgresCapability_RPC_Type
	MetricsCapabilities() []metrics.MetricsCapability_RPC_Type

	Ping(ctx context.Context) error
	Close() error
}

type data struct {
	connection            Handler
	identityClient        identity.IdentityClient
	operatorClient        operator.OperatorClient
	lifecycleClient       lifecycle.OperatorLifecycleClient
	walClient             wal.WALClient
	backupClient          backup.BackupClient
	reconcilerHooksClient reconciler.ReconcilerHooksClient
	restoreJobHooksClient restore.RestoreJobHooksClient
	postgresClient        postgresClient.PostgresClient
	metricsClient         metrics.MetricsClient

	name                        string
	version                     string
	capabilities                []identity.PluginCapability_Service_Type
	operatorCapabilities        []operator.OperatorCapability_RPC_Type
	walCapabilities             []wal.WALCapability_RPC_Type
	lifecycleCapabilities       []*lifecycle.OperatorLifecycleCapabilities
	backupCapabilities          []backup.BackupCapability_RPC_Type
	reconcilerCapabilities      []reconciler.ReconcilerHooksCapability_Kind
	restoreJobHooksCapabilities []restore.RestoreJobHooksCapability_Kind
	postgresCapabilities        []postgresClient.PostgresCapability_RPC_Type
	metricsCapabilities         []metrics.MetricsCapability_RPC_Type
}

func newPluginDataFromConnection(ctx context.Context, connection Handler) (data, error) {
	var err error

	identityClient := identity.NewIdentityClient(connection)

	var pluginInfoResponse *identity.GetPluginMetadataResponse

	if pluginInfoResponse, err = identityClient.GetPluginMetadata(
		ctx,
		&identity.GetPluginMetadataRequest{},
	); err != nil {
		return data{}, fmt.Errorf("while querying plugin identity: %w", err)
	}

	result := data{
		connection:            connection,
		name:                  pluginInfoResponse.Name,
		version:               pluginInfoResponse.Version,
		identityClient:        identity.NewIdentityClient(connection),
		operatorClient:        operator.NewOperatorClient(connection),
		lifecycleClient:       lifecycle.NewOperatorLifecycleClient(connection),
		walClient:             wal.NewWALClient(connection),
		backupClient:          backup.NewBackupClient(connection),
		reconcilerHooksClient: reconciler.NewReconcilerHooksClient(connection),
		restoreJobHooksClient: restore.NewRestoreJobHooksClient(connection),
		postgresClient:        postgresClient.NewPostgresClient(connection),
		metricsClient:         metrics.NewMetricsClient(connection),
	}

	return result, err
}

func (pluginData *data) loadPluginCapabilities(ctx context.Context) error {
	var pluginCapabilitiesResponse *identity.GetPluginCapabilitiesResponse
	var err error

	if pluginCapabilitiesResponse, err = pluginData.identityClient.GetPluginCapabilities(
		ctx,
		&identity.GetPluginCapabilitiesRequest{},
	); err != nil {
		return fmt.Errorf("while querying plugin capabilities: %w", err)
	}

	pluginData.capabilities = make([]identity.PluginCapability_Service_Type, len(pluginCapabilitiesResponse.Capabilities))
	for i := range pluginData.capabilities {
		pluginData.capabilities[i] = pluginCapabilitiesResponse.Capabilities[i].GetService().Type
	}

	return nil
}

func (pluginData *data) loadOperatorCapabilities(ctx context.Context) error {
	var operatorCapabilitiesResponse *operator.OperatorCapabilitiesResult
	var err error

	if operatorCapabilitiesResponse, err = pluginData.operatorClient.GetCapabilities(
		ctx,
		&operator.OperatorCapabilitiesRequest{},
	); err != nil {
		return fmt.Errorf("while querying plugin operator capabilities: %w", err)
	}

	pluginData.operatorCapabilities = make(
		[]operator.OperatorCapability_RPC_Type,
		len(operatorCapabilitiesResponse.Capabilities))
	for i := range pluginData.operatorCapabilities {
		pluginData.operatorCapabilities[i] = operatorCapabilitiesResponse.Capabilities[i].GetRpc().Type
	}

	return nil
}

func (pluginData *data) loadLifecycleCapabilities(ctx context.Context) error {
	var lifecycleCapabilitiesResponse *lifecycle.OperatorLifecycleCapabilitiesResponse
	var err error
	if lifecycleCapabilitiesResponse, err = pluginData.lifecycleClient.GetCapabilities(
		ctx,
		&lifecycle.OperatorLifecycleCapabilitiesRequest{},
	); err != nil {
		return fmt.Errorf("while querying plugin lifecycle capabilities: %w", err)
	}

	pluginData.lifecycleCapabilities = lifecycleCapabilitiesResponse.LifecycleCapabilities
	return nil
}

func (pluginData *data) loadReconcilerHooksCapabilities(ctx context.Context) error {
	var reconcilerHooksCapabilitiesResult *reconciler.ReconcilerHooksCapabilitiesResult
	var err error
	if reconcilerHooksCapabilitiesResult, err = pluginData.reconcilerHooksClient.GetCapabilities(
		ctx,
		&reconciler.ReconcilerHooksCapabilitiesRequest{},
	); err != nil {
		return fmt.Errorf("while querying plugin lifecycle capabilities: %w", err)
	}

	pluginData.reconcilerCapabilities = make(
		[]reconciler.ReconcilerHooksCapability_Kind,
		len(reconcilerHooksCapabilitiesResult.ReconcilerCapabilities))

	for i := range pluginData.reconcilerCapabilities {
		pluginData.reconcilerCapabilities[i] = reconcilerHooksCapabilitiesResult.ReconcilerCapabilities[i].Kind
	}
	return nil
}

func (pluginData *data) loadWALCapabilities(ctx context.Context) error {
	var walCapabilitiesResponse *wal.WALCapabilitiesResult
	var err error

	if walCapabilitiesResponse, err = pluginData.walClient.GetCapabilities(
		ctx,
		&wal.WALCapabilitiesRequest{},
	); err != nil {
		return fmt.Errorf("while querying plugin operator capabilities: %w", err)
	}

	pluginData.walCapabilities = make(
		[]wal.WALCapability_RPC_Type,
		len(walCapabilitiesResponse.Capabilities))
	for i := range pluginData.walCapabilities {
		pluginData.walCapabilities[i] = walCapabilitiesResponse.Capabilities[i].GetRpc().Type
	}

	return nil
}

func (pluginData *data) loadBackupCapabilities(ctx context.Context) error {
	var backupCapabilitiesResponse *backup.BackupCapabilitiesResult
	var err error

	if backupCapabilitiesResponse, err = pluginData.backupClient.GetCapabilities(
		ctx,
		&backup.BackupCapabilitiesRequest{},
	); err != nil {
		return fmt.Errorf("while querying plugin operator capabilities: %w", err)
	}

	pluginData.backupCapabilities = make(
		[]backup.BackupCapability_RPC_Type,
		len(backupCapabilitiesResponse.Capabilities))
	for i := range pluginData.backupCapabilities {
		pluginData.backupCapabilities[i] = backupCapabilitiesResponse.Capabilities[i].GetRpc().Type
	}

	return nil
}

func (pluginData *data) loadRestoreJobHooksCapabilities(ctx context.Context) error {
	var restoreJobHooksCapabilitiesResponse *restore.RestoreJobHooksCapabilitiesResult
	var err error

	if restoreJobHooksCapabilitiesResponse, err = pluginData.restoreJobHooksClient.GetCapabilities(
		ctx,
		&restore.RestoreJobHooksCapabilitiesRequest{},
	); err != nil {
		return fmt.Errorf("while querying plugin operator capabilities: %w", err)
	}

	pluginData.restoreJobHooksCapabilities = make(
		[]restore.RestoreJobHooksCapability_Kind,
		len(restoreJobHooksCapabilitiesResponse.Capabilities))
	for i := range pluginData.restoreJobHooksCapabilities {
		pluginData.restoreJobHooksCapabilities[i] = restoreJobHooksCapabilitiesResponse.Capabilities[i].Kind
	}

	return nil
}

func (pluginData *data) loadPostgresCapabilities(ctx context.Context) error {
	var postgresCapabilitiesResponse *postgresClient.PostgresCapabilitiesResult
	var err error

	if postgresCapabilitiesResponse, err = pluginData.postgresClient.GetCapabilities(
		ctx,
		&postgresClient.PostgresCapabilitiesRequest{},
	); err != nil {
		return fmt.Errorf("while querying plugin operator capabilities: %w", err)
	}

	pluginData.postgresCapabilities = make(
		[]postgresClient.PostgresCapability_RPC_Type,
		len(postgresCapabilitiesResponse.Capabilities))
	for i := range pluginData.postgresCapabilities {
		pluginData.postgresCapabilities[i] = postgresCapabilitiesResponse.Capabilities[i].GetRpc().Type
	}

	return nil
}

func (pluginData *data) loadMetricsCapabilities(ctx context.Context) error {
	var metricsCapabilitiesResponse *metrics.MetricsCapabilitiesResult
	var err error

	if metricsCapabilitiesResponse, err = pluginData.metricsClient.GetCapabilities(
		ctx,
		&metrics.MetricsCapabilitiesRequest{},
	); err != nil {
		return fmt.Errorf("while querying plugin metrics capabilities: %w", err)
	}

	pluginData.metricsCapabilities = make(
		[]metrics.MetricsCapability_RPC_Type,
		len(metricsCapabilitiesResponse.Capabilities))
	for i := range pluginData.metricsCapabilities {
		pluginData.metricsCapabilities[i] = metricsCapabilitiesResponse.Capabilities[i].GetRpc().Type
	}

	return nil
}

// Metadata extracts the plugin metadata reading from
// the internal metadata
func (pluginData *data) Metadata() Metadata {
	result := Metadata{
		Name:                       pluginData.name,
		Version:                    pluginData.version,
		Capabilities:               make([]string, len(pluginData.capabilities)),
		OperatorCapabilities:       make([]string, len(pluginData.operatorCapabilities)),
		WALCapabilities:            make([]string, len(pluginData.walCapabilities)),
		BackupCapabilities:         make([]string, len(pluginData.backupCapabilities)),
		RestoreJobHookCapabilities: make([]string, len(pluginData.restoreJobHooksCapabilities)),
		PostgresCapabilities:       make([]string, len(pluginData.postgresCapabilities)),
	}

	for i := range pluginData.capabilities {
		result.Capabilities[i] = pluginData.capabilities[i].String()
	}

	for i := range pluginData.operatorCapabilities {
		result.OperatorCapabilities[i] = pluginData.operatorCapabilities[i].String()
	}

	for i := range pluginData.walCapabilities {
		result.WALCapabilities[i] = pluginData.walCapabilities[i].String()
	}

	for i := range pluginData.backupCapabilities {
		result.BackupCapabilities[i] = pluginData.backupCapabilities[i].String()
	}

	for i := range pluginData.restoreJobHooksCapabilities {
		result.RestoreJobHookCapabilities[i] = pluginData.restoreJobHooksCapabilities[i].String()
	}

	return result
}

func (pluginData *data) Name() string {
	return pluginData.name
}

// Close closes the connection to the plugin.
func (pluginData *data) Close() error {
	return pluginData.connection.Close()
}

func (pluginData *data) LifecycleClient() lifecycle.OperatorLifecycleClient {
	return pluginData.lifecycleClient
}

func (pluginData *data) OperatorClient() operator.OperatorClient {
	return pluginData.operatorClient
}

func (pluginData *data) WALClient() wal.WALClient {
	return pluginData.walClient
}

func (pluginData *data) BackupClient() backup.BackupClient {
	return pluginData.backupClient
}

func (pluginData *data) RestoreJobHooksClient() restore.RestoreJobHooksClient {
	return pluginData.restoreJobHooksClient
}

func (pluginData *data) ReconcilerHooksClient() reconciler.ReconcilerHooksClient {
	return pluginData.reconcilerHooksClient
}

func (pluginData *data) PostgresClient() postgresClient.PostgresClient {
	return pluginData.postgresClient
}

func (pluginData *data) MetricsClient() metrics.MetricsClient {
	return pluginData.metricsClient
}

func (pluginData *data) PluginCapabilities() []identity.PluginCapability_Service_Type {
	return pluginData.capabilities
}

func (pluginData *data) OperatorCapabilities() []operator.OperatorCapability_RPC_Type {
	return pluginData.operatorCapabilities
}

func (pluginData *data) WALCapabilities() []wal.WALCapability_RPC_Type {
	return pluginData.walCapabilities
}

func (pluginData *data) LifecycleCapabilities() []*lifecycle.OperatorLifecycleCapabilities {
	return pluginData.lifecycleCapabilities
}

func (pluginData *data) BackupCapabilities() []backup.BackupCapability_RPC_Type {
	return pluginData.backupCapabilities
}

func (pluginData *data) ReconcilerCapabilities() []reconciler.ReconcilerHooksCapability_Kind {
	return pluginData.reconcilerCapabilities
}

func (pluginData *data) RestoreJobHooksCapabilities() []restore.RestoreJobHooksCapability_Kind {
	return pluginData.restoreJobHooksCapabilities
}

func (pluginData *data) PostgresCapabilities() []postgresClient.PostgresCapability_RPC_Type {
	return pluginData.postgresCapabilities
}

func (pluginData *data) MetricsCapabilities() []metrics.MetricsCapability_RPC_Type {
	return pluginData.metricsCapabilities
}

func (pluginData *data) Ping(ctx context.Context) error {
	_, err := pluginData.identityClient.Probe(ctx, &identity.ProbeRequest{})
	return err
}

// LoadPlugin loads the plugin connected over a certain collections,
// queries the metadata and prepares an active plugin connection interface
func LoadPlugin(ctx context.Context, handler Handler) (Interface, error) {
	result, err := newPluginDataFromConnection(ctx, handler)
	if err != nil {
		return nil, err
	}

	// Load the list of services implemented by the plugin
	if err = result.loadPluginCapabilities(ctx); err != nil {
		return nil, err
	}

	// If the plugin implements the Operator service, load its
	// capabilities
	if slices.Contains(result.capabilities, identity.PluginCapability_Service_TYPE_OPERATOR_SERVICE) {
		if err = result.loadOperatorCapabilities(ctx); err != nil {
			return nil, err
		}
	}

	// If the plugin implements the lifecycle service, load its
	// capabilities
	if slices.Contains(result.capabilities, identity.PluginCapability_Service_TYPE_LIFECYCLE_SERVICE) {
		if err = result.loadLifecycleCapabilities(ctx); err != nil {
			return nil, err
		}
	}

	// If the plugin implements the WAL service, load its
	// capabilities
	if slices.Contains(result.capabilities, identity.PluginCapability_Service_TYPE_WAL_SERVICE) {
		if err = result.loadWALCapabilities(ctx); err != nil {
			return nil, err
		}
	}

	// If the plugin implements the backup service, load its
	// capabilities
	if slices.Contains(result.capabilities, identity.PluginCapability_Service_TYPE_BACKUP_SERVICE) {
		if err = result.loadBackupCapabilities(ctx); err != nil {
			return nil, err
		}
	}

	// If the plugin implements the reconciler hooks, load its
	// capabilities
	if slices.Contains(result.capabilities, identity.PluginCapability_Service_TYPE_RECONCILER_HOOKS) {
		if err = result.loadReconcilerHooksCapabilities(ctx); err != nil {
			return nil, err
		}
	}

	// If the plugin implements the restore job hooks, load its
	// capabilities
	if slices.Contains(result.capabilities, identity.PluginCapability_Service_TYPE_RESTORE_JOB) {
		if err = result.loadRestoreJobHooksCapabilities(ctx); err != nil {
			return nil, err
		}
	}

	// If the plugin implements the postgres service, load its
	// capabilities
	if slices.Contains(result.capabilities, identity.PluginCapability_Service_TYPE_POSTGRES) {
		if err = result.loadPostgresCapabilities(ctx); err != nil {
			return nil, err
		}
	}

	// If the plugin implements the metrics service, load its
	// capabilities
	if slices.Contains(result.capabilities, identity.PluginCapability_Service_TYPE_METRICS) {
		if err = result.loadMetricsCapabilities(ctx); err != nil {
			return nil, err
		}
	}

	return &result, nil
}
