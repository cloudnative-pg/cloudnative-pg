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

package postgres

import (
	"context"
	"database/sql"
	"errors"
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
	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/execlog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logpipe"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

const (
	postgresName      = "postgres"
	pgCtlName         = "pg_ctl"
	pgRewindName      = "pg_rewind"
	pgBaseBackupName  = "pg_basebackup"
	pgIsReady         = "pg_isready"
	pgCtlTimeout      = "40000000" // greater than one year in seconds, big enough to simulate an infinite timeout
	pgControlDataName = "pg_controldata"

	pqPingOk         = 0 // server is accepting connections
	pqPingReject     = 1 // server is alive but rejecting connections
	pqPingNoResponse = 2 // could not establish connection
	pgPingNoAttempt  = 3 // connection not attempted (bad params)
)

// ShutdownMode represent a way to request the postmaster shutdown
type ShutdownMode string

const (
	// ShutdownModeSmart waits for all active clients to disconnect and any online backup to finish.
	// If the server is in hot standby, recovery and streaming replication will be terminated once
	// all clients have disconnected.
	ShutdownModeSmart = "smart"

	// ShutdownModeFast does not wait for clients to disconnect and will terminate an online
	// backup in progress.
	ShutdownModeFast = "fast"

	// ShutdownModeImmediate aborts all server processes immediately, without a clean shutdown.
	ShutdownModeImmediate = "immediate"
)

// ShutdownOptions is the configuration of a shutdown request to PostgreSQL
type ShutdownOptions struct {
	// Mode is the method we require for the shutdown
	Mode ShutdownMode

	// Wait is true whether we want to wait for the shutdown to complete
	Wait bool

	// Timeout is the maximum number of seconds to wait for the shutdown to complete
	// Used only if Wait is true. Defaulted by PostgreSQL to 60 seconds.
	Timeout *int32
}

// DefaultShutdownOptions are the default shutdown options. That is:
// 1. use the "fast" mode
// 2. wait for the operation to succeed
// 3. without setting any explicit timeout (defaulted by PostgreSQL to 60 seconds)
var DefaultShutdownOptions = ShutdownOptions{
	Mode:    ShutdownModeFast,
	Wait:    true,
	Timeout: nil,
}

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

	// The socket directory
	SocketDirectory string

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
	// adding the PostgreSQL CNPGConfigSha256 parameter
	ConfigSha256 string

	// PgCtlTimeoutForPromotion specifies the maximum number of seconds to wait when waiting for promotion to complete
	PgCtlTimeoutForPromotion int32

	// specifies the maximum number of seconds to wait when shutting down for a switchover
	MaxSwitchoverDelay int32

	// pgVersion is the PostgreSQL version
	pgVersion *semver.Version

	// instanceCommandChan is a channel for requesting actions on the instance
	instanceCommandChan chan InstanceCommand

	// InstanceManagerIsUpgrading tells if there is an instance manager upgrade in process
	InstanceManagerIsUpgrading atomic.Bool

	// PgRewindIsRunning tells if there is a `pg_rewind` process running
	PgRewindIsRunning bool

	// MaxStopDelay is the current MaxStopDelay of the cluster
	MaxStopDelay int32

	// canCheckReadiness specifies whether the instance can start being checked for readiness
	// Is set to true before the instance is run and to false once it exits,
	// it's used by the readiness probe to know whether it should be short-circuited
	canCheckReadiness atomic.Bool

	// mightBeUnavailable specifies whether we expect the instance to be down
	mightBeUnavailable atomic.Bool

	// fenced specifies whether fencing is on for the instance
	// fenced entails mightBeUnavailable ( entails as in logical consequence)
	fenced atomic.Bool
}

// IsFenced checks whether the instance is marked as fenced
func (instance *Instance) IsFenced() bool {
	return instance.fenced.Load()
}

// CanCheckReadiness checks whether the instance should be checked for readiness
func (instance *Instance) CanCheckReadiness() bool {
	return instance.canCheckReadiness.Load()
}

// MightBeUnavailable checks whether we expect the instance to be down
func (instance *Instance) MightBeUnavailable() bool {
	return instance.mightBeUnavailable.Load()
}

// SetFencing marks whether the instance is fenced, if enabling, marks also any down to be tolerated
func (instance *Instance) SetFencing(enabled bool) {
	instance.fenced.Store(enabled)
	if enabled {
		instance.SetMightBeUnavailable(true)
	}
}

// SetCanCheckReadiness marks whether the instance should be checked for readiness
func (instance *Instance) SetCanCheckReadiness(enabled bool) {
	instance.canCheckReadiness.Store(enabled)
}

// SetMightBeUnavailable marks whether the instance being down should be tolerated
func (instance *Instance) SetMightBeUnavailable(enabled bool) {
	instance.mightBeUnavailable.Store(enabled)
}

// InstanceCommand are commands for the goroutine managing postgres
type InstanceCommand string

const (
	// RestartSmartFast means the instance has to be restarted by first issuing
	// a smart shutdown and in case it doesn't work, a fast shutdown
	RestartSmartFast InstanceCommand = "RestartSmartFast"

	// FenceOn means the instance has to be restarted by first issuing
	// a smart shutdown and in case it doesn't work, a fast shutdown
	FenceOn InstanceCommand = "FenceOn"

	// FenceOff means the instance has to be restarted by first issuing
	// a smart shutdown and in case it doesn't work, a fast shutdown
	FenceOff InstanceCommand = "FenceOff"

	// ShutDownFastImmediate means the instance has to be shut down by first
	// issuing a fast shut down and in case of errors an immediate one
	ShutDownFastImmediate InstanceCommand = "ShutDownFastImmediate"
)

// NewInstance creates a new Instance object setting the defaults
func NewInstance() *Instance {
	return &Instance{
		SocketDirectory:     postgres.SocketDirectory,
		instanceCommandChan: make(chan InstanceCommand),
	}
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
func (instance *Instance) Startup() error {
	socketDir := GetSocketDir()
	if err := fileutils.EnsureDirectoryExist(socketDir); err != nil {
		return fmt.Errorf("while creating socket directory: %w", err)
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
// with Startup.
// This function will return an error whether PostgreSQL is still up
// after the shutdown request.
func (instance *Instance) Shutdown(options ShutdownOptions) error {
	instance.ShutdownConnections()

	// check instance status
	if !instance.isStatusRunning() {
		return fmt.Errorf("instance is not running")
	}

	pgCtlOptions := []string{
		"-D",
		instance.PgData,
		"-m",
		string(options.Mode),
		"stop",
	}

	if options.Wait {
		pgCtlOptions = append(pgCtlOptions, "-w")
	} else {
		pgCtlOptions = append(pgCtlOptions, "-W")
	}

	if options.Timeout != nil {
		pgCtlOptions = append(pgCtlOptions, "-t", fmt.Sprintf("%v", *options.Timeout))
	}

	log.Info("Shutting down instance",
		"pgdata", instance.PgData,
		"mode", options.Mode,
		"timeout", options.Timeout,
	)

	pgCtlCmd := exec.Command(pgCtlName, pgCtlOptions...) // #nosec
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
func (instance Instance) Run() (*execlog.StreamingCmd, error) {
	process, err := instance.CheckForExistingPostmaster(postgresName)
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
		return execlog.StreamingCmdFromProcess(process), nil
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
	streamingCmd, err := execlog.RunStreamingNoWait(postgresCmd, postgresName)
	if err != nil {
		return nil, err
	}

	return streamingCmd, nil
}

// WithActiveInstance execute the internal function while this
// PostgreSQL instance is running
func (instance Instance) WithActiveInstance(inner func() error) error {
	// Start the CSV logpipe to redirect log to stdout
	ctx, ctxCancel := context.WithCancel(context.Background())
	csvPipe := logpipe.NewLogPipe()

	go func() {
		if err := csvPipe.Start(ctx); err != nil {
			log.Info("csv pipeline encountered an error", "err", err)
		}
	}()

	defer func() {
		ctxCancel()
		csvPipe.GetExitedCondition().Wait()
	}()

	err := instance.Startup()
	if err != nil {
		return fmt.Errorf("while activating instance: %w", err)
	}

	defer func() {
		if err := instance.Shutdown(DefaultShutdownOptions); err != nil {
			log.Info("Error while deactivating instance", "err", err)
		}
	}()

	return inner()
}

// GetSuperUserDB gets a connection to the "postgres" database on this instance
func (instance *Instance) GetSuperUserDB() (*sql.DB, error) {
	return instance.ConnectionPool().Connection("postgres")
}

// GetTemplateDB gets a connection to the "template1" database on this instance
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
	const applicationName = "cnpg-instance-manager"
	if instance.pool == nil {
		socketDir := GetSocketDir()
		dsn := fmt.Sprintf(
			"host=%s port=%v user=%v sslmode=disable application_name=%v",
			socketDir,
			GetServerPort(),
			"postgres",
			applicationName,
		)

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
		row := db.QueryRow(fmt.Sprintf("SHOW %s", postgres.CNPGConfigSha256))
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

// managePgControlFileBackup ensures we have a useful pg_control file all the time
// even if pg_rewind fails for any possible reason.
// One of the possible situations is because of pg_rewind failures could end up
// in global/pg_control been replaced with a zero length file.
func (instance *Instance) managePgControlFileBackup() error {
	pgControlFilePath := filepath.Join(instance.PgData, "global", "pg_control")
	pgControlBackupFilePath := pgControlFilePath + ".old"

	pgControlSize, err := fileutils.GetFileSize(pgControlFilePath)
	if err != nil {
		return fmt.Errorf("while getting pg_control size: %w", err)
	}

	if pgControlSize != 0 {
		// We copy the current pg_control file into the pg_control.old as backup, even if we already know that
		// it was copy before, this could prevent any error in the future.
		err = fileutils.CopyFile(pgControlFilePath, pgControlBackupFilePath)
		if err != nil {
			return fmt.Errorf("while copying pg_control file for backup before pg_rewind: %w", err)
		}
		return os.Chmod(pgControlBackupFilePath, 0o600)
	}

	pgControlBackupExists, err := fileutils.FileExists(pgControlBackupFilePath)
	if err != nil {
		return fmt.Errorf("while checking for pg_control.old: %w", err)
	}

	// If the current pg_control file is zero-size and the pg_control.old file exist
	// we can copy the old pg_control file to the new one
	if pgControlBackupExists {
		err = fileutils.CopyFile(pgControlBackupFilePath, pgControlFilePath)
		if err != nil {
			return fmt.Errorf("while copying old pg_control to new pg_control: %w", err)
		}
		return os.Chmod(pgControlFilePath, 0o600)
	}

	return fmt.Errorf("pg_control file is zero and we don't have a pg_control.old")
}

// removePgControlFileBackup cleans up the pg_control backup after pg_rewind has successfully completed.
func (instance *Instance) removePgControlFileBackup() error {
	pgControlFilePath := filepath.Join(instance.PgData, "global", "pg_control")
	pgControlBackupFilePath := pgControlFilePath + ".old"

	err := os.Remove(pgControlBackupFilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// Rewind uses pg_rewind to align this data directory with the contents of the primary node.
// If postgres major version is >= 13, add "--restore-target-wal" option
func (instance *Instance) Rewind(postgresMajorVersion int) error {
	// Signal the liveness probe that we are running pg_rewind before starting postgres
	instance.PgRewindIsRunning = true
	defer func() {
		instance.PgRewindIsRunning = false
	}()

	instance.LogPgControldata("before pg_rewind")

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

	// Make sure PostgreSQL control file is not empty
	err := instance.managePgControlFileBackup()
	if err != nil {
		return err
	}

	log.Info("Starting up pg_rewind",
		"pgdata", instance.PgData,
		"options", options)

	pgRewindCmd := exec.Command(pgRewindName, options...) // #nosec
	pgRewindCmd.Env = instance.Env
	err = execlog.RunStreaming(pgRewindCmd, pgRewindName)
	if err != nil {
		log.Error(err, "Failed to execute pg_rewind", "options", options)
		return fmt.Errorf("error executing pg_rewind: %w", err)
	}

	// Clean up the pg_control backup after pg_rewind has successfully completed
	err = instance.removePgControlFileBackup()
	if err != nil {
		return err
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

	// Run `pg_isready` which returns 0 if everything is OK.
	// It returns 1 when PostgreSQL is not ready to accept
	// connections but it is starting up (this is a valid
	// condition for example for a standby that is fetching
	// WAL files and trying to reach a consistent state).
	cmd := exec.Command(pgIsReady, options...) // #nosec G204
	err := cmd.Run()

	// Verify that `pg_isready` has been executed correctly.
	// We expect that `pg_isready` returns 0 (err == nil) or another
	// valid exit code such as 1 or 2
	var exitError *exec.ExitError
	if err == nil || errors.As(err, &exitError) {
		switch code := cmd.ProcessState.ExitCode(); code {
		case pqPingOk:
			return nil
		case pqPingReject:
			return ErrPgRejectingConnection
		case pqPingNoResponse:
			return ErrNoConnectionEstablished
		case pgPingNoAttempt:
			return fmt.Errorf("pg_isready usage error: %w", err)
		default:
			return fmt.Errorf("unknown exit code %d: %w", code, err)
		}
	}

	// `pg_isready` had an unexpected failure
	return fmt.Errorf("failure executing %s: %w", pgIsReady, err)
}

// LogPgControldata logs the content of PostgreSQL control data, for debugging and tracing
func (instance *Instance) LogPgControldata(reason string) {
	log.Info("Extracting pg_controldata information", "reason", reason)

	pgControlDataCmd := exec.Command(pgControlDataName)
	pgControlDataCmd.Env = os.Environ()
	pgControlDataCmd.Env = append(pgControlDataCmd.Env, "PGDATA="+instance.PgData)
	err := execlog.RunBuffering(pgControlDataCmd, pgControlDataName)
	if err != nil {
		log.Error(err, "Error printing the control information of this PostgreSQL instance")
	}
}

// GetInstanceCommandChan is the channel where the lifecycle manager will
// wait for the operations requested on the instance
func (instance *Instance) GetInstanceCommandChan() <-chan InstanceCommand {
	return instance.instanceCommandChan
}

// RequestFastImmediateShutdown request the lifecycle manager to shut down
// PostegreSQL using the fast strategy and then the immediate strategy.
func (instance *Instance) RequestFastImmediateShutdown() {
	instance.instanceCommandChan <- ShutDownFastImmediate
}

// RequestAndWaitRestartSmartFast requests the lifecycle manager to
// restart the postmaster, and wait for the postmaster to be restarted
func (instance *Instance) RequestAndWaitRestartSmartFast() error {
	instance.SetMightBeUnavailable(true)
	defer instance.SetMightBeUnavailable(false)
	now := time.Now()
	instance.requestRestartSmartFast()
	err := instance.waitForInstanceRestarted(now)
	if err != nil {
		return fmt.Errorf("while waiting for instance restart: %w", err)
	}

	return nil
}

// requestRestartSmartFast request the lifecycle manager to restart
// the postmaster
func (instance *Instance) requestRestartSmartFast() {
	instance.instanceCommandChan <- RestartSmartFast
}

// RequestFencingOn request the lifecycle manager to shut down postgres and enable fencing
func (instance *Instance) RequestFencingOn() {
	instance.instanceCommandChan <- FenceOn
}

// RequestAndWaitFencingOff will request to remove the fencing
// and wait for the instance to be restarted
func (instance *Instance) RequestAndWaitFencingOff() error {
	defer instance.SetMightBeUnavailable(false)
	now := time.Now()
	instance.requestFencingOff()
	err := instance.waitForInstanceRestarted(now)
	if err != nil {
		return fmt.Errorf("while waiting for instance restart: %w", err)
	}

	// sleep enough for the pod to be ready again
	time.Sleep(2 * specs.ReadinessProbePeriod * time.Second)

	return nil
}

// requestFencingOff request the lifecycle manager to remove the fencing and restart postgres if needed
func (instance *Instance) requestFencingOff() {
	instance.instanceCommandChan <- FenceOff
}

// waitForInstanceRestarted waits until the instance reports being started
// after the given time
func (instance *Instance) waitForInstanceRestarted(after time.Time) error {
	retryOnEveryError := func(err error) bool {
		return true
	}
	return retry.OnError(RetryUntilServerAvailable, retryOnEveryError, func() error {
		db, err := instance.GetSuperUserDB()
		if err != nil {
			return err
		}
		var startTime time.Time
		row := db.QueryRow("SELECT pg_postmaster_start_time()")
		err = row.Scan(&startTime)
		if err != nil {
			return err
		}
		if !startTime.After(after) {
			return fmt.Errorf("instance not yet restarted: %v <= %v", startTime, after)
		}
		return nil
	})
}
