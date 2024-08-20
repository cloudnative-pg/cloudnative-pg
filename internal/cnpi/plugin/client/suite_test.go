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
	"testing"

	"github.com/cloudnative-pg/cnpg-i/pkg/backup"
	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	"github.com/cloudnative-pg/cnpg-i/pkg/operator"
	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	"github.com/cloudnative-pg/cnpg-i/pkg/wal"
	"google.golang.org/grpc"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Client Suite")
}

type fakeOperatorClient struct {
	capabilities          *operator.OperatorCapabilitiesResult
	status                *operator.SetStatusInClusterResponse
	errSetStatusInCluster error
}

func (f *fakeOperatorClient) GetCapabilities(
	_ context.Context,
	_ *operator.OperatorCapabilitiesRequest,
	_ ...grpc.CallOption,
) (*operator.OperatorCapabilitiesResult, error) {
	return f.capabilities, nil
}

func (f *fakeOperatorClient) ValidateClusterCreate(
	_ context.Context,
	_ *operator.OperatorValidateClusterCreateRequest,
	_ ...grpc.CallOption,
) (*operator.OperatorValidateClusterCreateResult, error) {
	panic("implement me")
}

func (f *fakeOperatorClient) ValidateClusterChange(
	_ context.Context,
	_ *operator.OperatorValidateClusterChangeRequest,
	_ ...grpc.CallOption,
) (*operator.OperatorValidateClusterChangeResult, error) {
	panic("implement me")
}

func (f *fakeOperatorClient) MutateCluster(
	_ context.Context,
	_ *operator.OperatorMutateClusterRequest,
	_ ...grpc.CallOption,
) (*operator.OperatorMutateClusterResult, error) {
	panic("implement me")
}

func (f *fakeOperatorClient) SetStatusInCluster(
	_ context.Context,
	_ *operator.SetStatusInClusterRequest,
	_ ...grpc.CallOption,
) (*operator.SetStatusInClusterResponse, error) {
	if f.errSetStatusInCluster != nil {
		return nil, f.errSetStatusInCluster
	}
	return f.status, nil
}

func (f *fakeOperatorClient) Deregister(
	_ context.Context,
	_ *operator.DeregisterRequest,
	_ ...grpc.CallOption,
) (*operator.DeregisterResponse, error) {
	panic("implement me")
}

type fakeConnection struct {
	lifecycleClient       lifecycle.OperatorLifecycleClient
	lifecycleCapabilities []*lifecycle.OperatorLifecycleCapabilities
	name                  string
	operatorClient        *fakeOperatorClient
}

func (f *fakeConnection) setStatusResponse(status []byte) {
	f.operatorClient.status = &operator.SetStatusInClusterResponse{
		JsonStatus: status,
	}
}

func (f *fakeConnection) Name() string {
	return f.name
}

func (f *fakeConnection) Metadata() connection.Metadata {
	panic("not implemented") // TODO: Implement
}

func (f *fakeConnection) LifecycleClient() lifecycle.OperatorLifecycleClient {
	return f.lifecycleClient
}

func (f *fakeConnection) OperatorClient() operator.OperatorClient {
	return f.operatorClient
}

func (f *fakeConnection) WALClient() wal.WALClient {
	panic("not implemented") // TODO: Implement
}

func (f *fakeConnection) BackupClient() backup.BackupClient {
	panic("not implemented") // TODO: Implement
}

func (f *fakeConnection) ReconcilerHooksClient() reconciler.ReconcilerHooksClient {
	panic("not implemented") // TODO: Implement
}

func (f *fakeConnection) PluginCapabilities() []identity.PluginCapability_Service_Type {
	panic("not implemented") // TODO: Implement
}

func (f *fakeConnection) OperatorCapabilities() []operator.OperatorCapability_RPC_Type {
	res := make(
		[]operator.OperatorCapability_RPC_Type,
		len(f.operatorClient.capabilities.Capabilities))

	for i := range f.operatorClient.capabilities.Capabilities {
		res[i] = f.operatorClient.capabilities.Capabilities[i].GetRpc().Type
	}

	return res
}

func (f *fakeConnection) WALCapabilities() []wal.WALCapability_RPC_Type {
	panic("not implemented") // TODO: Implement
}

func (f *fakeConnection) LifecycleCapabilities() []*lifecycle.OperatorLifecycleCapabilities {
	return f.lifecycleCapabilities
}

func (f *fakeConnection) BackupCapabilities() []backup.BackupCapability_RPC_Type {
	panic("not implemented") // TODO: Implement
}

func (f *fakeConnection) ReconcilerCapabilities() []reconciler.ReconcilerHooksCapability_Kind {
	panic("not implemented")
}

func (f *fakeConnection) Ping(_ context.Context) error {
	panic("not implemented") // TODO: Implement
}

func (f *fakeConnection) Close() error {
	panic("not implemented") // TODO: Implement
}
