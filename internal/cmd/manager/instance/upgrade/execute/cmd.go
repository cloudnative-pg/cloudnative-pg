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

// Package execute implements the "instance upgrade execute" subcommand
package execute

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/blang/semver"
	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/fileutils/compatibility"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/metricserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// NewCmd creates the cobra command
func NewCmd() *cobra.Command {
	var pgData string
	var podName string
	var clusterName string
	var namespace string

	cmd := &cobra.Command{
		Use:  "execute [options]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldBinDirFile := args[0]
			ctx := cmd.Context()
			// The fields in the instance are needed to correctly
			// download the secret containing the TLS
			// certificates
			instance := postgres.NewInstance().
				WithNamespace(namespace).
				WithPodName(podName).
				WithClusterName(clusterName)

			// Read the old bindir from the passed file
			oldBinDir, err := fileutils.ReadFile(oldBinDirFile)
			if err != nil {
				return fmt.Errorf("error while reading the old bindir: %w", err)
			}

			return upgradeSubCommand(ctx, instance, pgData, strings.TrimSpace(string(oldBinDir)))
		},
		PostRunE: func(cmd *cobra.Command, _ []string) error {
			if err := istio.TryInvokeQuitEndpoint(cmd.Context()); err != nil {
				return err
			}

			return linkerd.TryInvokeShutdownEndpoint(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of this pod, to "+
		"be checked against the cluster state")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of "+
		"the current cluster in k8s, used to download TLS certificates")

	return cmd
}

func upgradeSubCommand(ctx context.Context, instance *postgres.Instance, pgData string, oldBinDir string) error {
	contextLogger := log.FromContext(ctx)

	client, err := management.NewControllerRuntimeClient()
	if err != nil {
		contextLogger.Error(err, "Error creating Kubernetes client")
		return err
	}

	if err = management.WaitForGetClusterWithClient(ctx, client, ctrl.ObjectKey{
		Name:      instance.GetClusterName(),
		Namespace: instance.GetNamespaceName(),
	}); err != nil {
		return err
	}

	// Create a fake reconciler just to download the secrets and
	// the cluster definition
	metricExporter := metricserver.NewExporter(instance)
	reconciler := controller.NewInstanceReconciler(instance, client, metricExporter)

	// Download the cluster definition from the API server
	var cluster apiv1.Cluster
	if err := reconciler.GetClient().Get(ctx,
		ctrl.ObjectKey{Namespace: instance.GetNamespaceName(), Name: instance.GetClusterName()},
		&cluster,
	); err != nil {
		contextLogger.Error(err, "Error while getting cluster")
		return err
	}

	// Since we're directly using the reconciler here, we cannot
	// tell if the secrets were correctly downloaded or not.
	// If they were the following "pg_upgrade" command will work, if
	// they don't "pg_upgrade" with fail, complaining that the
	// cryptographic material is not available.
	reconciler.RefreshSecrets(ctx, &cluster)

	contextLogger.Info("Starting the upgrade process")

	newDataDir := "/var/lib/postgresql/data/new"
	var newWalDir *string
	if cluster.ShouldCreateWalArchiveVolume() {
		newWalDir = ptr.To(path.Join(specs.PgWalVolumePath, "new_wal"))
	}

	contextLogger.Info("Ensuring the new data directory does not exist", "directory", newDataDir)

	if err := os.RemoveAll(newDataDir); err != nil {
		return fmt.Errorf("failed to remove the directory: %w", err)
	}
	contextLogger.Info("Creating data directory", "directory", newDataDir)
	if err := runInitDB(newDataDir, newWalDir); err != nil {
		return fmt.Errorf("error while creating the data directory: %w", err)
	}

	contextLogger.Info("Preparing configuration files", "directory", newDataDir)
	if err := prepareConfigurationFiles(newDataDir); err != nil {
		return err
	}

	contextLogger.Info("Checking if we have anything to update")
	// Read pg_version from both the old and new data directories
	oldVersion, err := utils.GetPgdataVersion(pgData)
	if err != nil {
		return fmt.Errorf("error while reading the old version: %w", err)
	}

	newVersion, err := utils.GetPgdataVersion(newDataDir)
	if err != nil {
		return fmt.Errorf("error while reading the new version: %w", err)
	}

	if oldVersion.Equals(newVersion) {
		contextLogger.Info("Versions are the same, no need to upgrade")
		if err := os.RemoveAll(newDataDir); err != nil {
			return fmt.Errorf("failed to remove the directory: %w", err)
		}
		return nil
	}

	contextLogger.Info("Running pg_upgrade")
	if err := runPgUpgrade(pgData, newDataDir, oldBinDir); err != nil {
		return fmt.Errorf("error while running pg_upgrade: %w", err)
	}

	err = moveDataInPlace(ctx, pgData, oldVersion, newDataDir, newWalDir)
	if err != nil {
		contextLogger.Error(err, "Error while moving the data in place, saving the new data directory to avoid data loss")

		if errInner := moveDirIfExists(newDataDir, newDataDir+".failed"); err != nil {
			contextLogger.Error(errInner, "Error while saving the new data directory")
		}

		if newWalDir != nil {
			errInner := moveDirIfExists(*newWalDir, *newWalDir+".failed")
			if errInner != nil {
				contextLogger.Error(errInner, "Error while saving the new pg_wal directory")
			}
		}

		return err
	}

	contextLogger.Info("Upgrade completed successfully")

	return nil
}

func runInitDB(destDir string, walDir *string) error {
	// Invoke initdb to generate a data directory
	options := []string{
		"--username",
		"postgres",
		"-D",
		destDir,
	}

	if walDir != nil {
		options = append(options, "--waldir", *walDir)
	}

	// Certain CSI drivers may add setgid permissions on newly created folders.
	// A default umask is set to attempt to avoid this, by revoking group/other
	// permission bits on the PGDATA
	_ = compatibility.Umask(0o077)

	initdbCmd := exec.Command(constants.InitdbName, options...) // #nosec
	if err := execlog.RunBuffering(initdbCmd, constants.InitdbName); err != nil {
		return err
	}

	return nil
}

func prepareConfigurationFiles(destDir string) error {
	// Always read the custom and override configuration files created by the operator
	_, err := configfile.EnsureIncludes(path.Join(destDir, "postgresql.conf"),
		constants.PostgresqlCustomConfigurationFile,
		constants.PostgresqlOverrideConfigurationFile,
	)
	if err != nil {
		return fmt.Errorf("appending inclusion directives to postgresql.conf file resulted in an error: %w", err)
	}

	// Create a stub for the configuration file
	// to be filled during the real startup of this instance
	err = fileutils.CreateEmptyFile(
		path.Join(destDir, constants.PostgresqlCustomConfigurationFile))
	if err != nil {
		return fmt.Errorf("creating the operator managed configuration file '%v' resulted in an error: %w",
			constants.PostgresqlCustomConfigurationFile, err)
	}

	// Create a stub for the configuration file
	// to be filled during the real startup of this instance
	err = fileutils.CreateEmptyFile(
		path.Join(destDir, constants.PostgresqlOverrideConfigurationFile))
	if err != nil {
		return fmt.Errorf("creating the operator managed configuration file '%v' resulted in an error: %w",
			constants.PostgresqlOverrideConfigurationFile, err)
	}

	return nil
}

func runPgUpgrade(oldDataDir string, newDataDir string, oldBinDir string) error {
	// Run the pg_upgrade command
	cmd := exec.Command("pg_upgrade",
		"--link",
		"--old-bindir", oldBinDir,
		"--old-datadir", oldDataDir,
		"--new-datadir", newDataDir,
	) // #nosec
	cmd.Dir = newDataDir
	if err := execlog.RunBuffering(cmd, "pg_upgrade"); err != nil {
		return fmt.Errorf("error while running pg_upgrade: %w", err)
	}

	return nil
}

func moveDataInPlace(
	ctx context.Context,
	pgData string,
	oldVersion semver.Version,
	newDataDir string,
	newWalDir *string,
) error {
	contextLogger := log.FromContext(ctx)

	contextLogger.Info("Cleaning up the new data directory")
	if err := os.RemoveAll(path.Join(newDataDir, "delete_old_cluster.sh")); err != nil {
		return fmt.Errorf("error while removing the delete_old_cluster.sh script: %w", err)
	}

	contextLogger.Info("Cleaning up the old data directory")
	if err := clearDirectory(path.Join(pgData, "pg_wal")); err != nil {
		return fmt.Errorf("error while removing the content of old pg_wal directory: %w", err)
	}
	if err := clearDirectory(pgData, path.Join(pgData, "pg_wal")); err != nil {
		return fmt.Errorf("error while removing the content of old data directory: %w", err)
	}

	contextLogger.Info("Moving the new pg_wal directory in place")
	if err := moveContents(path.Join(newDataDir, "pg_wal"), path.Join(pgData, "pg_wal")); err != nil {
		return fmt.Errorf("error while moving the pg_wal directory content: %w", err)
	}
	if err := os.Remove(path.Join(newDataDir, "pg_wal")); err != nil {
		return fmt.Errorf("error while removing the new pg_wal directory: %w", err)
	}
	if newWalDir != nil {
		if err := os.Remove(*newWalDir); err != nil {
			return fmt.Errorf("error while removing the new pg_wal directory in the WAL volume: %w", err)
		}
	}

	contextLogger.Info("Moving the new data directory in place")
	if err := moveContents(newDataDir, pgData); err != nil {
		return fmt.Errorf("error while moving the new data directory: %w", err)
	}
	if err := os.Remove(newDataDir); err != nil {
		return fmt.Errorf("error while removing the new data directory: %w", err)
	}

	contextLogger.Info("Cleaning up the previous version directory from tablespaces")
	if err := removeMatchingPaths(ctx,
		path.Join(pgData, "pg_tblspc", "*", fmt.Sprintf("PG_%v_*", oldVersion))); err != nil {
		return fmt.Errorf("error while removing the old tablespaces directories: %w", err)
	}

	return nil
}

func clearDirectory(dir string, exclude ...string) error {
	// Read the contents of the directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	// Loop through the entries and remove them
	for _, entry := range entries {
		entryPath := path.Join(dir, entry.Name())
		if slices.Contains(exclude, entryPath) {
			continue
		}

		if entry.IsDir() {
			// Recursively remove subdirectory
			err = os.RemoveAll(entryPath)
		} else {
			// Remove file
			err = os.Remove(entryPath)
		}

		if err != nil {
			return fmt.Errorf("failed to remove %s: %w", entryPath, err)
		}
	}

	return nil
}

func moveContents(srcDir, destDir string) error {
	// Ensure the source directory exists
	srcInfo, err := os.Stat(srcDir)
	if err != nil {
		return fmt.Errorf("source directory does not exist: %v", err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source path is not a directory")
	}

	// Ensure the destination directory exists (create if it doesn't)
	err = os.MkdirAll(destDir, 0o750)
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// Read the contents of the source directory
	contents, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %v", err)
	}

	// Iterate through the contents and move each item
	for _, item := range contents {
		srcPath := filepath.Join(srcDir, item.Name())
		destPath := filepath.Join(destDir, item.Name())

		// Move the file or directory
		err := os.Rename(srcPath, destPath)
		if err != nil {
			return fmt.Errorf("failed to move %s to %s: %v", srcPath, destPath, err)
		}
	}

	return nil
}

func removeMatchingPaths(ctx context.Context, pattern string) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("Removing matching paths", "pattern", pattern)

	// Find all matching paths
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("error matching paths: %v", err)
	}

	// Iterate through the matches and remove each
	for _, match := range matches {
		contextLogger.Info("Removing path", "path", match)
		err := os.RemoveAll(match)
		if err != nil {
			return fmt.Errorf("failed to remove %s: %v", match, err)
		}
	}

	return nil
}

func moveDirIfExists(targetDir string, destDir string) error {
	if _, errExists := os.Stat(targetDir); !os.IsNotExist(errExists) {
		err := moveContents(targetDir, destDir)
		if err != nil {
			return err
		}
	}

	return nil
}
