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
	"fmt"
	"io"
	"path"
	"slices"
	"time"

	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"github.com/cloudnative-pg/cnpg-i/pkg/operator"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/timeout"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// defaultTimeout is the timeout applied by default to every GRPC call
const defaultTimeout = 30 * time.Second

type protocol interface {
	dial(ctx context.Context, path string) (connectionHandler, error)
}

type connectionHandler interface {
	grpc.ClientConnInterface
	io.Closer
}

type protocolUnix string

func (p protocolUnix) dial(ctx context.Context, path string) (connectionHandler, error) {
	contextLogger := log.FromContext(ctx)
	dialPath := fmt.Sprintf("unix://%s", path)

	contextLogger.Debug("Connecting to plugin", "path", dialPath)

	return grpc.Dial(
		dialPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			timeout.UnaryClientInterceptor(defaultTimeout),
		),
	)
}

// data represent a new CNPI client collection
type data struct {
	pluginPath string
	protocol   protocol
	plugins    []pluginData
}

type pluginData struct {
	connection     connectionHandler
	identityClient identity.IdentityClient
	operatorClient operator.OperatorClient

	name                 string
	version              string
	capabilities         []identity.PluginCapability_Service_Type
	operatorCapabilities []operator.OperatorCapability_RPC_Type
}

// NewUnixSocketClient creates a new CNPI client discovering plugins
// registered in a specific path
func NewUnixSocketClient(pluginPath string) Client {
	return &data{
		pluginPath: pluginPath,
		protocol:   protocolUnix(""),
	}
}

func (data *data) Load(ctx context.Context, name string) error {
	pluginData, err := data.loadPlugin(ctx, name)
	if err != nil {
		return err
	}

	data.plugins = append(data.plugins, pluginData)
	return nil
}

func (data *data) MetadataList() []Metadata {
	result := make([]Metadata, len(data.plugins))
	for i := range data.plugins {
		result[i] = data.plugins[i].Metadata()
	}

	return result
}

func (data *data) loadPlugin(ctx context.Context, name string) (pluginData, error) {
	var connection connectionHandler
	var err error

	defer func() {
		if err != nil && connection != nil {
			_ = connection.Close()
		}
	}()

	contextLogger := log.FromContext(ctx).WithValues("pluginName", name)
	ctx = log.IntoContext(ctx, contextLogger)

	if connection, err = data.protocol.dial(
		ctx,
		path.Join(data.pluginPath, name),
	); err != nil {
		contextLogger.Error(err, "Error while connecting to plugin")
		return pluginData{}, err
	}

	var result pluginData
	result, err = newPluginDataFromConnection(ctx, connection)
	if err != nil {
		return pluginData{}, err
	}

	// Load the list of services implemented by the plugin
	if err = result.loadPluginCapabilities(ctx); err != nil {
		return pluginData{}, err
	}

	// If the plugin implements the Operator service, load its
	// capabilities
	if slices.Contains(result.capabilities, identity.PluginCapability_Service_TYPE_OPERATOR_SERVICE) {
		if err = result.loadOperatorCapabilities(ctx); err != nil {
			return pluginData{}, err
		}
	}

	return result, nil
}

func (data *data) Close(ctx context.Context) {
	contextLogger := log.FromContext(ctx)
	for i := range data.plugins {
		plugin := &data.plugins[i]
		contextLogger := contextLogger.WithValues("pluginName", plugin.name)

		if err := plugin.connection.Close(); err != nil {
			contextLogger.Error(err, "while closing plugin connection")
		}
	}

	data.plugins = nil
}

func newPluginDataFromConnection(ctx context.Context, connection connectionHandler) (pluginData, error) {
	var err error

	identityClient := identity.NewIdentityClient(connection)

	var pluginInfoResponse *identity.GetPluginMetadataResponse

	if pluginInfoResponse, err = identityClient.GetPluginMetadata(
		ctx,
		&identity.GetPluginMetadataRequest{},
	); err != nil {
		return pluginData{}, fmt.Errorf("while querying plugin identity: %w", err)
	}

	result := pluginData{}
	result.connection = connection
	result.name = pluginInfoResponse.Name
	result.version = pluginInfoResponse.Version
	result.identityClient = identity.NewIdentityClient(connection)
	result.operatorClient = operator.NewOperatorClient(connection)

	return result, err
}

func (pluginData *pluginData) loadPluginCapabilities(ctx context.Context) error {
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

func (pluginData *pluginData) loadOperatorCapabilities(ctx context.Context) error {
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

// Metadata extracts the plugin metadata reading from
// the internal metadata
func (pluginData *pluginData) Metadata() Metadata {
	result := Metadata{
		Name:                 pluginData.name,
		Version:              pluginData.version,
		Capabilities:         make([]string, len(pluginData.capabilities)),
		OperatorCapabilities: make([]string, len(pluginData.operatorCapabilities)),
	}

	for i := range pluginData.capabilities {
		result.Capabilities[i] = pluginData.capabilities[i].String()
	}

	for i := range pluginData.operatorCapabilities {
		result.OperatorCapabilities[i] = pluginData.operatorCapabilities[i].String()
	}

	return result
}
