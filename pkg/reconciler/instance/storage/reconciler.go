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

package storage

import (
	"context"
	"io/fs"
	"os"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

type walDirectoryReconcilerOptions struct {
	// pgWalDirectory is the directory where PostgreSQL will look for WALs.
	// This is usually $PGDATA/pg_wal, and will be a symbolic link pointing
	// to the separate WAL storage if configured.
	pgWalDirectory string

	// walVolumeDirectory is the directory where the WAL volume is mounted.
	walVolumeDirectory string

	// walVolumeWalDirectory is the directory where the WALs should be stored.
	// This is usually inside of walVolumeDirectory
	walVolumeWalDirectory string
}

// ReconcileWalDirectory ensures that the `pg_wal` directory is moved to the attached volume (if present)
// and creates a symbolic link pointing to the new location.
func ReconcileWalDirectory(ctx context.Context) error {
	return internalReconcileWalDirectory(ctx, walDirectoryReconcilerOptions{
		pgWalDirectory:        specs.PgWalPath,
		walVolumeDirectory:    specs.PgWalVolumePath,
		walVolumeWalDirectory: specs.PgWalVolumePgWalPath,
	})
}

// internalReconcileWalDirectory is only meant to be used internally by unit tests
func internalReconcileWalDirectory(ctx context.Context, opts walDirectoryReconcilerOptions) error {
	contextLogger := log.FromContext(ctx)

	// Important: for now walStorage cannot be disabled once configured
	if pgWalExists, err := fileutils.FileExists(opts.walVolumeDirectory); err != nil {
		return err
	} else if !pgWalExists {
		return nil
	}

	// Check if `pg_wal` is already a symbolic link; if so, no further action is needed.
	pgWalDirInfo, err := os.Lstat(opts.pgWalDirectory)
	if err != nil {
		return err
	}
	if pgWalDirInfo.Mode().Type() == fs.ModeSymlink {
		return nil
	}

	contextLogger.Info("Moving data", "from", opts.pgWalDirectory, "to", opts.walVolumeWalDirectory)
	if err := fileutils.MoveDirectoryContent(opts.pgWalDirectory, opts.walVolumeWalDirectory); err != nil {
		contextLogger.Error(err, "Moving data", "from", opts.pgWalDirectory, "to",
			opts.walVolumeWalDirectory)
		return err
	}

	contextLogger.Debug("Deleting old path", "path", opts.pgWalDirectory)
	if err := fileutils.RemoveFile(opts.pgWalDirectory); err != nil {
		contextLogger.Error(err, "Deleting old path", "path", opts.pgWalDirectory)
		return err
	}

	contextLogger.Debug("Creating symlink", "from", opts.pgWalDirectory, "to", opts.walVolumeWalDirectory)
	return os.Symlink(opts.walVolumeWalDirectory, opts.pgWalDirectory)
}
