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

// Package postgres contains the function about starting up,
// shutting down and managing a PostgreSQL instance. These functions
// are primarily used by the instance manager
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"time"

	"github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/fileutils/compatibility"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	"github.com/jackc/pgx/v5"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logicalimport"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system"
)

type connectionProvider interface {
	// GetSuperUserDB returns the superuser database connection
	GetSuperUserDB() (*sql.DB, error)
	// GetTemplateDB returns the template database connection
	GetTemplateDB() (*sql.DB, error)
	// ConnectionPool returns the connection pool for this instance
	ConnectionPool() pool.Pooler
}

// dbTask represents a database initialization task that can be executed in parallel
type dbTask struct {
	name     string
	db       *sql.DB
	queries  []string
	refsPath string
}

// InitInfo contains all the info needed to bootstrap a new PostgreSQL instance
type InitInfo struct {
	// The data directory where to generate the new cluster
	PgData string

	// the data directory where to store the WAL
	PgWal string

	// The name of the database to be generated for the applications
	ApplicationDatabase string

	// The name of the role to be generated for the applications
	ApplicationUser string

	// The parent node, used to fill primary_conninfo
	ParentNode string

	// The current node, used to fill application_name
	PodName string

	// The cluster name to assign to
	ClusterName string

	// The namespace where the cluster will be installed
	Namespace string

	// The list options that should be passed to initdb to
	// create the cluster
	InitDBOptions []string

	// The list of queries to be executed just after having
	// configured a new instance
	PostInitSQL []string

	// The list of queries to be executed just after having
	// the application database created
	PostInitApplicationSQL []string

	// The list of queries to be executed inside the template1
	// database just after having configured a new instance
	PostInitTemplateSQL []string

	// Whether it is a temporary instance that will never contain real data.
	Temporary bool

	// PostInitApplicationSQLRefsFolder is the folder which contains a bunch
	// of SQL files to be executed inside the application database right after
	// having configured a new instance
	PostInitApplicationSQLRefsFolder string

	// PostInitSQLRefsFolder is the folder which contains a bunch of SQL files
	// to be executed inside the `postgres` database right after having configured a new instance
	PostInitSQLRefsFolder string

	// PostInitTemplateSQLRefsFolder is the folder which contains a bunch of SQL files
	// to be executed inside the `template1` database right after having configured a new instance
	PostInitTemplateSQLRefsFolder string

	// BackupLabelFile holds the content returned by pg_stop_backup. Needed for a hot backup restore
	BackupLabelFile []byte

	// TablespaceMapFile holds the content returned by pg_stop_backup. Needed for a hot backup restore
	TablespaceMapFile []byte
}

// EnsureTargetDirectoriesDoNotExist ensures that the target data and WAL directories do not exist.
// This is a safety check we do before initializing a new instance.
//
// If the PGDATA directory already exists and contains a valid PostgreSQL control file,
// the function moves the contents to uniquely named directories.
// If no valid control file is found, the function assumes the directory is the result of
// a failed initialization attempt and removes it.
//
// By moving rather than deleting the existing data, we use more disk space than necessary.
// However, this approach is justified for two reasons:
//
//  1. The PostgreSQL control file is the last file written by pg_basebackup.
//     So the only chance to trigger this protection is if the "join" Pod is interrupted
//     shortly after writing the control file but before the Pod terminates.
//     This is a very short time window, and it is extremely unlikely that it happens.
//
//  2. If the PGDATA directory wasn't created by us, renaming preserves potentially
//     important user data. This is particularly relevant when using static provisioning
//     of PersistentVolumeClaims (PVCs), as it prevents accidental overwriting of a valid
//     data directory that may exist in the PersistentVolumes (PVs).
func (info InitInfo) EnsureTargetDirectoriesDoNotExist(ctx context.Context) error {
	contextLogger := log.FromContext(ctx).WithValues("pgdata", info.PgData)

	pgDataExists, err := fileutils.FileExists(info.PgData)
	if err != nil {
		contextLogger.Error(err, "Error while checking for an existing data directory")
		return fmt.Errorf("while verifying if the data directory exists: %w", err)
	}

	pgWalExists := false
	if info.PgWal != "" {
		if pgWalExists, err = fileutils.FileExists(info.PgWal); err != nil {
			contextLogger.Error(err, "Error while checking for an existing WAL directory")
			return fmt.Errorf("while verifying if the WAL directory exists: %w", err)
		}
	}

	if !pgDataExists && !pgWalExists {
		return nil
	}

	out, err := info.GetInstance(nil).GetPgControldata()
	if err == nil {
		contextLogger.Info("pg_controldata check on existing directory succeeded, renaming the folders", "out", out)
		return info.renameExistingTargetDataDirectories(ctx, pgWalExists)
	}

	contextLogger.Info("pg_controldata check on existing directory failed, cleaning up folders", "err", err, "out", out)
	return info.removeExistingTargetDataDirectories(ctx, pgDataExists, pgWalExists)
}

func (info InitInfo) removeExistingTargetDataDirectories(ctx context.Context, pgDataExists, pgWalExists bool) error {
	contextLogger := log.FromContext(ctx).WithValues("pgdata", info.PgData, "pgwal", info.PgWal)

	if pgDataExists {
		contextLogger.Info("cleaning up existing data directory")
		if err := fileutils.RemoveDirectory(info.PgData); err != nil {
			contextLogger.Error(err, "error while cleaning up existing data directory")
			return err
		}
	}

	if pgWalExists {
		contextLogger.Info("cleaning up existing WAL directory")
		if err := fileutils.RemoveDirectory(info.PgWal); err != nil {
			contextLogger.Error(err, "error while cleaning up existing WAL directory")
			return err
		}
	}

	return nil
}

func (info InitInfo) renameExistingTargetDataDirectories(ctx context.Context, pgWalExists bool) error {
	contextLogger := log.FromContext(ctx).WithValues("pgdata", info.PgData, "pgwal", info.PgWal)

	suffixTimestamp := fileutils.FormatFriendlyTimestamp(time.Now())

	pgdataNewName := fmt.Sprintf("%s_%s", info.PgData, suffixTimestamp)
	contextLogger = contextLogger.WithValues()

	contextLogger.Info("renaming the data directory", "pgdataNewName", pgdataNewName)
	if err := os.Rename(info.PgData, pgdataNewName); err != nil {
		contextLogger.Error(err, "error while renaming existing data directory",
			"pgdataNewName", pgdataNewName)
		return fmt.Errorf("while renaming existing data directory: %w", err)
	}

	if pgWalExists {
		pgwalNewName := fmt.Sprintf("%s_%s", info.PgWal, suffixTimestamp)

		contextLogger.Info("renaming the WAL directory", "pgwalNewName", pgwalNewName)
		if err := os.Rename(info.PgWal, pgwalNewName); err != nil {
			contextLogger.Error(err, "error while renaming existing WAL directory")
			return fmt.Errorf("while renaming existing WAL directory: %w", err)
		}
	}

	return nil
}

// CreateDataDirectory creates a new data directory given the configuration
func (info InitInfo) CreateDataDirectory() error {
	// Invoke initdb to generate a data directory
	options := []string{
		"--username",
		"postgres",
		"-D",
		info.PgData,
	}

	// If temporary instance disable fsync on creation
	if info.Temporary {
		options = append(options, "--no-sync")
	}

	if info.PgWal != "" {
		options = append(options, "--waldir", info.PgWal)
	}
	// Add custom initdb options from the user
	options = append(options, info.InitDBOptions...)

	log.Info("Creating new data directory",
		"pgdata", info.PgData,
		"initDbOptions", options)

	// Certain CSI drivers may add setgid permissions on newly created folders.
	// A default umask is set to attempt to avoid this, by revoking group/other
	// permission bits on the PGDATA
	_ = compatibility.Umask(0o077)

	initdbCmd := exec.Command(constants.InitdbName, options...) // #nosec
	err := execlog.RunBuffering(initdbCmd, constants.InitdbName)
	if err != nil {
		return fmt.Errorf("error while creating the PostgreSQL instance: %w", err)
	}

	// Always read the custom and override configuration files created by the operator
	_, err = configfile.EnsureIncludes(path.Join(info.PgData, "postgresql.conf"),
		constants.PostgresqlCustomConfigurationFile,
		constants.PostgresqlOverrideConfigurationFile,
	)
	if err != nil {
		return fmt.Errorf("appending inclusion directives to postgresql.conf file resulted in an error: %w", err)
	}

	// Create a stub for the configuration file
	// to be filled during the real startup of this instance
	err = fileutils.CreateEmptyFile(
		path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile))
	if err != nil {
		return fmt.Errorf("creating the operator managed configuration file '%v' resulted in an error: %w",
			constants.PostgresqlCustomConfigurationFile, err)
	}

	// Create a stub for the configuration file
	// to be filled during the real startup of this instance
	err = fileutils.CreateEmptyFile(
		path.Join(info.PgData, constants.PostgresqlOverrideConfigurationFile))
	if err != nil {
		return fmt.Errorf("creating the operator managed configuration file '%v' resulted in an error: %w",
			constants.PostgresqlOverrideConfigurationFile, err)
	}

	return nil
}

// GetInstance gets the PostgreSQL instance which correspond to these init information
func (info InitInfo) GetInstance(cluster *apiv1.Cluster) *Instance {
	postgresInstance := NewInstance()
	postgresInstance.PgData = info.PgData
	postgresInstance.StartupOptions = []string{"listen_addresses='127.0.0.1'"}
	postgresInstance.SetCluster(cluster)
	return postgresInstance
}

// ConfigureNewInstance creates the expected users and databases in a new
// PostgreSQL instance. If any error occurs, we return it
func (info InitInfo) ConfigureNewInstance(instance connectionProvider) error {
	log.Info("Configuring new PostgreSQL instance")

	dbSuperUser, err := instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting superuser database: %w", err)
	}

	if info.ApplicationUser != "" {
		var existsRole bool
		userRow := dbSuperUser.QueryRow("SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = $1",
			info.ApplicationUser)
		if err = userRow.Scan(&existsRole); err != nil {
			return err
		}

		if !existsRole {
			if _, err = dbSuperUser.Exec(fmt.Sprintf(
				"CREATE ROLE %v LOGIN",
				pgx.Identifier{info.ApplicationUser}.Sanitize())); err != nil {
				return err
			}
		}
	}

	// Execute the custom set of init queries for the `postgres` database in parallel with template1
	log.Info("Executing post-init SQL instructions")

	// Collect all database tasks that can run in parallel
	tasks := []dbTask{
		{
			name:     "postgres",
			db:       dbSuperUser,
			queries:  info.PostInitSQL,
			refsPath: info.PostInitSQLRefsFolder,
		},
	}

	// Get template database connection
	dbTemplate, err := instance.GetTemplateDB()
	if err != nil {
		return fmt.Errorf("while getting template database: %w", err)
	}

	tasks = append(tasks, dbTask{
		name:     "template1",
		db:       dbTemplate,
		queries:  info.PostInitTemplateSQL,
		refsPath: info.PostInitTemplateSQLRefsFolder,
	})

	// Execute postgres and template1 tasks in parallel
	if err = info.executeTasksInParallel(tasks); err != nil {
		return err
	}

	filePath := filepath.Join(info.PgData, constants.CheckEmptyWalArchiveFile)
	// We create the check empty wal archive file to tell that we should check if the
	// destination path is empty
	if err := fileutils.CreateEmptyFile(filePath); err != nil {
		return fmt.Errorf("could not create %v file: %w", filePath, err)
	}

	if info.ApplicationUser == "" || info.ApplicationDatabase == "" {
		return nil
	}

	var existsDB bool
	dbRow := dbSuperUser.QueryRow("SELECT COUNT(*) > 0 FROM pg_catalog.pg_database WHERE datname = $1",
		info.ApplicationDatabase)
	if err = dbRow.Scan(&existsDB); err != nil {
		return err
	}

	if existsDB {
		return nil
	}
	_, err = dbSuperUser.Exec(fmt.Sprintf("CREATE DATABASE %v OWNER %v",
		pgx.Identifier{info.ApplicationDatabase}.Sanitize(),
		pgx.Identifier{info.ApplicationUser}.Sanitize()))
	if err != nil {
		return fmt.Errorf("could not create ApplicationDatabase: %w", err)
	}
	appDB, err := instance.ConnectionPool().Connection(info.ApplicationDatabase)
	if err != nil {
		return fmt.Errorf("could not get connection to ApplicationDatabase: %w", err)
	}
	// Execute the custom set of init queries for the application database
	log.Info("executing Application instructions")
	if err = info.executeQueries(appDB, info.PostInitApplicationSQL); err != nil {
		return fmt.Errorf("could not execute init Application queries: %w", err)
	}

	if err = info.executeSQLRefs(appDB, info.PostInitApplicationSQLRefsFolder); err != nil {
		return fmt.Errorf("could not execute post init application SQL refs: %w", err)
	}

	return nil
}

// executeTasksInParallel executes multiple database initialization tasks in parallel
func (info InitInfo) executeTasksInParallel(tasks []dbTask) error {
	type taskResult struct {
		name string
		err  error
	}

	results := make(chan taskResult, len(tasks))

	// Execute each task in a goroutine
	for _, task := range tasks {
		// Capture task in local variable for goroutine
		t := task
		go func() {
			log.Info("Executing post-init SQL for database", "database", t.name)

			// Execute inline queries
			if err := info.executeQueries(t.db, t.queries); err != nil {
				results <- taskResult{name: t.name, err: fmt.Errorf("inline queries: %w", err)}
				return
			}

			// Execute SQL from referenced files
			if err := info.executeSQLRefs(t.db, t.refsPath); err != nil {
				results <- taskResult{name: t.name, err: fmt.Errorf("SQL refs: %w", err)}
				return
			}

			results <- taskResult{name: t.name, err: nil}
		}()
	}

	// Collect results from all tasks
	var errors []error
	for i := 0; i < len(tasks); i++ {
		result := <-results
		if result.err != nil {
			errors = append(errors, fmt.Errorf("database %s: %w", result.name, result.err))
			log.Error(result.err, "Failed to execute post-init SQL", "database", result.name)
		} else {
			log.Info("Successfully executed post-init SQL", "database", result.name)
		}
	}

	// Return all errors if any occurred
	if len(errors) > 0 {
		combinedErr := errors[0]
		for i := 1; i < len(errors); i++ {
			combinedErr = fmt.Errorf("%v; %w", combinedErr, errors[i])
		}
		return fmt.Errorf("parallel execution failed: %w", combinedErr)
	}

	return nil
}

func (info InitInfo) executeSQLRefs(sqlUser *sql.DB, directory string) error {
	if directory == "" {
		return nil
	}

	if err := fileutils.EnsureDirectoryExists(directory); err != nil {
		return fmt.Errorf("could not find directory: %s, err: %w", directory, err)
	}

	files, err := fileutils.GetDirectoryContent(directory)
	if err != nil {
		return fmt.Errorf("could not get directory content from: %s, err: %w",
			directory, err)
	}

	// Sorting ensures that we execute the files in the correct order.
	// We generate the file names by appending a prefix with the number of execution during the volume generation.
	sort.Strings(files)

	for _, file := range files {
		sql, ioErr := fileutils.ReadFile(path.Join(directory, file))
		if ioErr != nil {
			return fmt.Errorf("could not read file: %s, err; %w", file, err)
		}

		if err = info.executeQueries(sqlUser, []string{string(sql)}); err != nil {
			return fmt.Errorf("could not execute queries: %w", err)
		}
	}

	return nil
}

// executeQueries run the set of queries in the provided database connection
func (info InitInfo) executeQueries(sqlUser *sql.DB, queries []string) error {
	if len(queries) == 0 {
		log.Debug("No queries to execute")
		return nil
	}

	for _, sqlQuery := range queries {
		log.Debug("Executing query", "sqlQuery", sqlQuery)
		_, err := sqlUser.Exec(sqlQuery)
		if err != nil {
			return err
		}
	}

	return nil
}

// Bootstrap creates and configures this new PostgreSQL instance
func (info InitInfo) Bootstrap(ctx context.Context) error {
	typedClient, err := management.NewControllerRuntimeClient()
	if err != nil {
		return err
	}

	cluster, err := info.loadCluster(ctx, typedClient)
	if err != nil {
		return err
	}

	enabledPluginNamesSet := stringset.From(cluster.GetJobEnabledPluginNames())
	pluginCli, err := pluginClient.NewClient(ctx, enabledPluginNamesSet)
	if err != nil {
		return fmt.Errorf("error while creating the plugin client: %w", err)
	}
	defer pluginCli.Close(ctx)
	ctx = pluginClient.SetPluginClientInContext(ctx, pluginCli)
	ctx = cluster.SetInContext(ctx)

	coredumpFilter := cluster.GetCoredumpFilter()
	if err := system.SetCoredumpFilter(coredumpFilter); err != nil {
		return err
	}

	err = info.CreateDataDirectory()
	if err != nil {
		return err
	}

	instance := info.GetInstance(cluster)

	// Detect an initdb bootstrap with import
	isImportBootstrap := cluster.Spec.Bootstrap != nil &&
		cluster.Spec.Bootstrap.InitDB != nil &&
		cluster.Spec.Bootstrap.InitDB.Import != nil

	if applied, err := instance.RefreshConfigurationFilesFromCluster(
		ctx,
		cluster,
		true,
		postgres.OperationType_TYPE_INIT,
	); err != nil {
		return fmt.Errorf("while writing the config: %w", err)
	} else if !applied {
		return fmt.Errorf("could not apply the config")
	}

	// Prepare the managed configuration file (override.conf)
	primaryConnInfo := info.GetPrimaryConnInfo()

	if isImportBootstrap {
		// Write a special configuration for the import phase
		if _, err := configurePostgresForImport(ctx, info.PgData); err != nil {
			return fmt.Errorf("while configuring Postgres for import: %w", err)
		}
	} else {
		// Write standard replication configuration
		if _, err = configurePostgresOverrideConfFile(info.PgData, primaryConnInfo, ""); err != nil {
			return fmt.Errorf("while configuring Postgres for replication: %w", err)
		}
	}

	// Configure the instance and run the logical import process
	if err := instance.WithActiveInstance(func() error {
		err = info.ConfigureNewInstance(instance)
		if err != nil {
			return fmt.Errorf("while configuring new instance: %w", err)
		}

		if isImportBootstrap {
			err = executeLogicalImport(ctx, typedClient, instance, cluster)
			if err != nil {
				return fmt.Errorf("while executing logical import: %w", err)
			}
		}

		return nil
	}); err != nil {
		return err
	}

	// In case of import bootstrap, we restore the standard configuration file content
	if isImportBootstrap {
		// Write standard replication configuration
		if _, err = configurePostgresOverrideConfFile(info.PgData, primaryConnInfo, ""); err != nil {
			return fmt.Errorf("while configuring Postgres for replication: %w", err)
		}

		// ... and then run fsync
		if err := info.initdbSyncOnly(ctx); err != nil {
			return fmt.Errorf("while flushing write cache to disk: %w", err)
		}
	}

	return nil
}

func executeLogicalImport(
	ctx context.Context,
	client ctrl.Client,
	instance *Instance,
	cluster *apiv1.Cluster,
) error {
	destinationPool := instance.ConnectionPool()
	defer destinationPool.ShutdownConnections()

	originPool, err := getConnectionPoolerForExternalCluster(ctx, cluster, client, cluster.Namespace)
	if err != nil {
		return err
	}
	defer originPool.ShutdownConnections()

	cloneType := cluster.Spec.Bootstrap.InitDB.Import.Type
	switch cloneType {
	case apiv1.MicroserviceSnapshotType:
		return logicalimport.Microservice(ctx, cluster, destinationPool, originPool)
	case apiv1.MonolithSnapshotType:
		return logicalimport.Monolith(ctx, cluster, destinationPool, originPool)
	default:
		return fmt.Errorf("unrecognized clone type %s", cloneType)
	}
}

func getConnectionPoolerForExternalCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
	client ctrl.Client,
	namespaceOfNewCluster string,
) (*pool.ConnectionPool, error) {
	externalCluster, ok := cluster.ExternalCluster(cluster.Spec.Bootstrap.InitDB.Import.Source.ExternalCluster)
	if !ok {
		return nil, fmt.Errorf("missing external cluster")
	}

	modifiedExternalCluster := externalCluster.DeepCopy()
	delete(modifiedExternalCluster.ConnectionParameters, "dbname")

	sourceDBConnectionString, err := external.ConfigureConnectionToServer(
		ctx,
		client,
		namespaceOfNewCluster,
		modifiedExternalCluster,
	)
	if err != nil {
		return nil, err
	}

	return pool.NewPostgresqlConnectionPool(sourceDBConnectionString), nil
}

// initdbSyncOnly Run initdb with --sync-only option after a database import
func (info InitInfo) initdbSyncOnly(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	// Invoke initdb to generate a data directory
	options := []string{
		"-D",
		info.PgData,
		"--sync-only",
	}
	contextLogger.Info("Running initdb --sync-only", "pgdata", info.PgData)
	initdbCmd := exec.Command(constants.InitdbName, options...) // #nosec
	if err := execlog.RunBuffering(initdbCmd, constants.InitdbName); err != nil {
		return fmt.Errorf("error while running initdb --sync-only: %w", err)
	}
	return nil
}
