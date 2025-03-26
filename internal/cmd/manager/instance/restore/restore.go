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

package restore

import (
	"context"
	"errors"
	"fmt"
	"os"

	barmanCommand "github.com/cloudnative-pg/barman-cloud/pkg/command"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

type restoreRunnable struct {
	cli         client.Client
	clusterName string
	namespace   string
	pgData      string
	pgWal       string
	cancel      context.CancelFunc
}

func (r *restoreRunnable) Start(ctx context.Context) error {
	// we will wait this way for the mgr and informers to be online
	if err := management.WaitForGetClusterWithClient(ctx, r.cli, client.ObjectKey{
		Name:      r.clusterName,
		Namespace: r.namespace,
	}); err != nil {
		return fmt.Errorf("while waiting for API server connectivity: %w", err)
	}

	info := postgres.InitInfo{
		ClusterName: r.clusterName,
		Namespace:   r.namespace,
		PgData:      r.pgData,
		PgWal:       r.pgWal,
	}

	if err := restoreSubCommand(ctx, info, r.cli); err != nil {
		return fmt.Errorf("while restoring cluster: %s", err)
	}

	// the backup was restored correctly and we now ask
	// the manager to quit
	r.cancel()
	return nil
}

func restoreSubCommand(ctx context.Context, info postgres.InitInfo, cli client.Client) error {
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
