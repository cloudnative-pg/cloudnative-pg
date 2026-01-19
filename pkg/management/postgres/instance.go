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

package postgres

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blang/semver"
	"github.com/cloudnative-pg/machinery/pkg/envmap"
	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/fileutils/compatibility"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logpipe"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/replicaclusterswitch/conditions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	// this is needed to correctly open the sql connection with the pgx driver
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
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

// GetPostgresExecutableName returns the name of the PostgreSQL executable
func GetPostgresExecutableName() string {
	if name := os.Getenv("POSTGRES_NAME"); name != "" {
		return name
	}

	return "postgres"
}

// shutdownMode represent a way to request the postmaster shutdown
type shutdownMode string

const (
	// shutdownModeSmart waits for all active clients to disconnect and any online backup to finish.
	// If the server is in hot standby, recovery and streaming replication will be terminated once
	// all clients have disconnected.
	shutdownModeSmart = "smart"

	// shutdownModeFast does not wait for clients to disconnect and will terminate an online
	// backup in progress.
	shutdownModeFast = "fast"

	// shutdownModeImmediate aborts all server processes immediately, without a clean shutdown.
	shutdownModeImmediate = "immediate"
)

// pgControlFileBackupExtension is the extension used to back up the pg_control file
const pgControlFileBackupExtension = ".old"

// shutdownOptions is the configuration of a shutdown request to PostgreSQL
type shutdownOptions struct {
	// Mode is the method we require for the shutdown
	Mode shutdownMode

	// Wait is true whether we want to wait for the shutdown to complete
	Wait bool

	// Timeout is the maximum number of seconds to wait for the shutdown to complete
	// Used only if Wait is true. Defaulted by PostgreSQL to 60 seconds.
	Timeout *int32
}

// defaultShutdownOptions are the default shutdown options. That is:
// 1. use the "fast" mode
// 2. wait for the operation to succeed
// 3. without setting any explicit timeout (defaulted by PostgreSQL to 60 seconds)
var defaultShutdownOptions = shutdownOptions{
	Mode:    shutdownModeFast,
	Wait:    true,
	Timeout: nil,
}

var (
	// ErrPgRejectingConnection postgres is alive, but rejecting connections
	ErrPgRejectingConnection = fmt.Errorf("server is alive but rejecting connections")

	// ErrNoConnectionEstablished postgres is alive, but rejecting connections
	ErrNoConnectionEstablished = fmt.Errorf("could not establish connection")
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

	// Pool of DB connections pointing to primary instance
	primaryPool *pool.ConnectionPool

	// The namespace of the k8s object representing this cluster
	namespace string

	// The name of the Pod where the controller is executing
	podName string

	// The name of the cluster this instance belongs in
	clusterName string

	// The sha256 of the config. It is computed on the config string, before
	// adding the PostgreSQL CNPGConfigSha256 parameter
	ConfigSha256 string

	// pgVersion is the PostgreSQL version
	pgVersion *semver.Version

	// instanceCommandChan is a channel for requesting actions on the instance
	instanceCommandChan chan InstanceCommand

	// SessionID is a unique identifier generated at instance manager startup.
	// This ID changes on every instance manager restart, including reboots that don't
	// change the container ID. Used to detect if the instance manager was restarted
	// during long-running operations like backups.
	SessionID string

	// InstanceManagerIsUpgrading tells if there is an instance manager upgrade in process
	InstanceManagerIsUpgrading atomic.Bool

	// PgRewindIsRunning tells if there is a `pg_rewind` process running
	PgRewindIsRunning bool

	// canCheckReadiness specifies whether the instance can start being checked for readiness
	// Is set to true before the instance is run and to false once it exits,
	// it's used by the readiness probe to know whether it should be short-circuited
	canCheckReadiness atomic.Bool

	// mightBeUnavailable specifies whether we expect the instance to be down
	mightBeUnavailable atomic.Bool

	// fenced specifies whether fencing is on for the instance
	// fenced entails mightBeUnavailable ( entails as in logical consequence)
	fenced atomic.Bool

	// slotsReplicatorChan is used to send replication slot configuration to the slot replicator
	slotsReplicatorChan chan *apiv1.ReplicationSlotsConfiguration

	// roleSynchronizerChan is used to send managed role configuration to the role synchronizer
	roleSynchronizerChan chan *apiv1.ManagedConfiguration

	// tablespaceSynchronizerChan is used to send tablespace configuration to the tablespace synchronizer
	tablespaceSynchronizerChan chan map[string]apiv1.TablespaceConfiguration

	// StatusPortTLS enables TLS on the status port used to communicate with the operator
	StatusPortTLS bool

	// MetricsPortTLS enables TLS on the port used to publish metrics over HTTP/HTTPS
	MetricsPortTLS bool

	serverCertificateHandler serverCertificateHandler

	// Cluster is the cluster this instance belongs to.
	// Guaranteed non-nil: initialized as empty in NewInstance(),
	// then set to the actual cluster by the reconciler.
	Cluster *apiv1.Cluster
}

type serverCertificateHandler struct {
	operationInProgress sync.Mutex

	// ServerCertificate is the certificate we use to serve https connections
	ServerCertificate *tls.Certificate
}

// GetServerCertificate returns the server certificate for the instance
func (instance *Instance) GetServerCertificate() *tls.Certificate {
	instance.serverCertificateHandler.operationInProgress.Lock()
	defer instance.serverCertificateHandler.operationInProgress.Unlock()

	return instance.serverCertificateHandler.ServerCertificate
}

// SetServerCertificate sets the server certificate for the instance
func (instance *Instance) SetServerCertificate(cert *tls.Certificate) {
	instance.serverCertificateHandler.operationInProgress.Lock()
	defer instance.serverCertificateHandler.operationInProgress.Unlock()

	instance.serverCertificateHandler.ServerCertificate = cert
}

// SetPostgreSQLAutoConfWritable allows or deny writes to the
// `postgresql.auto.conf` file in PGDATA
func (instance *Instance) SetPostgreSQLAutoConfWritable(writeable bool) error {
	autoConfFileName := path.Join(instance.PgData, "postgresql.auto.conf")

	mode := fs.FileMode(0o600)
	if !writeable {
		mode = 0o400
	}
	return os.Chmod(autoConfFileName, mode)
}

// IsReady runs PgIsReady
func (instance *Instance) IsReady() error {
	return PgIsReady()
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

// RequiresDesignatedPrimaryTransition checks if this instance is a primary
// that needs to become a designated primary in a replica cluster
func (instance *Instance) RequiresDesignatedPrimaryTransition() bool {
	if !instance.Cluster.IsReplica() {
		return false
	}

	if !conditions.IsDesignatedPrimaryTransitionRequested(instance.Cluster) {
		return false
	}

	if !instance.IsFenced() && !instance.MightBeUnavailable() {
		return false
	}

	// Check if this pod was the primary before the transition started.
	// We use CurrentPrimary instead of IsPrimary() because IsPrimary()
	// checks for the absence of standby.signal, which gets created during
	// the transition by RefreshReplicaConfiguration(). Using CurrentPrimary
	// keeps the sentinel true throughout the transition, allowing retries
	// if the status update fails due to optimistic locking conflicts.
	return instance.Cluster.Status.CurrentPrimary == instance.GetPodName()
}

// CheckHasDiskSpaceForWAL checks if we have enough disk space to store two WAL files,
// and returns true if we have free disk space for 2 WAL segments, false otherwise
func (instance *Instance) CheckHasDiskSpaceForWAL(ctx context.Context) (bool, error) {
	pgControlDataString, err := instance.GetPgControldata()
	if err != nil {
		return false, fmt.Errorf("while running pg_controldata to detect WAL segment size: %w", err)
	}

	pgControlData := utils.ParsePgControldataOutput(pgControlDataString)
	walSegmentSize, err := pgControlData.GetBytesPerWALSegment()
	if err != nil {
		return false, err
	}

	walDirectory := path.Join(instance.PgData, pgWalDirectory)
	return fileutils.NewDiskProbe(walDirectory).HasStorageAvailable(ctx, walSegmentSize)
}

// SetMightBeUnavailable marks whether the instance being down should be tolerated
func (instance *Instance) SetMightBeUnavailable(enabled bool) {
	instance.mightBeUnavailable.Store(enabled)
}

// ConfigureSlotReplicator sends the configuration to the slot replicator
func (instance *Instance) ConfigureSlotReplicator(config *apiv1.ReplicationSlotsConfiguration) {
	go func() {
		instance.slotsReplicatorChan <- config
	}()
}

// SlotReplicatorChan returns the communication channel to the slot replicator
func (instance *Instance) SlotReplicatorChan() <-chan *apiv1.ReplicationSlotsConfiguration {
	return instance.slotsReplicatorChan
}

// TriggerRoleSynchronizer sends the configuration to the role synchronizer
func (instance *Instance) TriggerRoleSynchronizer(config *apiv1.ManagedConfiguration) {
	go func() {
		instance.roleSynchronizerChan <- config
	}()
}

// RoleSynchronizerChan returns the communication channel to the role synchronizer
func (instance *Instance) RoleSynchronizerChan() <-chan *apiv1.ManagedConfiguration {
	return instance.roleSynchronizerChan
}

// TriggerTablespaceSynchronizer sends the configuration to the tablespace synchronizer
func (instance *Instance) TriggerTablespaceSynchronizer(config map[string]apiv1.TablespaceConfiguration) {
	go func() {
		instance.tablespaceSynchronizerChan <- config
	}()
}

// TablespaceSynchronizerChan returns the communication channel to the tablespace synchronizer
func (instance *Instance) TablespaceSynchronizerChan() <-chan map[string]apiv1.TablespaceConfiguration {
	return instance.tablespaceSynchronizerChan
}

// VerifyPgDataCoherence checks the PGDATA is correctly configured in terms
// of file rights and users
func (instance *Instance) VerifyPgDataCoherence(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	contextLogger.Debug("Checking PGDATA coherence")

	if err := fileutils.EnsurePgDataPerms(instance.PgData); err != nil {
		return err
	}

	// creates a bare pg_ident.conf that only grants local access
	_, err := instance.RefreshPGIdent(ctx, nil)
	return err
}

// InstanceCommand are commands for the goroutine managing postgres
type InstanceCommand string

const (
	// restartSmartFast means the instance has to be restarted by first issuing
	// a smart shutdown and in case it doesn't work, a fast shutdown
	restartSmartFast InstanceCommand = "RestartSmartFast"

	// fenceOn means the instance has to be restarted by first issuing
	// a smart shutdown and in case it doesn't work, a fast shutdown
	fenceOn InstanceCommand = "FenceOn"

	// fenceOff means the instance has to be restarted by first issuing
	// a smart shutdown and in case it doesn't work, a fast shutdown
	fenceOff InstanceCommand = "FenceOff"

	// shutDownFastImmediate means the instance has to be shut down by first
	// issuing a fast shut down and in case of errors an immediate one
	shutDownFastImmediate InstanceCommand = "ShutDownFastImmediate"
)

// NewInstance creates a new Instance object setting the defaults
func NewInstance() *Instance {
	return &Instance{
		SocketDirectory:            postgres.SocketDirectory,
		instanceCommandChan:        make(chan InstanceCommand),
		slotsReplicatorChan:        make(chan *apiv1.ReplicationSlotsConfiguration),
		roleSynchronizerChan:       make(chan *apiv1.ManagedConfiguration),
		tablespaceSynchronizerChan: make(chan map[string]apiv1.TablespaceConfiguration),
		SessionID:                  string(uuid.NewUUID()),
		// Initialize with an empty cluster to provide safe defaults for
		// timeout/delay getters before the reconciler sets the real cluster
		Cluster: &apiv1.Cluster{},
	}
}

// WithNamespace specifies the namespace for this Instance
func (instance *Instance) WithNamespace(namespace string) *Instance {
	instance.namespace = namespace
	return instance
}

// WithPodName specifies the pod name for this Instance
func (instance *Instance) WithPodName(podName string) *Instance {
	instance.podName = podName
	return instance
}

// WithClusterName specifies the name of the cluster this Instance belongs to
func (instance *Instance) WithClusterName(clusterName string) *Instance {
	instance.clusterName = clusterName
	return instance
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
	if err := fileutils.EnsureDirectoryExists(socketDir); err != nil {
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

	log.Info("Starting up instance", "pgdata", instance.PgData, "options", options)

	// We need to make sure that the permissions are the right ones
	// in some systems they may be messed up even if we fix them before
	if err := fileutils.EnsurePgDataPerms(instance.PgData); err != nil {
		return err
	}

	pgCtlCmd := exec.Command(pgCtlName, options...) // #nosec
	pgCtlCmd.Env = instance.buildPostgresEnv()
	err := execlog.RunStreaming(pgCtlCmd, pgCtlName)
	if err != nil {
		return fmt.Errorf("error starting PostgreSQL instance: %w", err)
	}

	return nil
}

// ShutdownConnections tears down database connections
func (instance *Instance) ShutdownConnections() {
	if instance.pool != nil {
		instance.pool.ShutdownConnections()
	}
	if instance.primaryPool != nil {
		instance.primaryPool.ShutdownConnections()
	}
}

// Shutdown stops a PostgreSQL instance that was previously started with Startup.
// Before shutting down, it attempts to execute a CHECKPOINT.
// The function returns an error if PostgreSQL remains running after the shutdown request.
func (instance *Instance) Shutdown(ctx context.Context, options shutdownOptions) error {
	contextLogger := log.FromContext(ctx)

	// check instance status
	if !instance.isStatusRunning() {
		return fmt.Errorf("instance is not running")
	}

	instance.tryCheckpointBeforeShutdown(ctx, options.Mode)
	instance.ShutdownConnections()

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

	contextLogger.Info("Shutting down instance",
		"pgdata", instance.PgData,
		"mode", options.Mode,
		"timeout", options.Timeout,
		"pgCtlOptions", pgCtlOptions,
	)

	pgCtlCmd := exec.Command(pgCtlName, pgCtlOptions...) // #nosec
	err := execlog.RunStreaming(pgCtlCmd, pgCtlName)
	if err != nil {
		return fmt.Errorf("error stopping PostgreSQL instance: %w", err)
	}

	return nil
}

// TryShuttingDownSmartFast first attempts to shut down the instance using the "smart" mode,
// which is preceded by a CHECKPOINT. If this fails or the specified timeout expires,
// it issues an "fast" shutdown request and waits for completion.
func (instance *Instance) TryShuttingDownSmartFast(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	var err error

	smartTimeout := instance.Cluster.GetSmartShutdownTimeout()
	maxStopDelay := instance.Cluster.GetMaxStopDelay()
	if maxStopDelay <= smartTimeout {
		contextLogger.Warning("Ignoring maxStopDelay <= smartShutdownTimeout",
			"smartShutdownTimeout", smartTimeout,
			"maxStopDelay", maxStopDelay,
		)
		smartTimeout = 0
	}

	if smartTimeout > 0 {
		contextLogger.Info("Requesting smart shutdown of the PostgreSQL instance")
		err = instance.Shutdown(
			ctx,
			shutdownOptions{
				Mode:    shutdownModeSmart,
				Wait:    true,
				Timeout: &smartTimeout,
			},
		)
		if err != nil {
			contextLogger.Warning("Error while handling the smart shutdown request", "err", err)
		}
	}

	if err != nil || smartTimeout == 0 {
		contextLogger.Info("Requesting fast shutdown of the PostgreSQL instance")
		err = instance.Shutdown(ctx,
			shutdownOptions{
				Mode: shutdownModeFast,
				Wait: true,
			},
		)
	}
	if err != nil {
		contextLogger.Error(err, "Error while shutting down the PostgreSQL instance")
		return err
	}

	contextLogger.Info("PostgreSQL instance shut down")
	return nil
}

// TryShuttingDownFastImmediate first attempts to shut down the instance using the "fast" mode,
// which is preceded by a CHECKPOINT. If this fails or the specified timeout expires,
// it issues an "immediate" shutdown request and waits for completion.
// Note: an immediate shutdown may lead to data loss.
func (instance *Instance) TryShuttingDownFastImmediate(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	contextLogger.Info("Requesting fast shutdown of the PostgreSQL instance")
	maxSwitchoverDelay := instance.Cluster.GetMaxSwitchoverDelay()
	err := instance.Shutdown(
		ctx,
		shutdownOptions{
			Mode:    shutdownModeFast,
			Wait:    true,
			Timeout: &maxSwitchoverDelay,
		},
	)
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		contextLogger.Info("Graceful shutdown failed. Issuing immediate shutdown",
			"exitCode", exitError.ExitCode())
		err = instance.Shutdown(ctx,
			shutdownOptions{
				Mode: shutdownModeImmediate,
				Wait: true,
			},
		)
	}
	return err
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
func (instance *Instance) Reload(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	options := []string{
		"-D",
		instance.PgData,
		"reload",
	}

	contextLogger.Info(
		"Requesting configuration reload",
		"pgdata", instance.PgData,
		"pgCtlOptions", options)

	// Need to reload certificates if they changed
	if instance.primaryPool != nil {
		instance.primaryPool.ShutdownConnections()
	}

	pgCtlCmd := exec.Command(pgCtlName, options...) // #nosec
	err := execlog.RunStreaming(pgCtlCmd, pgCtlName)
	if err != nil {
		return fmt.Errorf("error requesting configuration reload: %w", err)
	}

	return nil
}

// Run this instance returning an OS process needed
// to control the instance execution
func (instance *Instance) Run() (*execlog.StreamingCmd, error) {
	process, err := instance.CheckForExistingPostmaster(GetPostgresExecutableName())
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
	if err := fileutils.EnsureDirectoryExists(socketDir); err != nil {
		return nil, fmt.Errorf("while creating socket directory: %w", err)
	}

	options := []string{
		"-D", instance.PgData,
	}

	// We need to make sure that the permissions are the right ones
	// in some systems they may be messed up even if we fix them before
	if err := fileutils.EnsurePgDataPerms(instance.PgData); err != nil {
		return nil, err
	}

	postgresCmd := exec.Command(GetPostgresExecutableName(), options...) // #nosec
	postgresCmd.Env = instance.buildPostgresEnv()
	compatibility.AddInstanceRunCommands(postgresCmd)

	streamingCmd, err := execlog.RunStreamingNoWait(postgresCmd, GetPostgresExecutableName())
	if err != nil {
		return nil, err
	}

	return streamingCmd, nil
}

// buildPostgresEnv builds the environment variables that should be used by PostgreSQL
// to run the main process, taking care of adding any library path that is needed for
// extensions.
func (instance *Instance) buildPostgresEnv() []string {
	env := instance.Env
	if env == nil {
		env = os.Environ()
	}
	envMap, _ := envmap.Parse(env)
	envMap["PG_OOM_ADJUST_FILE"] = "/proc/self/oom_score_adj"
	envMap["PG_OOM_ADJUST_VALUE"] = "0"

	// If there are no additional library paths, we use the environment variables
	// of the current process
	additionalLibraryPaths := collectLibraryPaths(instance.Cluster.Spec.PostgresConfiguration.Extensions)
	if len(additionalLibraryPaths) == 0 {
		return envMap.StringSlice()
	}

	// We add the additional library paths after the entries that are already
	// available.
	currentLibraryPath := envMap["LD_LIBRARY_PATH"]
	if currentLibraryPath != "" {
		currentLibraryPath += ":"
	}
	currentLibraryPath += strings.Join(additionalLibraryPaths, ":")
	envMap["LD_LIBRARY_PATH"] = currentLibraryPath

	return envMap.StringSlice()
}

// collectLibraryPaths returns a list of PATHS which should be added to LD_LIBRARY_PATH
// given an extension
func collectLibraryPaths(extensionList []apiv1.ExtensionConfiguration) []string {
	result := make([]string, 0, len(extensionList))

	for _, extension := range extensionList {
		for _, libraryPath := range extension.LdLibraryPath {
			result = append(
				result,
				filepath.Join(postgres.ExtensionsBaseDirectory, extension.Name, libraryPath),
			)
		}
	}

	return result
}

// WithActiveInstance execute the internal function while this
// PostgreSQL instance is running
func (instance *Instance) WithActiveInstance(inner func() error) error {
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
		if err := instance.Shutdown(ctx, defaultShutdownOptions); err != nil {
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

	parsedVersion, err := postgresutils.GetPgVersion(db)
	if err != nil {
		return semver.Version{}, err
	}
	instance.pgVersion = parsedVersion
	return *parsedVersion, nil
}

// ConnectionPool gets or initializes the connection pool for this instance
func (instance *Instance) ConnectionPool() pool.Pooler {
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

		instance.pool = pool.NewPostgresqlConnectionPool(dsn)
	}

	return instance.pool
}

// PrimaryConnectionPool gets or initializes the primary connection pool for this instance
func (instance *Instance) PrimaryConnectionPool() *pool.ConnectionPool {
	if instance.primaryPool == nil {
		instance.primaryPool = pool.NewPostgresqlConnectionPool(instance.GetPrimaryConnInfo())
	}

	return instance.primaryPool
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

// Demote demotes an existing PostgreSQL instance
func (instance *Instance) Demote(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	contextLogger.Info("Demoting instance", "pgpdata", instance.PgData)
	slotName := cluster.GetSlotNameFromInstanceName(instance.GetPodName())
	_, err := UpdateReplicaConfiguration(instance.PgData, instance.GetPrimaryConnInfo(), slotName)
	return err
}

// WaitForPrimaryAvailable waits until we can connect to the primary
func (instance *Instance) WaitForPrimaryAvailable(ctx context.Context) error {
	primaryConnInfo := instance.GetPrimaryConnInfo() + " connect_timeout=5"

	log.Info("Waiting for the new primary to be available",
		"primaryConnInfo", primaryConnInfo)

	db, err := sql.Open("pgx", primaryConnInfo)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	return waitForConnectionAvailable(ctx, db)
}

// WaitForSuperuserConnectionAvailable waits until we can connect to this
// instance using the superuser account
func (instance *Instance) WaitForSuperuserConnectionAvailable(ctx context.Context) error {
	db, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	return waitForConnectionAvailable(ctx, db)
}

// waitForConnectionAvailable waits until we can connect to the passed
// sql.DB connection
func waitForConnectionAvailable(ctx context.Context, db *sql.DB) error {
	contextLogger := log.FromContext(ctx)
	errorIsRetryable := func(err error) bool {
		if ctx.Err() != nil {
			return false
		}
		return err != nil
	}

	return retry.OnError(RetryUntilServerAvailable, errorIsRetryable, func() error {
		err := db.PingContext(ctx)
		if err != nil {
			contextLogger.Info("DB not available, will retry", "err", err)
		}
		return err
	})
}

// waitUntilConfigShaMatches waits until the configuration is correctly set
func (instance *Instance) waitUntilConfigShaMatches() error {
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
			return fmt.Errorf("configuration not yet matching: got %s, wanted %s", sha, instance.ConfigSha256)
		}
		return nil
	})
}

// WaitForConfigReload returns the postgresqlStatus and any error encountered
func (instance *Instance) WaitForConfigReload(ctx context.Context) (*postgres.PostgresqlStatus, error) {
	// This function could also be called while the server is being
	// started up, so we are not sure that the server is really active.
	// Let's wait for that.
	if instance.ConfigSha256 == "" {
		return nil, nil
	}

	err := instance.WaitForSuperuserConnectionAvailable(ctx)
	if err != nil {
		return nil, fmt.Errorf("while applying new configuration: %w", err)
	}

	err = instance.waitUntilConfigShaMatches()
	if err != nil {
		return nil, fmt.Errorf("while waiting for new configuration to be reloaded: %w", err)
	}

	status, err := instance.GetStatus()
	if err != nil {
		return nil, fmt.Errorf("while applying new configuration: %w", err)
	}

	if status.MightBeUnavailableMaskedError != "" {
		return nil, fmt.Errorf(
			"while applying new configuration encountered an error masked by mightBeUnavailable: %s",
			status.MightBeUnavailableMaskedError,
		)
	}

	return status, nil
}

// GetSynchronousReplicationMetadata reads the current PostgreSQL configuration
// and extracts the parameters that were used to compute the synchronous_standby_names
// GUC.
func (instance *Instance) GetSynchronousReplicationMetadata(
	ctx context.Context,
) (*postgres.SynchronousStandbyNamesConfig, error) {
	db, err := instance.GetSuperUserDB()
	if err != nil {
		return nil, err
	}

	var metadata string
	row := db.QueryRowContext(
		ctx, fmt.Sprintf("SHOW %s", postgres.CNPGSynchronousStandbyNamesMetadata))
	err = row.Scan(&metadata)
	if err != nil {
		return nil, err
	}

	if len(metadata) == 0 {
		return nil, nil
	}

	var result postgres.SynchronousStandbyNamesConfig
	if err := json.Unmarshal([]byte(metadata), &result); err != nil {
		return nil, fmt.Errorf("while decoding synchronous_standby_names metadata: %w", err)
	}

	return &result, nil
}

// waitForStreamingConnectionAvailable waits until we can connect to the passed
// sql.DB connection using streaming protocol
func waitForStreamingConnectionAvailable(ctx context.Context, db *sql.DB) error {
	contextLogger := log.FromContext(ctx)

	errorIsRetryable := func(err error) bool {
		return err != nil
	}

	return retry.OnError(RetryUntilServerAvailable, errorIsRetryable, func() error {
		result, err := db.Query("IDENTIFY_SYSTEM")
		if err != nil || result.Err() != nil {
			contextLogger.Info("DB not available, will retry", "err", err)
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
	pgControlBackupFilePath := pgControlFilePath + pgControlFileBackupExtension

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
	pgControlBackupFilePath := pgControlFilePath + pgControlFileBackupExtension

	err := os.Remove(pgControlBackupFilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// Rewind uses pg_rewind to align this data directory with the contents of the primary node.
// If postgres major version is >= 13, add "--restore-target-wal" option
func (instance *Instance) Rewind(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	// Signal the liveness probe that we are running pg_rewind before starting postgres
	instance.PgRewindIsRunning = true
	defer func() {
		instance.PgRewindIsRunning = false
	}()

	instance.LogPgControldata(ctx, "before pg_rewind")

	primaryConnInfo := instance.GetPrimaryConnInfo()
	options := make([]string, 0, 6)
	options = append(options,
		"-P",
		"--source-server", primaryConnInfo,
		"--target-pgdata", instance.PgData,
	)

	// make sure restore_command is set in override.conf
	if _, err := configurePostgresOverrideConfFile(instance.PgData, primaryConnInfo, ""); err != nil {
		return err
	}

	options = append(options, "--restore-target-wal")

	// Make sure PostgreSQL control file is not empty
	err := instance.managePgControlFileBackup()
	if err != nil {
		return err
	}

	contextLogger.Info("Starting up pg_rewind",
		"pgdata", instance.PgData,
		"options", options)

	pgRewindCmd := exec.Command(pgRewindName, options...) // #nosec
	pgRewindCmd.Env = instance.buildPostgresEnv()
	err = execlog.RunStreaming(pgRewindCmd, pgRewindName)
	if err != nil {
		contextLogger.Error(err, "Failed to execute pg_rewind", "options", options)
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
func PgIsReady() error {
	// We just use the environment variables we already have
	// to pass the connection parameters
	options := []string{
		"-U", "postgres",
		"-d", "postgres",
		"-q",
	}

	// Run `pg_isready` which returns 0 if everything is OK.
	// It returns 1 when PostgreSQL is not ready to accept
	// connections but, it is starting up (this is a valid
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

func (instance *Instance) buildPgControldataCommand() *exec.Cmd {
	pgControlDataCmd := exec.Command(pgControlDataName)
	pgControlDataCmd.Env = os.Environ()
	pgControlDataCmd.Env = append(pgControlDataCmd.Env, "PGDATA="+instance.PgData)

	return pgControlDataCmd
}

// LogPgControldata logs the content of PostgreSQL control data, for debugging and tracing
func (instance *Instance) LogPgControldata(ctx context.Context, reason string) {
	contextLogger := log.FromContext(ctx)

	contextLogger.Info("Extracting pg_controldata information", "reason", reason)

	pgControlDataCmd := instance.buildPgControldataCommand()
	err := execlog.RunBuffering(pgControlDataCmd, pgControlDataName)
	if err != nil {
		contextLogger.Error(err, "Error printing the control information of this PostgreSQL instance")
	}
}

// GetPgControldata returns the output of pg_controldata and any errors encountered
func (instance *Instance) GetPgControldata() (string, error) {
	pgControlDataCmd := instance.buildPgControldataCommand()
	out, err := pgControlDataCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("while executing pg_controldata: %w", err)
	}

	return string(out), nil
}

// GetInstanceCommandChan is the channel where the lifecycle manager will
// wait for the operations requested on the instance
func (instance *Instance) GetInstanceCommandChan() <-chan InstanceCommand {
	return instance.instanceCommandChan
}

// GetClusterName returns the name of the cluster where this instance belongs
func (instance *Instance) GetClusterName() string {
	return instance.clusterName
}

// GetPodName returns the name of the pod where this instance is running
func (instance *Instance) GetPodName() string {
	return instance.podName
}

// GetNamespaceName returns the name of the namespace where this instance is running
func (instance *Instance) GetNamespaceName() string {
	return instance.namespace
}

// GetArchitecture returns the runtime architecture
func (instance *Instance) GetArchitecture() string {
	return runtime.GOARCH
}

// RequestFastImmediateShutdown request the lifecycle manager to shut down
// PostgreSQL using the fast strategy and then the immediate strategy.
func (instance *Instance) RequestFastImmediateShutdown() {
	instance.instanceCommandChan <- shutDownFastImmediate
}

// RequestAndWaitRestartSmartFast requests the lifecycle manager to
// restart the postmaster, and wait for the postmaster to be restarted
func (instance *Instance) RequestAndWaitRestartSmartFast(ctx context.Context, timeout time.Duration) error {
	instance.SetMightBeUnavailable(true)
	defer instance.SetMightBeUnavailable(false)

	restartCtx, cancel := context.WithTimeoutCause(
		ctx,
		timeout,
		fmt.Errorf("timeout while restarting PostgreSQL"))
	defer cancel()

	now := time.Now()
	instance.requestRestartSmartFast()
	if err := instance.waitForInstanceRestarted(restartCtx, now); err != nil {
		return fmt.Errorf("while waiting for instance restart: %w", err)
	}

	return nil
}

// requestRestartSmartFast request the lifecycle manager to restart
// the postmaster
func (instance *Instance) requestRestartSmartFast() {
	instance.instanceCommandChan <- restartSmartFast
}

// RequestFencingOn request the lifecycle manager to shut down postgres and enable fencing
func (instance *Instance) RequestFencingOn() {
	instance.instanceCommandChan <- fenceOn
}

// RequestAndWaitFencingOff will request to remove the fencing
// and wait for the instance to be restarted
func (instance *Instance) RequestAndWaitFencingOff(ctx context.Context, timeout time.Duration) error {
	liftFencing := func() error {
		ctxWithTimeout, cancel := context.WithTimeoutCause(
			ctx,
			timeout,
			fmt.Errorf("timeout while resuming PostgreSQL from fencing"),
		)
		defer cancel()

		defer instance.SetMightBeUnavailable(false)
		now := time.Now()
		instance.requestFencingOff()
		err := instance.waitForInstanceRestarted(ctxWithTimeout, now)
		if err != nil {
			return fmt.Errorf("while waiting for instance restart: %w", err)
		}

		return nil
	}

	// let PostgreSQL start up
	if err := liftFencing(); err != nil {
		return err
	}

	// sleep enough for the pod to be ready again
	time.Sleep(2 * specs.ReadinessProbePeriod * time.Second)

	return nil
}

// requestFencingOff request the lifecycle manager to remove the fencing and restart postgres if needed
func (instance *Instance) requestFencingOff() {
	instance.instanceCommandChan <- fenceOff
}

// waitForInstanceRestarted waits until the instance reports being started
// after the given time
func (instance *Instance) waitForInstanceRestarted(ctx context.Context, after time.Time) error {
	retryUntilContextCancelled := func(_ error) bool {
		return ctx.Err() == nil
	}

	return retry.OnError(RetryUntilServerAvailable, retryUntilContextCancelled, func() error {
		db, err := instance.GetSuperUserDB()
		if err != nil {
			return err
		}
		var startTime time.Time
		row := db.QueryRowContext(ctx, "SELECT pg_catalog.pg_postmaster_start_time()")
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

// DropConnections drops all the connections of backend_type 'client backend'
func (instance *Instance) DropConnections() error {
	conn, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	if _, err := conn.Exec(
		`SELECT pg_catalog.pg_terminate_backend(pid)
			   FROM pg_catalog.pg_stat_activity
			   WHERE pid <> pg_backend_pid()
			     AND backend_type = 'client backend';`,
	); err != nil {
		return fmt.Errorf("while dropping connections: %w", err)
	}

	return nil
}

// GetPrimaryConnInfo returns the DSN to reach the primary
func (instance *Instance) GetPrimaryConnInfo() string {
	result := buildPrimaryConnInfo(instance.GetClusterName()+"-rw", instance.GetPodName()) + " dbname=postgres"

	standbyTCPUserTimeout := os.Getenv("CNPG_STANDBY_TCP_USER_TIMEOUT")
	if len(standbyTCPUserTimeout) == 0 {
		// Default to 5000ms (5 seconds) if not explicitly set
		standbyTCPUserTimeout = "5000"
	}

	result = fmt.Sprintf("%s tcp_user_timeout='%s'", result,
		strings.ReplaceAll(strings.ReplaceAll(standbyTCPUserTimeout, `\`, `\\`), `'`, `\'`))

	return result
}

// HandleInstanceCommandRequests execute a command requested by the reconciliation
// loop.
func (instance *Instance) HandleInstanceCommandRequests(
	ctx context.Context,
	req InstanceCommand,
) (restartNeeded bool, err error) {
	contextLogger := log.FromContext(ctx)

	if instance.IsFenced() {
		switch req {
		case fenceOff:
			contextLogger.Info("Fence lifting request received, will proceed with restarting the instance if needed")
			instance.SetFencing(false)
			return true, nil
		default:
			contextLogger.Warning("Received request while fencing, ignored", "req", req)
			return false, nil
		}
	}
	switch req {
	case fenceOn:
		contextLogger.Info("Fencing request received, will proceed shutting down the instance")
		instance.SetFencing(true)
		if err := instance.TryShuttingDownFastImmediate(ctx); err != nil {
			return false, fmt.Errorf("while shutting down the instance to fence it: %w", err)
		}
		return false, nil
	case restartSmartFast:
		return true, instance.TryShuttingDownSmartFast(ctx)
	case shutDownFastImmediate:
		if err := instance.TryShuttingDownFastImmediate(ctx); err != nil {
			contextLogger.Error(err, "error shutting down instance, proceeding")
		}
		return false, nil
	default:
		return false, fmt.Errorf("unrecognized request: %s", req)
	}
}

// tryCheckpointBeforeShutdown attempts to issue a checkpoint before shutdown.
// This is skipped if the instance is not a primary or if an immediate shutdown is requested.
// This reduces shutdown time and subsequent promotion time for replicas, especially for systems with high
// checkpoint_timeout.
// All outcomes (success, failure, or skipped) are logged appropriately.
func (instance *Instance) tryCheckpointBeforeShutdown(ctx context.Context, mode shutdownMode) {
	contextLogger := log.FromContext(ctx).WithName("checkpoint_before_shutdown")
	contextLogger.Info("Attempting checkpoint before shutdown")

	if mode == shutdownModeImmediate {
		contextLogger.Info("Skipping checkpoint: immediate shutdown requested")
		return
	}

	isPrimary, err := instance.IsPrimary()
	if err != nil {
		contextLogger.Error(err, "Failed to determine instance role, skipping checkpoint")
		return
	}

	if !isPrimary {
		contextLogger.Info("Skipping checkpoint: instance is not primary")
		return
	}

	db, err := instance.GetSuperUserDB()
	if err != nil {
		contextLogger.Error(err, "Failed to get superuser DB connection, skipping checkpoint")
		return
	}

	contextLogger.Info("Executing CHECKPOINT command before shutdown")
	if _, err = db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		contextLogger.Error(err, "Failed to execute CHECKPOINT command")
		return
	}

	contextLogger.Info("Checkpoint completed successfully")
}
