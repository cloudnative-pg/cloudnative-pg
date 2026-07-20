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

// Package bootstrap contains the logic that initializes a PostgreSQL data
// directory in-process. The instance manager uses it to bootstrap an instance
// without relying on a dedicated Kubernetes Job.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"

	barmanCommand "github.com/cloudnative-pg/barman-cloud/pkg/command"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	instancecertificate "github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/instance/certificate"
)

// Mode is the bootstrap method used to initialize a PostgreSQL data directory.
type Mode string

const (
	// ModeInitDB creates a brand-new data directory with initdb.
	ModeInitDB Mode = "initdb"

	// ModeJoin downloads the data directory from a primary with pg_basebackup.
	ModeJoin Mode = "join"

	// ModePgBaseBackup clones an external server with pg_basebackup.
	ModePgBaseBackup Mode = "pgbasebackup"

	// ModeRestore recovers the data directory from a backup.
	ModeRestore Mode = "restore"

	// ModeRestoreSnapshot recovers the data directory seeded from a volume snapshot.
	ModeRestoreSnapshot Mode = "restoresnapshot"
)

// Instruction describes how the instance manager must bootstrap a data directory.
type Instruction struct {
	// Mode is the bootstrap method to use.
	Mode Mode

	// Immediate, when true, only recovers the snapshot without starting
	// PostgreSQL. It is meaningful only for the ModeRestoreSnapshot mode.
	Immediate bool
}

// Execute bootstraps the PostgreSQL data directory described by info following
// the given instruction. The instance argument is only used by the join mode
// (to refresh its certificates); the cli argument is used by every mode except
// initdb.
func Execute(
	ctx context.Context,
	cli client.Client,
	instance *postgres.Instance,
	info postgres.InitInfo,
	instruction Instruction,
) error {
	switch instruction.Mode {
	case ModeInitDB:
		return executeInitDB(ctx, info)
	case ModeJoin:
		return executeJoin(ctx, cli, instance, info)
	case ModePgBaseBackup:
		return executePgBaseBackup(ctx, cli, info)
	case ModeRestore:
		return executeRestore(ctx, cli, info)
	case ModeRestoreSnapshot:
		return executeRestoreSnapshot(ctx, cli, info, instruction.Immediate)
	default:
		return fmt.Errorf("unknown bootstrap mode: %q", instruction.Mode)
	}
}

func executeInitDB(ctx context.Context, info postgres.InitInfo) error {
	contextLogger := log.FromContext(ctx)

	if err := info.EnsureTargetDirectoriesDoNotExist(ctx); err != nil {
		return err
	}

	if err := info.Bootstrap(ctx); err != nil {
		contextLogger.Error(err, "Error while bootstrapping data directory")
		return err
	}

	return nil
}

func executeJoin(ctx context.Context, cli client.Client, instance *postgres.Instance, info postgres.InitInfo) error {
	contextLogger := log.FromContext(ctx)

	if err := info.EnsureTargetDirectoriesDoNotExist(ctx); err != nil {
		return err
	}

	// Download the cluster definition from the API server
	var cluster apiv1.Cluster
	if err := cli.Get(ctx,
		client.ObjectKey{Namespace: instance.GetNamespaceName(), Name: instance.GetClusterName()},
		&cluster,
	); err != nil {
		contextLogger.Error(err, "Error while getting cluster")
		return err
	}
	instance.SetCluster(&cluster)

	if _, err := instancecertificate.NewReconciler(cli, instance).RefreshSecrets(ctx, &cluster); err != nil {
		contextLogger.Error(err, "Error while refreshing secrets")
		return err
	}

	// Run "pg_basebackup" to download the data directory from the primary
	if err := info.Join(ctx, &cluster); err != nil {
		contextLogger.Error(err, "Error joining node")
		return err
	}

	return nil
}

func executeRestore(ctx context.Context, cli client.Client, info postgres.InitInfo) error {
	contextLogger := log.FromContext(ctx)

	if err := info.EnsureTargetDirectoriesDoNotExist(ctx); err != nil {
		return err
	}

	if err := info.Restore(ctx, cli); err != nil {
		contextLogger.Error(err, "Error while restoring a backup")
		cleanupDataDirectoryIfNeeded(ctx, err, info.PgData)
		return err
	}

	contextLogger.Info("restore command execution completed without errors")

	return nil
}

func executeRestoreSnapshot(ctx context.Context, cli client.Client, info postgres.InitInfo, immediate bool) error {
	contextLogger := log.FromContext(ctx)

	if err := info.RestoreSnapshot(ctx, cli, immediate); err != nil {
		contextLogger.Error(err, "Error while restoring a backup")
		return err
	}

	return nil
}

func cleanupDataDirectoryIfNeeded(ctx context.Context, restoreError error, dataDirectory string) {
	contextLogger := log.FromContext(ctx)

	var barmanError *barmanCommand.CloudRestoreError
	if !errors.As(restoreError, &barmanError) {
		return
	}

	if !barmanError.IsRetriable() {
		return
	}

	contextLogger.Info("Cleaning up data directory", "directory", dataDirectory)
	if err := fileutils.RemoveDirectory(dataDirectory); err != nil && !os.IsNotExist(err) {
		contextLogger.Error(
			err,
			"error occurred cleaning up data directory",
			"directory", dataDirectory)
	}
}
