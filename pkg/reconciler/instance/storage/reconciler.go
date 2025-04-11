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

// ReconcileWalStorage ensures that the `pg_wal` directory is moved to the attached volume (if present)
// and creates a symbolic link pointing to the new location.
func ReconcileWalStorage(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	if pgWalExists, err := fileutils.FileExists(specs.PgWalVolumePath); err != nil {
		return err
	} else if !pgWalExists {
		return nil
	}

	// Check if `pg_wal` is already a symbolic link; if so, no further action is needed.
	pgWalDirInfo, err := os.Lstat(specs.PgWalPath)
	if err != nil {
		return err
	}
	if pgWalDirInfo.Mode().Type() == fs.ModeSymlink {
		return nil
	}

	contextLogger.Info("Moving data", "from", specs.PgWalPath, "to", specs.PgWalVolumePgWalPath)
	if err := fileutils.MoveDirectoryContent(specs.PgWalPath, specs.PgWalVolumePgWalPath); err != nil {
		contextLogger.Error(err, "Moving data", "from", specs.PgWalPath, "to",
			specs.PgWalVolumePgWalPath)
		return err
	}

	contextLogger.Debug("Deleting old path", "path", specs.PgWalPath)
	if err := fileutils.RemoveFile(specs.PgWalPath); err != nil {
		contextLogger.Error(err, "Deleting old path", "path", specs.PgWalPath)
		return err
	}

	contextLogger.Debug("Creating symlink", "from", specs.PgWalPath, "to", specs.PgWalVolumePgWalPath)
	return os.Symlink(specs.PgWalVolumePgWalPath, specs.PgWalPath)
}
