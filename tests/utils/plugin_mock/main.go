/*
Copyright 2025 The CloudNativePG Contributors.

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

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudnative-pg/cnpg-i/pkg/backup"
	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"github.com/cloudnative-pg/cnpg-i/pkg/wal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type identityServer struct {
	identity.UnimplementedIdentityServer
}

type backupServer struct {
	backup.UnimplementedBackupServer
}

type walServer struct {
	wal.UnimplementedWALServer
}

func (s *identityServer) GetPluginMetadata(
	context.Context,
	*identity.GetPluginMetadataRequest,
) (*identity.GetPluginMetadataResponse, error) {
	return &identity.GetPluginMetadataResponse{
		Name:          "cnpg-i-plugin-mock.test",
		Version:       "0.0.1",
		DisplayName:   "Mock Plugin",
		ProjectUrl:    "https://github.com/cloudnative-pg/cloudnative-pg",
		RepositoryUrl: "https://github.com/cloudnative-pg/cloudnative-pg",
		License:       "Apache-2.0",
		LicenseUrl:    "http://www.apache.org/licenses/LICENSE-2.0",
		Maturity:      "alpha",
	}, nil
}

func (s *identityServer) GetPluginCapabilities(
	context.Context,
	*identity.GetPluginCapabilitiesRequest,
) (*identity.GetPluginCapabilitiesResponse, error) {
	return &identity.GetPluginCapabilitiesResponse{
		Capabilities: []*identity.PluginCapability{
			{
				Type: &identity.PluginCapability_Service_{
					Service: &identity.PluginCapability_Service{
						Type: identity.PluginCapability_Service_TYPE_BACKUP_SERVICE,
					},
				},
			},
			{
				Type: &identity.PluginCapability_Service_{
					Service: &identity.PluginCapability_Service{
						Type: identity.PluginCapability_Service_TYPE_WAL_SERVICE,
					},
				},
			},
		},
	}, nil
}

func (s *identityServer) Probe(
	context.Context,
	*identity.ProbeRequest,
) (*identity.ProbeResponse, error) {
	return &identity.ProbeResponse{
		Ready: true,
	}, nil
}

func (s *backupServer) GetCapabilities(
	context.Context,
	*backup.BackupCapabilitiesRequest,
) (*backup.BackupCapabilitiesResult, error) {
	return &backup.BackupCapabilitiesResult{
		Capabilities: []*backup.BackupCapability{
			{
				Type: &backup.BackupCapability_Rpc{
					Rpc: &backup.BackupCapability_RPC{
						Type: backup.BackupCapability_RPC_TYPE_BACKUP,
					},
				},
			},
		},
	}, nil
}

func (s *backupServer) Backup(
	ctx context.Context,
	req *backup.BackupRequest,
) (*backup.BackupResult, error) {
	fmt.Printf("Starting backup\n")
	return &backup.BackupResult{
		BackupId:   "mock-backup-id",
		BackupName: "mock-backup-name",
		StartedAt:  time.Now().Unix(),
		StoppedAt:  time.Now().Unix(),
		InstanceId: "mock-instance-id",
		Online:     true,
		BeginLsn:   "0/1000028", // Dummy LSN
		EndLsn:     "0/1000060", // Dummy LSN
		BeginWal:   "000000010000000000000001",
		EndWal:     "000000010000000000000001",
	}, nil
}

func (s *walServer) GetCapabilities(
	context.Context,
	*wal.WALCapabilitiesRequest,
) (*wal.WALCapabilitiesResult, error) {
	return &wal.WALCapabilitiesResult{
		Capabilities: []*wal.WALCapability{
			{
				Type: &wal.WALCapability_Rpc{
					Rpc: &wal.WALCapability_RPC{
						Type: wal.WALCapability_RPC_TYPE_ARCHIVE_WAL,
					},
				},
			},
			{
				Type: &wal.WALCapability_Rpc{
					Rpc: &wal.WALCapability_RPC{
						Type: wal.WALCapability_RPC_TYPE_RESTORE_WAL,
					},
				},
			},
		},
	}, nil
}

func (s *walServer) Archive(
	ctx context.Context,
	req *wal.WALArchiveRequest,
) (*wal.WALArchiveResult, error) {
	fmt.Printf("Received WAL archive request for %s\n", req.SourceFileName)
	return &wal.WALArchiveResult{}, nil
}

func (s *walServer) Restore(
	ctx context.Context,
	req *wal.WALRestoreRequest,
) (*wal.WALRestoreResult, error) {
	fmt.Printf("Received WAL restore request for %s\n", req.SourceWalName)
	return &wal.WALRestoreResult{}, nil
}

func main() {
	port := os.Getenv("PLUGIN_PORT")
	if port == "" {
		port = "9090"
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		fmt.Printf("failed to listen: %v\n", err)
		os.Exit(1)
	}

	s := grpc.NewServer()

	identity.RegisterIdentityServer(s, &identityServer{})
	backup.RegisterBackupServer(s, &backupServer{})
	wal.RegisterWALServer(s, &walServer{})
	reflection.Register(s)

	fmt.Printf("server listening at %v\n", lis.Addr())

	go func() {
		if err := s.Serve(lis); err != nil {
			fmt.Printf("failed to serve: %v\n", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("Shutting down server...")
	s.GracefulStop()
}
