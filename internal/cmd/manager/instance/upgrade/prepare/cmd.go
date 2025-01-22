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

// Package prepare implement the "instance upgrade prepare" subcommand
package prepare

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
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

	cmd.Flags().StringVar(&pgConfig, "pg-config", getEnvOrDefault("PG_CONFIG", "pg_config"),
		`The path of "pg_config" executable. Defaults to "pg_config".`)

	return &cmd
}

func getEnvOrDefault(env, def string) string {
	if value, ok := os.LookupEnv(env); ok {
		return value
	}
	return def
}

// copyPostgresInstallation is roughly equivalent to the following bash code
//
//	> rm -fr /controller/old
//	>
//	> bindir=$(pg_config --bindir)
//	> mkdir -p "/controller/old${bindir}"
//	> cp -ax "${bindir}"/. "/controller/old${bindir}"
//	>
//	> pkglibdir=$(pg_config --pkglibdir)
//	> mkdir -p "/controller/old${pkglibdir}"
//	> cp -ax "${pkglibdir}"/. "/controller/old${pkglibdir}"
//	>
//	> sharedir=$(pg_config --sharedir)
//	> mkdir -p "/controller/old${sharedir}"
//	> cp -ax "${sharedir}"/. "/controller/old${sharedir}"
//	>
//	> echo "/controller/old${bindir}" > /controller/old/bindir.txt
func copyPostgresInstallation(ctx context.Context, pgConfig string, dest string) error {
	contextLogger := log.FromContext(ctx)

	dest = path.Clean(dest)

	contextLogger.Info("Copying the PostgreSQL installation to the destination", "destination", dest)

	// Remove the old directory
	contextLogger.Info("Removing the destination directory", "directory", dest)
	err := os.RemoveAll(dest)
	if err != nil {
		return fmt.Errorf("failed to remove the directory: %w", err)
	}

	// Create the destination directory
	contextLogger.Info("Creating the destination directory", "directory", dest)
	err = os.MkdirAll(dest, 0o750)
	if err != nil {
		return fmt.Errorf("failed to create the directory: %w", err)
	}

	for _, config := range []string{"bindir", "pkglibdir", "sharedir"} {
		sourceDir, err := getPostgresConfig(pgConfig, config)
		if err != nil {
			return err
		}
		sourceDir = path.Clean(sourceDir)
		destDir := path.Clean(path.Join(dest, sourceDir))

		if config == "bindir" {
			destFile := path.Join(dest, "bindir.txt")
			contextLogger.Info("Creating the bindir.txt file", "file", destFile)
			_, err := fileutils.WriteStringToFile(destFile, fmt.Sprintf("%s\n", destDir))
			if err != nil {
				return fmt.Errorf("failed to write the %q file: %w", destFile, err)
			}
		}

		contextLogger.Info("Creating the directory", "directory", destDir)
		err = os.MkdirAll(destDir, 0o750)
		if err != nil {
			return fmt.Errorf("failed to create the directory: %w", err)
		}

		contextLogger.Info("Copying the files", "source", sourceDir, "destination", destDir)
		err = exec.Command("cp", "-a", sourceDir+"/.", destDir).Run() //nolint:gosec
		if err != nil {
			return fmt.Errorf("failed to copy the files: %w", err)
		}
	}

	return nil
}

func getPostgresConfig(pgConfig string, dir string) (string, error) {
	out, err := exec.Command(pgConfig, "--"+dir).Output() //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("failed to get the %q value from pg_config: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}
