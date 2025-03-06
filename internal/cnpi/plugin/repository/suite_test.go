package repository

import (
	"context"
	"net"
	"testing"

	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRepository(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Repository Suite")
}

type identityImplementation struct {
	identity.UnimplementedIdentityServer
}

// GetPluginMetadata implements Identity
func (i identityImplementation) GetPluginMetadata(
	_ context.Context,
	_ *identity.GetPluginMetadataRequest,
) (*identity.GetPluginMetadataResponse, error) {
	return &identity.GetPluginMetadataResponse{
		Name:          "testing-service",
		Version:       "0.0.1",
		DisplayName:   "testing-service",
		ProjectUrl:    "https://github.com/cloudnative-pg/cloudnative-pg",
		RepositoryUrl: "https://github.com/cloudnative-pg/cloudnative-pg",
		License:       "APACHE 2.0",
		Maturity:      "alpha",
	}, nil
}

// GetPluginCapabilities implements identity
func (i identityImplementation) GetPluginCapabilities(
	_ context.Context,
	_ *identity.GetPluginCapabilitiesRequest,
) (*identity.GetPluginCapabilitiesResponse, error) {
	return &identity.GetPluginCapabilitiesResponse{
		Capabilities: []*identity.PluginCapability{},
	}, nil
}

// Probe implements Identity
func (i identityImplementation) Probe(
	_ context.Context,
	_ *identity.ProbeRequest,
) (*identity.ProbeResponse, error) {
	return &identity.ProbeResponse{
		Ready: true,
	}, nil
}

type unitTestProtocol struct {
	name         string
	mockHandlers []*mockHandler
	server       *grpc.Server
}

type mockHandler struct {
	*grpc.ClientConn
	closed bool
}

func newUnitTestProtocol(name string) *unitTestProtocol {
	return &unitTestProtocol{name: name}
}

func (h *mockHandler) Close() error {
	_ = h.ClientConn.Close()
	h.closed = true
	return nil
}

func (p *unitTestProtocol) Dial(ctx context.Context) (connection.Handler, error) {
	listener := bufconn.Listen(1024 * 1024)

	if len(p.mockHandlers) == 0 {
		p.server = grpc.NewServer()

		identity.RegisterIdentityServer(p.server, &identityImplementation{})

		go func() {
			<-ctx.Done()
			p.server.Stop()
		}()

		go func() {
			_ = p.server.Serve(listener)
		}()
	}

	dialer := func(_ context.Context, _ string) (net.Conn, error) {
		return listener.Dial()
	}

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	mh := &mockHandler{
		ClientConn: conn,
	}
	p.mockHandlers = append(p.mockHandlers, mh)
	return mh, nil
}
