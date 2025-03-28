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

// Package prepare implement the "instance upgrade prepare" subcommand
package prepare

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/cloudnative-pg/machinery/pkg/env"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/postgres/pgconfig"
	"github.com/spf13/cobra"
)

// NewCmd create the cobra command
func NewCmd() *cobra.Command {
	var pgConfig string

	cmd := cobra.Command{
		Use:  "prepare [target]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			contextLogger := log.FromContext(cmd.Context())
			dest := args[0]

			if err := copyPostgresInstallation(cmd.Context(), pgConfig, dest); err != nil {
				contextLogger.Error(err, "Failed to copy the PostgreSQL installation")
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&pgConfig, "pg-config", env.GetOrDefault("PG_CONFIG", "pg_config"),
		`The path of "pg_config" executable. Defaults to "pg_config".`)

	return &cmd
}

// copyPostgresInstallation replicates the PostgreSQL installation to the specified destination directory
// for use by the pg_upgrade command as the old binary directory.
//
// Steps performed:
// 1. Removes the existing destination directory if it exists.
// 2. Retrieves the PostgreSQL binary, library, and shared directories using pg_config.
// 3. Creates the corresponding directories in the destination path.
// 4. Copies the contents of the PostgreSQL directories to the destination.
// 5. Creates a bindir.txt file in the destination directory with the path to the binary directory.
func copyPostgresInstallation(ctx context.Context, pgConfig string, dest string) error {
	contextLogger := log.FromContext(ctx)

	dest = path.Clean(dest)

	contextLogger.Info("Copying the PostgreSQL installation to the destination", "destination", dest)

	contextLogger.Info("Removing the destination directory", "directory", dest)
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("failed to remove the directory: %w", err)
	}

	contextLogger.Info("Creating the destination directory", "directory", dest)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("failed to create the directory: %w", err)
	}

	copyLocations := []pgconfig.ConfigurationParameter{pgconfig.BinDir, pgconfig.PkgLibDir, pgconfig.ShareDir}
	for _, config := range copyLocations {
		sourceDir, err := pgconfig.GetConfigurationParameter(pgConfig, config)
		if err != nil {
			return err
		}
		sourceDir = path.Clean(sourceDir)
		destDir := path.Clean(path.Join(dest, sourceDir))

		if config == pgconfig.BinDir {
			destFile := path.Join(dest, "bindir.txt")
			contextLogger.Info("Creating the bindir.txt file", "file", destFile)
			if _, err := fileutils.WriteStringToFile(destFile, fmt.Sprintf("%s\n", destDir)); err != nil {
				return fmt.Errorf("failed to write the %q file: %w", destFile, err)
			}
		}

		contextLogger.Info("Creating the directory", "directory", destDir)
		if err := os.MkdirAll(destDir, 0o750); err != nil {
			return fmt.Errorf("failed to create the directory: %w", err)
		}

		contextLogger.Info("Copying the files", "source", sourceDir, "destination", destDir)

		// We use "cp" instead of os.CopyFS because the latter doesn't
		// support symbolic links as of Go 1.24 and we don't want to
		// include any other dependencies in the project nor
		// re-implementing the wheel.
		//
		// This should be re-evaluated in the future and the
		// requirement to have "cp" in the image should be removed.
		if err := exec.Command("cp", "-a", sourceDir+"/.", destDir).Run(); err != nil { //nolint:gosec
			return fmt.Errorf("failed to copy the files: %w", err)
		}
	}

	return nil
}
