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

// Package execute implements the "instance upgrade execute" subcommand
package execute

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cnpgiPostgres "github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"github.com/cloudnative-pg/machinery/pkg/env"
	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/fileutils/compatibility"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	"github.com/spf13/cobra"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	instancecertificate "github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/instance/certificate"
	instancestorage "github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/instance/storage"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// NewCmd creates the cobra command
func NewCmd() *cobra.Command {
	var pgData string
	var podName string
	var clusterName string
	var namespace string
	var pgUpgrade string
	var pgUpgradeArgs []string
	var initdb string
	var initdbArgs []string

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
			oldBinDirBytes, err := fileutils.ReadFile(oldBinDirFile)
			if err != nil {
				return fmt.Errorf("error while reading the old bindir: %w", err)
			}

			oldBinDir := strings.TrimSpace(string(oldBinDirBytes))
			info := upgradeInfo{
				pgData:        pgData,
				oldBinDir:     oldBinDir,
				pgUpgrade:     pgUpgrade,
				pgUpgradeArgs: pgUpgradeArgs,
				initdb:        initdb,
				initdbArgs:    initdbArgs,
			}
			return info.upgradeSubCommand(ctx, instance)
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
	cmd.Flags().StringVar(&pgUpgrade, "pg-upgrade", env.GetOrDefault("PG_UPGRADE", "pg_upgrade"),
		`The path of "pg_upgrade" executable. Defaults to "pg_upgrade".`)
	cmd.Flags().StringArrayVar(&pgUpgradeArgs, "pg-upgrade-args", nil,
		`Additional arguments for "pg_upgrade" invocation. `+
			`Use the --pg-upgrade-args flag multiple times to pass multiple arguments.`)
	cmd.Flags().StringVar(&initdb, "initdb", env.GetOrDefault("INITDB", "initdb"),
		`The path of "initdb" executable. Defaults to "initdb".`)
	cmd.Flags().StringArrayVar(&initdbArgs, "initdb-args", nil,
		`Additional arguments for "initdb" invocation.`+
			`Use the --initdb-args flag multiple times to pass multiple arguments.`)

	return cmd
}

type upgradeInfo struct {
	pgData        string
	oldBinDir     string
	pgUpgrade     string
	pgUpgradeArgs []string
	initdb        string
	initdbArgs    []string
}

// nolint:gocognit
func (ui upgradeInfo) upgradeSubCommand(ctx context.Context, instance *postgres.Instance) error {
	contextLogger := log.FromContext(ctx)

	client, err := management.NewControllerRuntimeClient()
	if err != nil {
		contextLogger.Error(err, "Error creating Kubernetes client")
		return err
	}

	clusterObjectKey := ctrl.ObjectKey{Name: instance.GetClusterName(), Namespace: instance.GetNamespaceName()}
	if err = management.WaitForGetClusterWithClient(ctx, client, clusterObjectKey); err != nil {
		return err
	}

	// Download the cluster definition from the API server
	var cluster apiv1.Cluster
	if err := client.Get(ctx, clusterObjectKey, &cluster); err != nil {
		contextLogger.Error(err, "Error while getting cluster")
		return err
	}
	instance.SetCluster(&cluster)

	if _, err := instancecertificate.NewReconciler(client, instance).RefreshSecrets(ctx, &cluster); err != nil {
		return fmt.Errorf("error while downloading secrets: %w", err)
	}

	if err := instancestorage.ReconcileWalDirectory(ctx); err != nil {
		return fmt.Errorf("error while reconciling the WAL storage: %w", err)
	}

	if err := fileutils.EnsureDirectoryExists(postgres.GetSocketDir()); err != nil {
		return fmt.Errorf("while creating socket directory: %w", err)
	}

	contextLogger.Info("Searching for failed upgrades")

	var failedDirs []string
	for _, dir := range []string{specs.PgDataPath, specs.PgWalVolumePgWalPath} {
		matches, err := filepath.Glob(dir + "*.failed_*")
		if err != nil {
			return fmt.Errorf("error matching paths: %w", err)
		}
		failedDirs = append(failedDirs, matches...)
	}
	if len(failedDirs) > 0 {
		return fmt.Errorf("found failed upgrade directories: %v", failedDirs)
	}

	contextLogger.Info("Starting the upgrade process")

	newDataDir := fmt.Sprintf("%s-new", specs.PgDataPath)
	var newWalDir *string
	if cluster.ShouldCreateWalArchiveVolume() {
		newWalDir = ptr.To(fmt.Sprintf("%s-new", specs.PgWalVolumePgWalPath))
	}

	contextLogger.Info("Ensuring the new data directory does not exist", "directory", newDataDir)

	if err := os.RemoveAll(newDataDir); err != nil {
		return fmt.Errorf("failed to remove the directory: %w", err)
	}

	if newWalDir != nil {
		contextLogger.Info("Ensuring the new pg_wal directory does not exist", "directory", *newWalDir)
		if err := os.RemoveAll(*newWalDir); err != nil {
			return fmt.Errorf("failed to remove the directory: %w", err)
		}
	}

	// Extract controldata information from the old data directory
	controlData, err := getControlData(ui.oldBinDir, ui.pgData)
	if err != nil {
		return fmt.Errorf("error while getting old data directory control data: %w", err)
	}

	targetVersion, err := cluster.GetPostgresqlMajorVersion()
	if err != nil {
		return fmt.Errorf("error while getting the target version from the cluster object: %w", err)
	}

	contextLogger.Info("Creating data directory", "directory", newDataDir)
	if err := runInitDB(newDataDir, newWalDir, controlData, targetVersion, ui.initdb, ui.initdbArgs); err != nil {
		return fmt.Errorf("error while creating the data directory: %w", err)
	}

	contextLogger.Info("Preparing configuration files", "directory", newDataDir)
	if err := prepareConfigurationFiles(ctx, cluster, newDataDir); err != nil {
		return err
	}

	contextLogger.Info("Checking if we have anything to update")
	// Read pg_version from both the old and new data directories
	oldVersion, err := postgresutils.GetMajorVersionFromPgData(ui.pgData)
	if err != nil {
		return fmt.Errorf("error while reading the old version: %w", err)
	}

	newVersion, err := postgresutils.GetMajorVersionFromPgData(newDataDir)
	if err != nil {
		return fmt.Errorf("error while reading the new version: %w", err)
	}

	if oldVersion == newVersion {
		contextLogger.Info("Versions are the same, no need to upgrade")
		if err := os.RemoveAll(newDataDir); err != nil {
			return fmt.Errorf("failed to remove the directory: %w", err)
		}
		return nil
	}

	// We need to make sure that the permissions are the right ones
	// in some systems they may be messed up even if we fix them before
	_ = fileutils.EnsurePgDataPerms(ui.pgData)
	_ = fileutils.EnsurePgDataPerms(newDataDir)

	contextLogger.Info("Running pg_upgrade")

	if err := ui.runPgUpgrade(newDataDir); err != nil {
		// TODO: in case of failures we should dump the content of the pg_upgrade logs
		return fmt.Errorf("error while running pg_upgrade: %w", err)
	}

	err = moveDataInPlace(ctx, ui.pgData, oldVersion, newDataDir, newWalDir)
	if err != nil {
		contextLogger.Error(err,
			"Error while moving the data in place, saving the new data directory to avoid data loss")

		suffixTimestamp := fileutils.FormatFriendlyTimestamp(time.Now())

		dirToBeSaved := []string{
			newDataDir,
			ui.pgData + ".old",
		}
		if newWalDir != nil {
			dirToBeSaved = append(dirToBeSaved,
				*newWalDir,
				specs.PgWalVolumePgWalPath+".old",
			)
		}

		for _, dir := range dirToBeSaved {
			failedPgDataName := fmt.Sprintf("%s.failed_%s", dir, suffixTimestamp)
			if errInner := moveDirIfExists(ctx, dir, failedPgDataName); errInner != nil {
				contextLogger.Error(errInner, "Error while saving a directory after a failure", "dir", dir)
			}
		}

		return err
	}

	contextLogger.Info("Upgrade completed successfully")

	return nil
}

func getControlData(binDir, pgData string) (map[string]string, error) {
	pgControlDataCmd := exec.Command(path.Join(binDir, "pg_controldata")) // #nosec
	pgControlDataCmd.Env = append(os.Environ(), "PGDATA="+pgData)

	out, err := pgControlDataCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("while executing pg_controldata: %w", err)
	}

	return utils.ParsePgControldataOutput(string(out)), nil
}

func runInitDB(
	destDir string,
	walDir *string,
	pgControlData map[string]string,
	targetMajorVersion int,
	initdb string,
	initdbArgs []string,
) error {
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

	// Extract the WAL segment size from the pg_controldata output
	options, err := tryAddWalSegmentSize(pgControlData, options)
	if err != nil {
		return err
	}

	options, err = tryAddDataChecksums(pgControlData, targetMajorVersion, options)
	if err != nil {
		return err
	}

	options = append(options, initdbArgs...)

	// Certain CSI drivers may add setgid permissions on newly created folders.
	// A default umask is set to attempt to avoid this, by revoking group/other
	// permission bits on the PGDATA
	_ = compatibility.Umask(0o077)

	initdbCmd := exec.Command(initdb, options...) // #nosec
	if err := execlog.RunStreaming(initdbCmd, initdb); err != nil {
		return err
	}

	return nil
}

func tryAddDataChecksums(
	pgControlData utils.PgControlData,
	targetMajorVersion int,
	options []string,
) ([]string, error) {
	dataPageChecksumVersion, err := pgControlData.GetDataPageChecksumVersion()
	if err != nil {
		return nil, err
	}

	if dataPageChecksumVersion != "1" {
		// In postgres 18 we will have to set "--no-data-checksums" if checksums are disabled (they are enabled by default)
		if targetMajorVersion >= 18 {
			return append(options, "--no-data-checksums"), nil
		}
		return options, nil
	}

	return append(options, "--data-checksums"), nil
}

func tryAddWalSegmentSize(pgControlData utils.PgControlData, options []string) ([]string, error) {
	walSegmentSize, err := pgControlData.GetBytesPerWALSegment()
	if err != nil {
		return nil, fmt.Errorf("error while reading the WAL segment size: %w", err)
	}

	param := "--wal-segsize=" + strconv.Itoa(walSegmentSize/(1024*1024))
	return append(options, param), nil
}

func prepareConfigurationFiles(ctx context.Context, cluster apiv1.Cluster, destDir string) error {
	// Always read the custom and override configuration files created by the operator
	_, err := configfile.EnsureIncludes(path.Join(destDir, "postgresql.conf"),
		constants.PostgresqlCustomConfigurationFile,
		constants.PostgresqlOverrideConfigurationFile,
	)
	if err != nil {
		return fmt.Errorf("appending inclusion directives to postgresql.conf file resulted in an error: %w", err)
	}

	// Set `max_slot_wal_keep_size` to the default value because any other value causes an error
	// during pg_upgrade in PostgreSQL 17 before 17.6. The bug has been fixed with the commit
	// https://github.com/postgres/postgres/commit/f36e5774
	tmpCluster := cluster.DeepCopy()
	tmpCluster.Spec.PostgresConfiguration.Parameters["max_slot_wal_keep_size"] = "-1"

	pgMajorVersion, err := postgresutils.GetMajorVersionFromPgData(destDir)
	if err != nil {
		return fmt.Errorf("error while reading the new data directory version: %w", err)
	}
	if pgMajorVersion >= 18 {
		tmpCluster.Spec.PostgresConfiguration.Parameters["idle_replication_slot_timeout"] = "0"
	}

	enabledPluginNamesSet := stringset.From(cluster.GetJobEnabledPluginNames())
	pluginCli, err := pluginClient.NewClient(ctx, enabledPluginNamesSet)
	if err != nil {
		return fmt.Errorf("error while creating the plugin client: %w", err)
	}
	defer pluginCli.Close(ctx)

	ctx = pluginClient.SetPluginClientInContext(ctx, pluginCli)
	ctx = cluster.SetInContext(ctx)

	newInstance := postgres.Instance{PgData: destDir}
	if _, err := newInstance.RefreshConfigurationFilesFromCluster(
		ctx,
		tmpCluster,
		false,
		cnpgiPostgres.OperationType_TYPE_UPGRADE,
	); err != nil {
		return fmt.Errorf("error while creating the configuration files for new datadir %q: %w", destDir, err)
	}

	if _, err := newInstance.RefreshPGIdent(ctx, nil); err != nil {
		return fmt.Errorf("error while creating the pg_ident.conf file for new datadir %q: %w", destDir, err)
	}

	// Create a stub for the configuration file
	// to be filled during the real startup of this instance
	err = fileutils.CreateEmptyFile(path.Join(destDir, constants.PostgresqlOverrideConfigurationFile))
	if err != nil {
		return fmt.Errorf("creating the operator managed configuration file '%v' resulted in an error: %w",
			constants.PostgresqlOverrideConfigurationFile, err)
	}

	return nil
}

func (ui upgradeInfo) runPgUpgrade(
	newDataDir string,
) error {
	args := make([]string, 0, 9+len(ui.pgUpgradeArgs))
	args = append(args,
		"--link",
		"--username", "postgres",
		"--old-bindir", ui.oldBinDir,
		"--old-datadir", ui.pgData,
		"--new-datadir", newDataDir,
	)
	args = append(args, ui.pgUpgradeArgs...)

	// Run the pg_upgrade command
	cmd := exec.Command(ui.pgUpgrade, args...) // #nosec
	cmd.Dir = newDataDir
	if err := execlog.RunStreaming(cmd, path.Base(ui.pgUpgrade)); err != nil {
		return fmt.Errorf("error while running %q: %w", cmd, err)
	}

	return nil
}

func moveDataInPlace(
	ctx context.Context,
	pgData string,
	oldMajor int,
	newDataDir string,
	newWalDir *string,
) error {
	contextLogger := log.FromContext(ctx)

	contextLogger.Info("Cleaning up the new data directory")
	if err := os.RemoveAll(path.Join(newDataDir, "delete_old_cluster.sh")); err != nil {
		return fmt.Errorf("error while removing the delete_old_cluster.sh script: %w", err)
	}

	contextLogger.Info("Moving the old data directory")
	if err := os.Rename(pgData, pgData+".old"); err != nil {
		return fmt.Errorf("error while moving the old data directory: %w", err)
	}

	if newWalDir != nil {
		contextLogger.Info("Moving the old pg_wal directory")
		if err := os.Rename(specs.PgWalVolumePgWalPath, specs.PgWalVolumePgWalPath+".old"); err != nil {
			return fmt.Errorf("error while moving the old pg_wal directory: %w", err)
		}
	}

	contextLogger.Info("Moving the new data directory in place")
	if err := os.Rename(newDataDir, pgData); err != nil {
		return fmt.Errorf("error while moving the new data directory: %w", err)
	}

	if newWalDir != nil {
		contextLogger.Info("Moving the new pg_wal directory in place")
		if err := os.Rename(*newWalDir, specs.PgWalVolumePgWalPath); err != nil {
			return fmt.Errorf("error while moving the pg_wal directory content: %w", err)
		}
		if err := fileutils.RemoveFile(specs.PgWalPath); err != nil {
			return fmt.Errorf("error while removing the symlink to pg_wal: %w", err)
		}
		if err := os.Symlink(specs.PgWalVolumePgWalPath, specs.PgWalPath); err != nil {
			return fmt.Errorf("error while creating the symlink to pg_wal: %w", err)
		}
	}

	contextLogger.Info("Removing the old data directory and pg_wal directory")
	if err := os.RemoveAll(pgData + ".old"); err != nil {
		return fmt.Errorf("error while removing the old data directory: %w", err)
	}
	if err := os.RemoveAll(specs.PgWalVolumePgWalPath + ".old"); err != nil {
		return fmt.Errorf("error while removing the old pg_wal directory: %w", err)
	}

	contextLogger.Info("Cleaning up the previous version directory from tablespaces")
	if err := removeMatchingPaths(ctx,
		path.Join(pgData, "pg_tblspc", "*", fmt.Sprintf("PG_%v_*", oldMajor))); err != nil {
		return fmt.Errorf("error while removing the old tablespaces directories: %w", err)
	}

	return nil
}

func removeMatchingPaths(ctx context.Context, pattern string) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("Removing matching paths", "pattern", pattern)

	// Find all matching paths
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("error matching paths: %w", err)
	}

	// Iterate through the matches and remove each
	for _, match := range matches {
		contextLogger.Info("Removing path", "path", match)
		err := os.RemoveAll(match)
		if err != nil {
			return fmt.Errorf("failed to remove %s: %w", match, err)
		}
	}

	return nil
}

func moveDirIfExists(ctx context.Context, oldPath string, newPath string) error {
	contextLogger := log.FromContext(ctx)
	if _, errExists := os.Stat(oldPath); !os.IsNotExist(errExists) {
		contextLogger.Info("Moving directory", "oldPath", oldPath, "newPath", newPath)
		err := os.Rename(oldPath, newPath)
		if err != nil {
			return err
		}
	}

	return nil
}
