/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"

	"github.com/blang/semver"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/execlog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/logpipe"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/pool"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

const (
	postgresName     = "postgres"
	pgCtlName        = "pg_ctl"
	pgRewindName     = "pg_rewind"
	pgBaseBackupName = "pg_basebackup"
	pgIsReady        = "pg_isready"
	pgCtlTimeout     = "40000000" // greater than one year in seconds, big enough to simulate an infinite timeout

	pqPingOk         = 0 // server is accepting connections
	pqPingReject     = 1 // server is alive but rejecting connections
	pqPingNoResponse = 2 // could not establish connection
	pgPingNoAttempt  = 3 // connection not attempted (bad params)
)

var (
	// ErrPgRejectingConnection postgres is alive, but rejecting connections
	ErrPgRejectingConnection = fmt.Errorf("server is alive but rejecting connections")

	// ErrNoConnectionEstablished postgres is alive, but rejecting connections
	ErrNoConnectionEstablished = fmt.Errorf("could not establish connection")

	// In a version string we need only the initial sequence of digits and dots
	versionRegex = regexp.MustCompile(`^[\d.]+`)

	// ErrMalformedServerVersion the version string is not recognised
	ErrMalformedServerVersion = fmt.Errorf("unrecognized server version")
)

// Instance represent a PostgreSQL instance to be executed
// in the current environment
type Instance struct {
	// The data directory
	PgData string

	// The environment variables that will be used to start the instance
	Env []string

	// Command line options to pass to the postgres process, see the
	// '-c' option of pg_ctl for an useful example
	StartupOptions []string

	// Pool of DB connections pointing to every used database
	pool *pool.ConnectionPool

	// The namespace of the k8s object representing this cluster
	Namespace string

	// The name of the Pod where the controller is executing
	PodName string

	// The name of the cluster of which this Pod is belonging
	ClusterName string

	// The sha256 of the config. It is computed on the config string, before
	// adding the PostgreSQL CNPConfigSha256 parameter
	ConfigSha256 string

	// PgCtlTimeoutForPromotion specifies the maximum number of seconds to wait when waiting for promotion to complete
	PgCtlTimeoutForPromotion int32

	// pgVersion is the PostgreSQL version
	pgVersion *semver.Version

	// InstanceManagerIsUpgrading tells is there is an instance manager upgrade in process
	InstanceManagerIsUpgrading bool
}

// RetryUntilServerAvailable is the default retry configuration that is used
// to wait for a successful connection to a certain server
var RetryUntilServerAvailable = wait.Backoff{
	Duration: 5 * time.Second,
	// Steps is declared as an "int", so we are capping
	// to int32 to support ARM-based 32 bit architectures
	Steps: math.MaxInt32,
}

// GetSocketDir gets the name of the directory that will contain
// the Unix socket for the PostgreSQL server. This is detected using
// the PGHOST environment variable or using a default
func GetSocketDir() string {
	socketDir := os.Getenv("PGHOST")
	if socketDir == "" {
		socketDir = postgres.SocketDirectory
	}

	return socketDir
}

// GetServerPort gets the port where the postmaster will be listening
// using the environment variable or, when empty, the default one
func GetServerPort() int {
	pgPort := os.Getenv("PGPORT")
	if pgPort == "" {
		return postgres.ServerPort
	}

	result, err := strconv.Atoi(pgPort)
	if err != nil {
		log.Info("Wrong port number inside the process environment variables",
			"PGPORT", pgPort)
		return postgres.ServerPort
	}

	return result
}

// Startup starts up a PostgreSQL instance and wait for the instance to be
// started
func (instance Instance) Startup() error {
	socketDir := GetSocketDir()
	if err := fileutils.EnsureDirectoryExist(socketDir); err != nil {
		return fmt.Errorf("while creating socket directory: %w", err)
	}

	// start consuming csv logs
	if err := logpipe.Start(); err != nil {
		return err
	}

	options := []string{
		"start",
		"-w",
		"-D", instance.PgData,
		"-o", fmt.Sprintf("-c port=%v -c unix_socket_directories=%v", GetServerPort(), socketDir),
		"-t " + pgCtlTimeout,
	}

	// Add postgres server command line options
	for _, opt := range instance.StartupOptions {
		options = append(options, "-o", "-c "+opt)
	}

	log.Info("Starting up instance", "pgdata", instance.PgData)

	pgCtlCmd := exec.Command(pgCtlName, options...) // #nosec
	pgCtlCmd.Env = instance.Env
	err := execlog.RunStreaming(pgCtlCmd, pgCtlName)
	if err != nil {
		return fmt.Errorf("error starting PostgreSQL instance: %w", err)
	}

	return nil
}

// ShutdownConnections tears down database connections
func (instance *Instance) ShutdownConnections() {
	if instance.pool == nil {
		return
	}
	instance.pool.ShutdownConnections()
}

// Shutdown shuts down a PostgreSQL instance which was previously started
// with Startup
func (instance *Instance) Shutdown() error {
	instance.ShutdownConnections()

	// check instance status
	if !instance.isStatusRunning() {
		return fmt.Errorf("instance is not running")
	}

	options := []string{
		"-D",
		instance.PgData,
		"-m",
		"fast",
		"-w",
		"stop",
	}

	log.Info("Shutting down instance",
		"pgdata", instance.PgData)

	pgCtlCmd := exec.Command(pgCtlName, options...) // #nosec
	err := execlog.RunStreaming(pgCtlCmd, pgCtlName)
	if err != nil {
		return fmt.Errorf("error stopping PostgreSQL instance: %w", err)
	}

	return nil
}

// isStatusRunning checks the status of a running server using pg_ctl status
func (instance *Instance) isStatusRunning() bool {
	options := []string{
		"-D",
		instance.PgData,
		"status",
	}

	pgCtlCmd := exec.Command(pgCtlName, options...) // #nosec
	err := execlog.RunBuffering(pgCtlCmd, pgCtlName)
	return err == nil
}

// Reload makes a certain active instance reload the configuration
func (instance *Instance) Reload() error {
	options := []string{
		"-D",
		instance.PgData,
		"reload",
	}

	log.Info("Requesting configuration reload",
		"pgdata", instance.PgData)

	pgCtlCmd := exec.Command(pgCtlName, options...) // #nosec
	err := execlog.RunStreaming(pgCtlCmd, pgCtlName)
	if err != nil {
		return fmt.Errorf("error requesting configuration reload: %w", err)
	}

	return nil
}

// Run this instance returning an OS process needed
// to control the instance execution
func (instance Instance) Run() (*os.Process, error) {
	process, err := instance.CheckForExistingPostmaster()
	if err != nil {
		return nil, err
	}

	// We have already a postmaster running, let's use this
	//
	// NOTE: following this path the instance manager has no way
	// to reattach the process stdout/stderr. This should not do
	// any harm because PostgreSQL stops writing on stdout/stderr
	// when the logging collector starts.
	if process != nil {
		return process, nil
	}

	// We don't have a postmaster running and we need to create
	// one.

	socketDir := GetSocketDir()
	if err := fileutils.EnsureDirectoryExist(socketDir); err != nil {
		return nil, fmt.Errorf("while creating socket directory: %w", err)
	}

	options := []string{
		"-D", instance.PgData,
	}

	postgresCmd := exec.Command(postgresName, options...) // #nosec
	postgresCmd.Env = instance.Env
	postgresCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	err = execlog.RunStreamingNoWait(postgresCmd, postgresName)
	if err != nil {
		return nil, err
	}

	return postgresCmd.Process, nil
}

// WithActiveInstance execute the internal function while this
// PostgreSQL instance is running
func (instance Instance) WithActiveInstance(inner func() error) error {
	err := instance.Startup()
	if err != nil {
		return fmt.Errorf("while activating instance: %w", err)
	}
	defer func() {
		if err := instance.Shutdown(); err != nil {
			log.Info("Error while deactivating instance", "err", err)
		}
	}()

	return inner()
}

// GetSuperUserDB gets a connection to the super user db "postgres" on this instance
func (instance *Instance) GetSuperUserDB() (*sql.DB, error) {
	return instance.ConnectionPool().Connection("postgres")
}

// GetTemplateDB gets a connection to the template user db "template1" on this instance
func (instance *Instance) GetTemplateDB() (*sql.DB, error) {
	return instance.ConnectionPool().Connection("template1")
}

// GetPgVersion queries the postgres instance to know the current version, parses it and memoize it for future uses
func (instance *Instance) GetPgVersion() (semver.Version, error) {
	// Better not to recompute what we already have
	if instance.pgVersion != nil {
		return *instance.pgVersion, nil
	}

	db, err := instance.GetSuperUserDB()
	if err != nil {
		return semver.Version{}, err
	}

	var versionString string
	row := db.QueryRow("SHOW server_version")
	err = row.Scan(&versionString)
	if err != nil {
		return semver.Version{}, err
	}

	return instance.parseVersion(versionString)
}

// Version could contain more characters than just the version tag,
// e.g. `13.4 (Debian 13.4-4.pgdg100+1)`.
// Therefore, we extract the initial sequence of digits and dots, then we parse it
func (instance *Instance) parseVersion(version string) (semver.Version, error) {
	if versionRegex.MatchString(version) {
		parsedVersion, err := semver.ParseTolerant(versionRegex.FindStringSubmatch(version)[0])
		if err != nil {
			return semver.Version{}, err
		}

		instance.pgVersion = &parsedVersion
		return *instance.pgVersion, nil
	}

	return semver.Version{}, ErrMalformedServerVersion
}

// ConnectionPool gets or initializes the connection pool for this instance
func (instance *Instance) ConnectionPool() *pool.ConnectionPool {
	if instance.pool == nil {
		socketDir := GetSocketDir()
		dsn := fmt.Sprintf(
			"host=%s port=%v user=%v sslmode=disable",
			socketDir,
			GetServerPort(),
			"postgres")

		instance.pool = pool.NewConnectionPool(dsn)
	}

	return instance.pool
}

// IsPrimary check if the data directory belongs to a primary server or to a
// secondary one by looking for a "standby.signal" file inside the data
// directory. IMPORTANT: this method also works when the instance is not
// started up
func (instance *Instance) IsPrimary() (bool, error) {
	result, err := fileutils.FileExists(filepath.Join(instance.PgData, "standby.signal"))
	if err != nil {
		return false, err
	}
	if result {
		return false, nil
	}

	result, err = fileutils.FileExists(filepath.Join(instance.PgData, "recovery.conf"))
	if err != nil {
		return false, err
	}
	if result {
		return false, nil
	}

	return true, nil
}

// Demote demote an existing PostgreSQL instance
func (instance *Instance) Demote() error {
	log.Info("Demoting instance",
		"pgpdata", instance.PgData)

	_, err := UpdateReplicaConfiguration(instance.PgData, instance.ClusterName, instance.PodName)
	return err
}

// WaitForPrimaryAvailable waits until we can connect to the primary
func (instance *Instance) WaitForPrimaryAvailable() error {
	primaryConnInfo := buildPrimaryConnInfo(
		instance.ClusterName+"-rw", instance.PodName) + " dbname=postgres connect_timeout=5"

	log.Info("Waiting for the new primary to be available",
		"primaryConnInfo", primaryConnInfo)

	db, err := sql.Open("postgres", primaryConnInfo)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	return waitForConnectionAvailable(db)
}

// CompleteCrashRecovery temporary starts up the server and wait for it
// to be fully available for queries. This will ensure that the crash recovery
// is fully done.
// Important: this function must be called only when the instance isn't started
func (instance *Instance) CompleteCrashRecovery() error {
	log.Info("Waiting for server to complete crash recovery")

	defer func() {
		instance.ShutdownConnections()
	}()

	return instance.WithActiveInstance(instance.WaitForSuperuserConnectionAvailable)
}

// WaitForSuperuserConnectionAvailable waits until we can connect to this
// instance using the superuser account
func (instance *Instance) WaitForSuperuserConnectionAvailable() error {
	db, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	return waitForConnectionAvailable(db)
}

// waitForConnectionAvailable waits until we can connect to the passed
// sql.DB connection
func waitForConnectionAvailable(db *sql.DB) error {
	errorIsRetryable := func(err error) bool {
		return err != nil
	}

	return retry.OnError(RetryUntilServerAvailable, errorIsRetryable, func() error {
		err := db.Ping()
		if err != nil {
			log.Info("DB not available, will retry", "err", err)
		}
		return err
	})
}

// WaitForConfigReloaded waits until the config has been reloaded
func (instance *Instance) WaitForConfigReloaded() error {
	errorIsRetryable := func(err error) bool {
		return err != nil
	}
	db, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	return retry.OnError(retry.DefaultRetry, errorIsRetryable, func() error {
		var sha string
		row := db.QueryRow(fmt.Sprintf("SHOW %s", postgres.CNPConfigSha256))
		err = row.Scan(&sha)
		if err != nil {
			return err
		}
		if sha != instance.ConfigSha256 {
			return fmt.Errorf("configuration not yet updated: got %s, wanted %s", sha, instance.ConfigSha256)
		}
		return nil
	})
}

// waitForStreamingConnectionAvailable waits until we can connect to the passed
// sql.DB connection using streaming protocol
func waitForStreamingConnectionAvailable(db *sql.DB) error {
	errorIsRetryable := func(err error) bool {
		return err != nil
	}

	return retry.OnError(RetryUntilServerAvailable, errorIsRetryable, func() error {
		result, err := db.Query("IDENTIFY_SYSTEM")
		if err != nil || result.Err() != nil {
			log.Info("DB not available, will retry", "err", err)
			return err
		}
		defer func() {
			innerErr := result.Close()
			if err == nil && innerErr != nil {
				err = innerErr
			}
		}()
		return err
	})
}

// Rewind uses pg_rewind to align this data directory with the contents of the primary node.
// If postgres major version is >= 13, add "--restore-target-wal" option
func (instance *Instance) Rewind(postgresMajorVersion int) error {
	primaryConnInfo := buildPrimaryConnInfo(instance.ClusterName+"-rw", instance.PodName)
	options := []string{
		"-P",
		"--source-server", primaryConnInfo + " dbname=postgres",
		"--target-pgdata", instance.PgData,
	}

	// As PostgreSQL 13 introduces support of restore from the WAL archive in pg_rewind,
	// let’s automatically use it, if possible
	if postgresMajorVersion >= 13 {
		options = append(options, "--restore-target-wal")
	}

	log.Info("Starting up pg_rewind",
		"pgdata", instance.PgData,
		"options", options)

	pgRewindCmd := exec.Command(pgRewindName, options...) // #nosec
	pgRewindCmd.Env = instance.Env
	err := execlog.RunStreaming(pgRewindCmd, pgRewindName)
	if err != nil {
		return fmt.Errorf("error executing pg_rewind: %w", err)
	}

	return nil
}

// PgIsReady gets the status from the pg_isready command
func (instance *Instance) PgIsReady() error {
	// We just use the environment variables we already have
	// to pass the connection parameters
	options := []string{
		"-U", "postgres",
		"-d", "postgres",
		"-q",
	}

	cmd := exec.Command(pgIsReady, options...) // #nosec G204
	if err := cmd.Run(); err != nil {
		return err
	}

	switch code := cmd.ProcessState.ExitCode(); code {
	case pqPingOk:
		return nil
	case pqPingReject:
		return ErrPgRejectingConnection
	case pqPingNoResponse:
		return ErrNoConnectionEstablished
	case pgPingNoAttempt:
		return fmt.Errorf("pg_isready usage error")
	default:
		return fmt.Errorf("unknown exit code: %d", code)
	}
}
