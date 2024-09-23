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

	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/fileutils/compatibility"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logicalimport"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system"
)

const (
	// CheckEmptyWalArchiveFile is the name of the file in the PGDATA that,
	// if present, requires the WAL archiver to check that the backup object
	// store is empty.
	CheckEmptyWalArchiveFile = ".check-empty-wal-archive"
)

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

// CheckTargetDataDirectory ensures that the target data directory does not exist.
// This is a safety check we do before initializing a new instance data directory.
//
// If the PGDATA directory already exists and contains a valid PostgreSQL control file,
// the function moves its contents to a uniquely named directory.
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
func (info InitInfo) CheckTargetDataDirectory(ctx context.Context) error {
	contextLogger := log.FromContext(ctx).WithValues("pgdata", info.PgData)

	pgDataExists, err := fileutils.FileExists(info.PgData)
	if err != nil {
		log.Error(err, "Error while checking for an existing PGData")
		return fmt.Errorf("while verifying is PGDATA exists: %w", err)
	}
	if !pgDataExists {
		// The PGDATA directory doesn't exist. We can definitely
		// write to it
		return nil
	}

	// We've an existing directory. Let's check if this is a real
	// PGDATA directory or not.
	out, err := info.GetInstance().GetPgControldata()
	if err != nil {
		contextLogger.Info("pg_controldata check on existing directory failed, cleaning it up",
			"out", out, "err", err)

		if err := fileutils.RemoveDirectory(info.PgData); err != nil {
			contextLogger.Error(err, "error while cleaning up existing data directory")
			return err
		}

		return nil
	}

	renamedDirectoryName := fmt.Sprintf("%s_%s", info.PgData, fileutils.FormatFriendlyTimestamp(time.Now()))
	contextLogger = contextLogger.WithValues(
		"out", out,
		"newName", renamedDirectoryName,
	)

	contextLogger.Info("pg_controldata check on existing directory succeeded, renaming the folder")
	if err := os.Rename(info.PgData, renamedDirectoryName); err != nil {
		contextLogger.Error(err, "error while renaming existing data directory")
		return fmt.Errorf("while renaming existing data directory: %w", err)
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
func (info InitInfo) GetInstance() *Instance {
	postgresInstance := NewInstance()
	postgresInstance.PgData = info.PgData
	postgresInstance.StartupOptions = []string{"listen_addresses='127.0.0.1'"}
	return postgresInstance
}

// ConfigureNewInstance creates the expected users and databases in a new
// PostgreSQL instance. If any error occurs, we return it
func (info InitInfo) ConfigureNewInstance(instance *Instance) error {
	log.Info("Configuring new PostgreSQL instance")

	dbSuperUser, err := instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting superuser database: %w", err)
	}

	var existsRole bool
	userRow := dbSuperUser.QueryRow("SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = $1",
		info.ApplicationUser)
	err = userRow.Scan(&existsRole)
	if err != nil {
		return err
	}

	if !existsRole {
		_, err = dbSuperUser.Exec(fmt.Sprintf(
			"CREATE ROLE %v LOGIN",
			pgx.Identifier{info.ApplicationUser}.Sanitize()))
		if err != nil {
			return err
		}
	}

	// Execute the custom set of init queries for the `postgres` database
	log.Info("Executing post-init SQL instructions")
	if err = info.executeQueries(dbSuperUser, info.PostInitSQL); err != nil {
		return err
	}
	if err = info.executeSQLRefs(dbSuperUser, info.PostInitSQLRefsFolder); err != nil {
		return fmt.Errorf("could not execute post init application SQL refs: %w", err)
	}

	dbTemplate, err := instance.GetTemplateDB()
	if err != nil {
		return fmt.Errorf("while getting template database: %w", err)
	}
	// Execute the custom set of init queries for the `template1` database
	log.Info("Executing post-init template SQL instructions")
	if err = info.executeQueries(dbTemplate, info.PostInitTemplateSQL); err != nil {
		return fmt.Errorf("could not execute init Template queries: %w", err)
	}
	if err = info.executeSQLRefs(dbTemplate, info.PostInitTemplateSQLRefsFolder); err != nil {
		return fmt.Errorf("could not execute post init application SQL refs: %w", err)
	}
	if info.ApplicationDatabase == "" {
		return nil
	}

	var existsDB bool
	dbRow := dbSuperUser.QueryRow("SELECT COUNT(*) > 0 FROM pg_database WHERE datname = $1", info.ApplicationDatabase)
	err = dbRow.Scan(&existsDB)
	if err != nil {
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

	filePath := filepath.Join(info.PgData, CheckEmptyWalArchiveFile)
	// We create the check empty wal archive file to tell that we should check if the
	// destination path it is empty
	if err := fileutils.CreateEmptyFile(filepath.Clean(filePath)); err != nil {
		return fmt.Errorf("could not create %v file: %w", filePath, err)
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

	coredumpFilter := cluster.GetCoredumpFilter()
	if err := system.SetCoredumpFilter(coredumpFilter); err != nil {
		return err
	}

	err = info.CreateDataDirectory()
	if err != nil {
		return err
	}

	instance := info.GetInstance()

	// Detect an initdb bootstrap with import
	isImportBootstrap := cluster.Spec.Bootstrap != nil &&
		cluster.Spec.Bootstrap.InitDB != nil &&
		cluster.Spec.Bootstrap.InitDB.Import != nil

	if applied, err := instance.RefreshConfigurationFilesFromCluster(cluster, true); err != nil {
		return fmt.Errorf("while writing the config: %w", err)
	} else if !applied {
		return fmt.Errorf("could not apply the config")
	}

	// Prepare the managed configuration file (override.conf)
	primaryConnInfo := info.GetPrimaryConnInfo()
	slotName := cluster.GetSlotNameFromInstanceName(info.PodName)

	if isImportBootstrap {
		// Write a special configuration for the import phase
		if _, err := configurePostgresForImport(ctx, info.PgData); err != nil {
			return fmt.Errorf("while configuring Postgres for import: %w", err)
		}
	} else {
		// Write standard replication configuration
		if _, err = configurePostgresOverrideConfFile(info.PgData, primaryConnInfo, slotName); err != nil {
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
		/// Write standard replication configuration
		if _, err = configurePostgresOverrideConfFile(info.PgData, primaryConnInfo, slotName); err != nil {
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
