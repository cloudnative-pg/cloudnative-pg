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

package controller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"path"
	"path/filepath"
	"strconv"
	"time"

	postgresClient "github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgiclient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/controller"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/roles"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/reconciler"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	postgresManagement "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/metrics"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/metricserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres/replication"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/promotiontoken"
	externalcluster "github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/replicaclusterswitch"
	clusterstatus "github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system"
)

const (
	userSearchFunctionSchema = "public"
	userSearchFunctionName   = "user_search"
	userSearchFunction       = "SELECT usename, passwd FROM pg_catalog.pg_shadow WHERE usename=$1;"
)

// RetryUntilWalReceiverDown is the default retry configuration that is used
// to wait for the WAL receiver process to be down
var RetryUntilWalReceiverDown = wait.Backoff{
	Duration: 1 * time.Second,
	// Steps is declared as an "int", so we are capping
	// to int32 to support ARM-based 32 bit architectures
	Steps: math.MaxInt32,
}

// shouldRequeue specifies whether a new reconciliation loop should be triggered
type shoudRequeue bool

// Reconcile is the main reconciliation loop for the instance
// TODO this function needs to be refactor
//
//nolint:gocognit,gocyclo
func (r *InstanceReconciler) Reconcile(
	ctx context.Context,
	_ reconcile.Request,
) (reconcile.Result, error) {
	// set up a convenient contextLog object so we don't have to type request over and over again
	contextLogger := log.FromContext(ctx).
		WithValues(
			"instance", r.instance.GetPodName(),
			"cluster", r.instance.GetClusterName(),
			"namespace", r.instance.GetNamespaceName(),
		)

	// if the context has already been cancelled,
	// trying to reconcile would just lead to misleading errors being reported
	if err := ctx.Err(); err != nil {
		contextLogger.Warning("Context cancelled, will not reconcile", "err", err)
		return ctrl.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The cluster has been deleted.
			// We just need to wait for this instance manager to be terminated
			contextLogger.Debug("Could not find Cluster")
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, fmt.Errorf("could not fetch Cluster: %w", err)
	}

	contextLogger.Debug("Reconciling Cluster")

	pluginLoadingContext, cancelPluginLoading := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPluginLoading()

	pluginClient, err := cnpgiclient.WithPlugins(
		pluginLoadingContext,
		r.pluginRepository,
		cluster.GetInstanceEnabledPluginNames()...,
	)
	if err != nil {
		contextLogger.Error(err, "Error loading plugins, retrying")
		return ctrl.Result{}, err
	}
	defer func() {
		pluginClient.Close(ctx)
	}()

	ctx = cnpgiclient.SetPluginClientInContext(ctx, pluginClient)
	ctx = cluster.SetInContext(ctx)

	// Reconcile PostgreSQL instance parameters
	r.reconcileInstance(cluster)

	// Takes care of the `.check-empty-wal-archive` file
	if err := r.reconcileCheckWalArchiveFile(cluster); err != nil {
		return reconcile.Result{}, err
	}

	// Refresh the cache
	requeueOnMissingPermissions := r.updateCacheFromCluster(ctx, cluster)

	// Reconcile monitoring section
	r.reconcileMetrics(ctx, cluster)
	r.reconcileMonitoringQueries(ctx, cluster)

	// Verify that the promotion token is usable before changing the archive mode and triggering restarts
	if err := r.verifyPromotionToken(cluster); err != nil {
		var tokenError *promotiontoken.TokenVerificationError
		if errors.As(err, &tokenError) {
			if !tokenError.IsRetryable() {
				oldCluster := cluster.DeepCopy()
				contextLogger.Error(
					err,
					"Fatal error while verifying the promotion token",
					"tokenStatus", tokenError.Error(),
					"tokenContent", tokenError.TokenContent(),
				)

				cluster.Status.Phase = apiv1.PhaseUnrecoverable
				cluster.Status.PhaseReason = "Promotion token content is not correct for current instance"
				err := r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
				return reconcile.Result{}, err
			}
		}
	}

	// Reconcile secrets and cryptographic material
	// This doesn't need the PG connection, but it needs to reload it in case of changes
	reloadNeeded, err := r.certificateReconciler.RefreshSecrets(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("while refreshing secrets: %w", err)
	}

	reloadConfigNeeded, err := r.refreshConfigurationFiles(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}
	reloadNeeded = reloadNeeded || reloadConfigNeeded

	// here we execute initialization tasks that need to be executed only on the first reconciliation loop
	if !r.firstReconcileDone.Load() {
		if err = r.initialize(ctx, cluster); err != nil {
			return handleErrNextLoop(err)
		}
		r.firstReconcileDone.Store(true)
	}

	// Reconcile cluster role without DB
	reloadClusterRoleConfig, err := r.reconcileClusterRoleWithoutDB(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}
	reloadNeeded = reloadNeeded || reloadClusterRoleConfig

	r.systemInitialization.Broadcast()

	if result := r.reconcileFencing(ctx, cluster); result != nil {
		contextLogger.Info("Fencing status changed, will not proceed with the reconciliation loop")
		return *result, nil
	}

	if r.instance.IsFenced() || r.instance.MightBeUnavailable() {
		contextLogger.Info("Instance could be down, will not proceed with the reconciliation loop")
		return reconcile.Result{}, nil
	}

	if err := r.instance.IsReady(); err != nil {
		contextLogger.Info("Instance is still down, will retry in 1 second")
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	// Instance promotion will not automatically load the changed configuration files.
	// Therefore, it should not be counted as "a restart".
	if err := r.reconcilePrimary(ctx, cluster); err != nil {
		var tokenError *promotiontoken.TokenVerificationError
		if errors.As(err, &tokenError) {
			contextLogger.Warning(
				"Waiting for promotion token to be verified",
				"tokenStatus", tokenError.Error(),
				"tokenContent", tokenError.TokenContent(),
			)
			// We should be waiting for WAL recovery to reach the LSN in the token
			return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	restarted, err := r.reconcileOldPrimary(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	if r.IsDBUp(ctx) != nil {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	restartedInplace, err := r.restartPrimaryInplaceIfRequested(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}
	restarted = restarted || restartedInplace

	if reloadNeeded && !restarted {
		contextLogger.Info("reloading the instance")

		// IMPORTANT
		//
		// We are unsure of the state of the PostgreSQL configuration
		// meanwhile a new configuration is applied.
		//
		// For this reason, before applying a new configuration we
		// reset the FailoverQuorum object - de facto preventing any failover -
		// and we update it after.
		if err = r.resetFailoverQuorumObject(ctx, cluster); err != nil {
			return reconcile.Result{}, err
		}
		if err = r.instance.Reload(ctx); err != nil {
			return reconcile.Result{}, fmt.Errorf("while reloading the instance: %w", err)
		}
		if err = r.processConfigReloadAndManageRestart(ctx, cluster); err != nil {
			return reconcile.Result{}, fmt.Errorf("cannot apply new PostgreSQL configuration: %w", err)
		}
	}

	if err = r.updateFailoverQuorumObject(ctx, cluster); err != nil {
		return reconcile.Result{}, err
	}

	// IMPORTANT
	// From now on, the database can be assumed as running. Every operation
	// needing the database to be up should be put below this line.

	r.configureSlotReplicator(cluster)

	postgresDB, err := r.instance.ConnectionPool().Connection("postgres")
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("while getting the postgres connection: %w", err)
	}
	if result, err := reconciler.ReconcileReplicationSlots(
		ctx,
		r.instance.GetPodName(),
		postgresDB,
		cluster,
	); err != nil || !result.IsZero() {
		return result, err
	}

	if r.instance.GetPodName() == cluster.Status.CurrentPrimary {
		result, err := roles.Reconcile(ctx, r.instance, cluster, r.client)
		if err != nil || !result.IsZero() {
			return result, err
		}
	}

	if err = r.refreshCredentialsFromSecret(ctx, cluster); err != nil {
		return reconcile.Result{}, fmt.Errorf("while updating database owner password: %w", err)
	}

	if res, err := r.dropStaleReplicationConnections(ctx, cluster); err != nil || !res.IsZero() {
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("while dropping stale replica connections: %w", err)
		}
		return res, nil
	}

	if err := r.reconcileDatabases(ctx, cluster); err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot reconcile database configurations: %w", err)
	}

	if err := r.reconcilePgbouncerAuthUser(ctx, postgresDB, cluster); err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot reconcile pgbouncer integration: %w", err)
	}

	// Reconcile postgresql.auto.conf file permissions (< PG 17)
	// IMPORTANT: this needs a database connection to determine
	// the PostgreSQL major version
	r.reconcilePostgreSQLAutoConfFilePermissions(ctx, cluster)

	// EXTREMELY IMPORTANT
	//
	// The reconciliation loop may not have applied all the changes needed. In this case
	// we ensure another reconciliation loop will happen.
	// This is going to happen when:
	//
	// 1. We still don't have permissions to read the referenced secrets. This will
	//    happen when the secrets referenced in the Cluster change and the operator
	//    have not still reconciled the Role, or when the Role have not been applied
	//    by the API Server.
	//
	// 2. The current primary is reconciled before all the topology is extracted by the
	//    operator. Without another reconciliation loop we would have an incoherent
	//    state of electable synchronous_names inside the configuration.
	//    (this is only relevant if syncReplicaElectionConstraint is enabled)
	if requeueOnMissingPermissions || r.shouldRequeueForMissingTopology(ctx, cluster) {
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return reconcile.Result{}, nil
}

func (r *InstanceReconciler) configureSlotReplicator(cluster *apiv1.Cluster) {
	switch r.instance.GetPodName() {
	case cluster.Status.CurrentPrimary, cluster.Status.TargetPrimary:
		r.instance.ConfigureSlotReplicator(nil)
	default:
		r.instance.ConfigureSlotReplicator(cluster.Spec.ReplicationSlots)
	}
}

func (r *InstanceReconciler) restartPrimaryInplaceIfRequested(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (bool, error) {
	isPrimary := cluster.Status.CurrentPrimary == r.instance.GetPodName()
	restartRequested := isPrimary && cluster.Status.Phase == apiv1.PhaseInplacePrimaryRestart
	if restartRequested {
		if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
			return false, fmt.Errorf("cannot restart the primary in-place when a switchover is in progress")
		}

		if err := r.instance.RequestAndWaitRestartSmartFast(
			ctx,
			cluster.GetRestartTimeout(),
		); err != nil {
			return true, err
		}

		return true, clusterstatus.PatchWithOptimisticLock(
			ctx,
			r.client,
			cluster,
			clusterstatus.SetPhase(apiv1.PhaseHealthy, "Primary instance restarted in-place"),
			clusterstatus.SetClusterReadyCondition,
		)
	}
	return false, nil
}

func (r *InstanceReconciler) refreshConfigurationFiles(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (reloadNeeded bool, err error) {
	reloadNeeded, err = r.refreshPGHBA(ctx, cluster)
	if err != nil {
		return false, err
	}

	reloadIdent, err := r.instance.RefreshPGIdent(ctx, cluster.Spec.PostgresConfiguration.PgIdent)
	if err != nil {
		return false, err
	}
	reloadNeeded = reloadNeeded || reloadIdent

	reloadImages := r.requiresImagesRollout(ctx, cluster)
	reloadNeeded = reloadNeeded || reloadImages

	// Reconcile PostgreSQL configuration
	// This doesn't need the PG connection, but it needs to reload it in case of changes
	reloadConfig, err := r.instance.RefreshConfigurationFilesFromCluster(
		ctx,
		cluster,
		false,
		postgresClient.OperationType_TYPE_RECONCILE,
	)
	if err != nil {
		return false, err
	}
	reloadNeeded = reloadNeeded || reloadConfig

	reloadReplicaConfig, err := r.instance.RefreshReplicaConfiguration(ctx, cluster, r.client)
	if err != nil {
		return false, err
	}
	reloadNeeded = reloadNeeded || reloadReplicaConfig
	return reloadNeeded, nil
}

func (r *InstanceReconciler) requiresImagesRollout(ctx context.Context, cluster *apiv1.Cluster) bool {
	contextLogger := log.FromContext(ctx)

	latestImages := stringset.New()
	latestImages.Put(cluster.Spec.ImageName)
	for _, extension := range cluster.Spec.PostgresConfiguration.Extensions {
		latestImages.Put(extension.ImageVolumeSource.Reference)
	}

	if r.runningImages == nil {
		r.runningImages = latestImages
		contextLogger.Info("Detected running images", "runningImages", r.runningImages.ToSortedList())

		return false
	}

	contextLogger.Trace(
		"Calculated image requirements",
		"latestImages", latestImages.ToSortedList(),
		"runningImages", r.runningImages.ToSortedList())

	if latestImages.Eq(r.runningImages) {
		return false
	}

	contextLogger.Info(
		"Detected drift between the bootstrap images and the configuration. Skipping configuration reload",
		"runningImages", r.runningImages.ToSortedList(),
		"latestImages", latestImages.ToSortedList(),
	)

	return true
}

func (r *InstanceReconciler) reconcileFencing(ctx context.Context, cluster *apiv1.Cluster) *reconcile.Result {
	contextLogger := log.FromContext(ctx)

	fencingRequired := cluster.IsInstanceFenced(r.instance.GetPodName())
	isFenced := r.instance.IsFenced()
	switch {
	case !isFenced && fencingRequired:
		// fencing required and not enabled yet, request fencing and stop
		r.instance.RequestFencingOn()
		return &reconcile.Result{}
	case isFenced && !fencingRequired:
		// fencing enabled and not required anymore, request to disable fencing and continue
		timeout := time.Second * time.Duration(cluster.GetMaxStartDelay())
		err := r.instance.RequestAndWaitFencingOff(ctx, timeout)
		if err != nil {
			contextLogger.Error(err, "while waiting for the instance to be restarted after lifting the fence")
		}
		return &reconcile.Result{}
	}
	return nil
}

func handleErrNextLoop(err error) (reconcile.Result, error) {
	if errors.Is(err, controller.ErrNextLoop) {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}
	return reconcile.Result{}, err
}

// initialize will handle initialization tasks
func (r *InstanceReconciler) initialize(ctx context.Context, cluster *apiv1.Cluster) error {
	// we check there are no parameters that would prevent a follower to start
	if err := r.verifyParametersForFollower(ctx, cluster); err != nil {
		return err
	}

	if err := r.verifyPgDataCoherenceForPrimary(ctx, cluster); err != nil {
		return err
	}

	if err := system.SetCoredumpFilter(cluster.GetCoredumpFilter()); err != nil {
		return err
	}

	if err := r.ReconcileTablespaces(ctx, cluster); err != nil {
		return err
	}

	r.instance.SetFencing(cluster.IsInstanceFenced(r.instance.GetPodName()))

	return nil
}

// verifyParametersForFollower enforces that the follower's settings for the enforced
// parameters are higher than those of the primary.
// This could not be the case if the cluster spec value for one of those parameters
// is decreased shortly after having been increased. The follower would be restarting
// towards a high level, then write the lower value to the local config
func (r *InstanceReconciler) verifyParametersForFollower(
	ctx context.Context,
	cluster *apiv1.Cluster,
) error {
	contextLogger := log.FromContext(ctx)

	if isPrimary, _ := r.instance.IsPrimary(); isPrimary {
		return nil
	}

	// we use a file as a flag to ensure the pod has been restarted already. I.e. on
	// newly created pod we don't need to check the enforced parameters
	filename := path.Join(r.instance.PgData, fmt.Sprintf("%s-%s",
		constants.Startup, r.instance.GetPodName()))
	exists, err := fileutils.FileExists(filename)
	if err != nil {
		return err
	}
	// if the file did not exist, the pod was newly created and we can skip out
	if !exists {
		_, err := fileutils.WriteFileAtomic(filename, []byte(nil), 0o600)
		return err
	}
	contextLogger.Info("Found previous run flag", "filename", filename)
	controldataParams, err := postgresManagement.LoadEnforcedParametersFromPgControldata(r.instance.PgData)
	if err != nil {
		return err
	}
	clusterParams, err := postgresManagement.LoadEnforcedParametersFromCluster(cluster)
	if err != nil {
		return err
	}

	options := make(map[string]string)
	for key, enforcedparam := range controldataParams {
		clusterparam, found := clusterParams[key]
		if !found {
			continue
		}
		// if the values from `pg_controldata` are higher than the cluster spec,
		// they are the safer choice, so set them in config
		if enforcedparam > clusterparam {
			options[key] = strconv.Itoa(enforcedparam)
		}
	}
	if len(options) == 0 {
		return nil
	}
	contextLogger.Info("Updating some enforced parameters that would prevent the instance to start",
		"parameters", options, "clusterParams", clusterParams)
	// we write the safer enforced parameter values to pod config as safety
	// in the face of cluster specs going up and down from nervous users
	if _, err := configfile.UpdatePostgresConfigurationFile(
		path.Join(r.instance.PgData, constants.PostgresqlCustomConfigurationFile),
		options); err != nil {
		return err
	}
	return nil
}

// reconcileOldPrimary shuts down the instance in case it is an old primary
func (r *InstanceReconciler) reconcileOldPrimary(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (restarted bool, err error) {
	contextLogger := log.FromContext(ctx)

	if cluster.Status.TargetPrimary == r.instance.GetPodName() {
		return false, nil
	}

	isPrimary, err := r.instance.IsPrimary()
	if err != nil || !isPrimary {
		return false, err
	}

	contextLogger.Info("This is the former primary instance. Shutting it down to allow it to be demoted to a replica.")

	// Perform a fast shutdown on the instance and wait for the instance manager to stop.
	// The fast shutdown process will be preceded by a CHECKPOINT.
	// When the Pod restarts, it will be demoted to act as a replica of the new primary.
	r.Instance().RequestFastImmediateShutdown()

	// We wait for the lifecycle manager to have received the immediate shutdown request
	// and, having processed it, to request the termination of the instance manager.
	// When the termination has been requested, this context will be cancelled.
	<-ctx.Done()

	cluster.LogTimestampsWithMessage(ctx, "Old primary shutdown complete")

	return true, nil
}

// IsDBUp checks whether the superuserdb is reachable and returns an error if that's not the case
func (r *InstanceReconciler) IsDBUp(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)
	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		contextLogger.Warning(fmt.Sprintf("while getting a connection to the instance: %s", err))
		return err
	}

	if err := db.Ping(); err != nil {
		contextLogger.Info("DB not available, will retry", "err", err)
		return err
	}
	return nil
}

// reconcileDatabases reconciles all the existing databases
func (r *InstanceReconciler) reconcileDatabases(ctx context.Context, cluster *apiv1.Cluster) error {
	ok, err := r.instance.IsPrimary()
	if err != nil {
		return fmt.Errorf("unable to check if instance is primary: %w", err)
	}
	if !ok {
		return nil
	}
	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("getting the superuserdb: %w", err)
	}

	extensionStatusChanged := false
	for _, extension := range postgres.ManagedExtensions {
		extensionIsUsed := extension.IsUsed(cluster.Spec.PostgresConfiguration.Parameters)
		if lastStatus, ok := r.extensionStatus[extension.Name]; !ok || lastStatus != extensionIsUsed {
			extensionStatusChanged = true
			break
		}
	}

	databases, errors := r.getAllAccessibleDatabases(ctx, db)
	for _, databaseName := range databases {
		db, err := r.instance.ConnectionPool().Connection(databaseName)
		if err != nil {
			errors = append(errors,
				fmt.Errorf("could not connect to database %s: %w", databaseName, err))
			continue
		}
		if extensionStatusChanged {
			if err = r.reconcileExtensions(ctx, db, cluster.Spec.PostgresConfiguration.Parameters); err != nil {
				errors = append(errors,
					fmt.Errorf("could not reconcile extensions for database %s: %w", databaseName, err))
			}
		}
	}
	if errors != nil {
		return fmt.Errorf("got errors while reconciling databases: %v", errors)
	}

	for _, extension := range postgres.ManagedExtensions {
		extensionIsUsed := extension.IsUsed(cluster.Spec.PostgresConfiguration.Parameters)
		r.extensionStatus[extension.Name] = extensionIsUsed
	}

	return nil
}

// getAllAccessibleDatabases returns the list of all the accessible databases using the superuser
func (r *InstanceReconciler) getAllAccessibleDatabases(
	ctx context.Context,
	db *sql.DB,
) (databases []string, errors []error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{
		ReadOnly: true,
	})
	if err != nil {
		errors = append(errors, err)
		return nil, errors
	}

	defer func() {
		if err := tx.Commit(); err != nil {
			errors = append(errors, err)
		}
	}()

	databases, errors = postgresutils.GetAllAccessibleDatabases(tx, "datallowconn")
	return databases, errors
}

// ReconcileExtensions reconciles the expected extensions for this
// PostgreSQL instance
func (r *InstanceReconciler) reconcileExtensions(
	ctx context.Context, db *sql.DB, userSettings map[string]string,
) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		// This is a no-op when the transaction is committed
		_ = tx.Rollback()
	}()

	for _, extension := range postgres.ManagedExtensions {
		extensionIsUsed := extension.IsUsed(userSettings)

		row := tx.QueryRow("SELECT COUNT(*) > 0 FROM pg_catalog.pg_extension WHERE extname = $1", extension.Name)
		err = row.Err()
		if err != nil {
			break
		}

		var extensionIsInstalled bool
		if err = row.Scan(&extensionIsInstalled); err != nil {
			break
		}

		// We don't just use the "IF EXIST" to avoid stressing PostgreSQL with
		// a DDL when it is not really needed.

		if !extension.SkipCreateExtension && extensionIsUsed && !extensionIsInstalled {
			_, err = tx.Exec(fmt.Sprintf("CREATE EXTENSION %s", extension.Name))
		} else if !extensionIsUsed && extensionIsInstalled {
			_, err = tx.Exec(fmt.Sprintf("DROP EXTENSION %s", extension.Name))
		}
		if err != nil {
			break
		}
	}

	if err != nil {
		return err
	}

	return tx.Commit()
}

// ReconcileExtensions reconciles the expected extensions for this
// PostgreSQL instance
func (r *InstanceReconciler) reconcilePgbouncerAuthUser(
	ctx context.Context,
	db *sql.DB,
	cluster *apiv1.Cluster,
) error {
	// This need to be executed only against the primary node
	ok, err := r.instance.IsPrimary()
	if err != nil {
		return fmt.Errorf("unable to check if instance is primary: %w", err)
	}
	if !ok {
		return nil
	}

	// If there is no integrated pgbouncer, we directly skip the
	// integration.
	integrations := cluster.Status.PoolerIntegrations
	if integrations == nil || len(integrations.PgBouncerIntegration.Secrets) == 0 {
		return nil
	}

	// Otherwise, we need to ensure that both the role and the
	// auth_query function are present in the superuser database.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		// This is a no-op when the transaction is committed
		_ = tx.Rollback()
	}()

	var existsRole bool
	row := tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = '%s'",
		apiv1.PGBouncerPoolerUserName))
	err = row.Scan(&existsRole)
	if err != nil {
		return err
	}
	if !existsRole {
		_, err := tx.Exec(fmt.Sprintf("CREATE ROLE %s WITH LOGIN", apiv1.PGBouncerPoolerUserName))
		if err != nil {
			return err
		}

		_, err = tx.Exec(fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s",
			apiv1.PoolerAuthDBName, apiv1.PGBouncerPoolerUserName))
		if err != nil {
			return err
		}
	}

	var existsFunction bool
	row = tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) > 0 FROM pg_catalog.pg_proc WHERE proname='%s' and prosrc='%s'",
		userSearchFunctionName,
		userSearchFunction))
	err = row.Scan(&existsFunction)
	if err != nil {
		return err
	}
	if !existsFunction {
		_, err = tx.Exec(fmt.Sprintf("CREATE OR REPLACE FUNCTION %s.%s(uname TEXT) "+
			"RETURNS TABLE (usename name, passwd text) "+
			"as '%s' "+
			"LANGUAGE sql SECURITY DEFINER",
			userSearchFunctionSchema,
			userSearchFunctionName,
			userSearchFunction))
		if err != nil {
			return err
		}
		_, err = tx.Exec(fmt.Sprintf("REVOKE ALL ON FUNCTION %s.%s(text) FROM public;",
			userSearchFunctionSchema, userSearchFunctionName))
		if err != nil {
			return err
		}
		_, err = tx.Exec(fmt.Sprintf("GRANT EXECUTE ON FUNCTION %s.%s(text) TO %s",
			userSearchFunctionSchema,
			userSearchFunctionName,
			apiv1.PGBouncerPoolerUserName))
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// reconcileClusterRoleWithoutDB updates this instance's configuration files
// according to the role written in the cluster status
func (r *InstanceReconciler) reconcileClusterRoleWithoutDB(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (changed bool, err error) {
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return false, err
	}
	// Reconcile replica role
	if cluster.Status.TargetPrimary != r.instance.GetPodName() {
		if !isPrimary {
			// We need to ensure that this instance is replicating from the correct server
			return r.instance.RefreshReplicaConfiguration(ctx, cluster, r.client)
		}
		return false, nil
	}

	// Reconcile designated primary role
	if cluster.IsReplica() {
		return r.reconcileDesignatedPrimary(ctx, cluster)
	}
	// This is a primary server
	return false, nil
}

// reconcileMetrics updates the prometheus metrics that deal with instance
// manager or cluster data: manual_switchover_required, sync_replicas, replica_mode
func (r *InstanceReconciler) reconcileMetrics(
	ctx context.Context,
	cluster *apiv1.Cluster,
) {
	exporter := r.metricsServerExporter
	// We should never reset the SwitchoverRequired metrics as it needs the primary instance restarts,
	// however, if the cluster is healthy we make sure it is set to 0.
	if cluster.Status.CurrentPrimary == r.instance.GetPodName() {
		if cluster.Status.Phase == apiv1.PhaseWaitingForUser {
			exporter.Metrics.SwitchoverRequired.Set(1)
		} else {
			exporter.Metrics.SwitchoverRequired.Set(0)
		}
	}

	exporter.Metrics.SyncReplicas.WithLabelValues("min").Set(float64(cluster.Spec.MinSyncReplicas))
	exporter.Metrics.SyncReplicas.WithLabelValues("max").Set(float64(cluster.Spec.MaxSyncReplicas))

	syncReplicas := replication.GetExpectedSyncReplicasNumber(ctx, cluster)
	exporter.Metrics.SyncReplicas.WithLabelValues("expected").Set(float64(syncReplicas))

	if cluster.IsReplica() {
		exporter.Metrics.ReplicaCluster.Set(1)
	} else {
		exporter.Metrics.ReplicaCluster.Set(0)
	}
}

// reconcileMonitoringQueries applies the custom monitoring queries to the
// web server
func (r *InstanceReconciler) reconcileMonitoringQueries(
	ctx context.Context,
	cluster *apiv1.Cluster,
) {
	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("Reconciling custom monitoring queries")

	dbname := "postgres"
	if len(cluster.GetApplicationDatabaseName()) != 0 {
		dbname = cluster.GetApplicationDatabaseName()
	}

	queriesCollector := metrics.NewQueriesCollector("cnpg", r.instance, dbname)
	queriesCollector.InjectUserQueries(metricserver.DefaultQueries)

	if cluster.Spec.Monitoring == nil {
		r.metricsServerExporter.SetCustomQueries(queriesCollector)
		return
	}

	for _, reference := range cluster.Spec.Monitoring.CustomQueriesConfigMap {
		var configMap corev1.ConfigMap
		err := r.GetClient().Get(
			ctx,
			client.ObjectKey{Namespace: r.instance.GetNamespaceName(), Name: reference.Name},
			&configMap)
		if err != nil {
			contextLogger.Warning("Unable to get configMap containing custom monitoring queries",
				"reference", reference,
				"error", err.Error())
			continue
		}

		data, ok := configMap.Data[reference.Key]
		if !ok {
			contextLogger.Warning("Missing key in configMap",
				"reference", reference)
			continue
		}

		err = queriesCollector.ParseQueries([]byte(data))
		if err != nil {
			contextLogger.Warning("Error while parsing custom queries in ConfigMap",
				"reference", reference,
				"error", err.Error())
			continue
		}
	}

	for _, reference := range cluster.Spec.Monitoring.CustomQueriesSecret {
		var secret corev1.Secret
		err := r.GetClient().Get(ctx,
			client.ObjectKey{
				Namespace: r.instance.GetNamespaceName(),
				Name:      reference.Name,
			},
			&secret)
		if err != nil {
			contextLogger.Warning("Unable to get secret containing custom monitoring queries",
				"reference", reference,
				"error", err.Error())
			continue
		}

		data, ok := secret.Data[reference.Key]
		if !ok {
			contextLogger.Warning("Missing key in secret",
				"reference", reference)
			continue
		}

		err = queriesCollector.ParseQueries(data)
		if err != nil {
			contextLogger.Warning("Error while parsing custom queries in Secret",
				"reference", reference,
				"error", err.Error())
			continue
		}
	}

	r.metricsServerExporter.SetCustomQueries(queriesCollector)
}

// reconcileInstance sets PostgreSQL instance parameters to current values
func (r *InstanceReconciler) reconcileInstance(cluster *apiv1.Cluster) {
	detectRequiresDesignatedPrimaryTransition := func() bool {
		if !cluster.IsReplica() {
			return false
		}

		if !externalcluster.IsDesignatedPrimaryTransitionRequested(cluster) {
			return false
		}

		if !r.instance.IsFenced() && !r.instance.MightBeUnavailable() {
			return false
		}

		// Check if this pod was the primary before the transition started.
		// We use CurrentPrimary instead of IsPrimary() because IsPrimary()
		// checks for the absence of standby.signal, which gets created during
		// the transition by RefreshReplicaConfiguration(). Using CurrentPrimary
		// keeps the sentinel true throughout the transition, allowing retries
		// if the status update fails due to optimistic locking conflicts.
		return cluster.Status.CurrentPrimary == r.instance.GetPodName()
	}

	r.instance.PgCtlTimeoutForPromotion = cluster.GetPgCtlTimeoutForPromotion()
	r.instance.MaxSwitchoverDelay = cluster.GetMaxSwitchoverDelay()
	r.instance.MaxStopDelay = cluster.GetMaxStopDelay()
	r.instance.SmartStopDelay = cluster.GetSmartShutdownTimeout()
	r.instance.RequiresDesignatedPrimaryTransition = detectRequiresDesignatedPrimaryTransition()
	r.instance.Cluster = cluster
}

// PostgreSQLAutoConfWritable reconciles the permissions bit of `postgresql.auto.conf`
// given the relative setting in `.spec.postgresql.enableAlterSystem`
func (r *InstanceReconciler) reconcilePostgreSQLAutoConfFilePermissions(ctx context.Context, cluster *apiv1.Cluster) {
	contextLogger := log.FromContext(ctx)
	version, err := r.instance.GetPgVersion()
	if err != nil {
		contextLogger.Error(err, "while getting Postgres version")
		return
	}

	if version.Major >= 17 {
		// PostgreSQL 17 and newer versions allow preventing ALTER SYSTEM
		// usages using a GUC. We don't need to do anything on the file
		// system side.
		return
	}

	autoConfWriteable := cluster.Spec.PostgresConfiguration.EnableAlterSystem
	if err = r.instance.SetPostgreSQLAutoConfWritable(autoConfWriteable); err != nil {
		contextLogger.Error(err, "Error while changing mode of the postgresql.auto.conf file, skipped")
	}
}

// reconcileCheckWalArchiveFile takes care of the `.check-empty-wal-archive`
// file inside the PGDATA.
// If `.check-empty-wal-archive` is present, the WAL archiver verifies
// that the backup object store is empty.
// The file is created immediately after initdb and removed after the
// first WAL is archived
func (r *InstanceReconciler) reconcileCheckWalArchiveFile(cluster *apiv1.Cluster) error {
	filePath := filepath.Join(r.instance.PgData, constants.CheckEmptyWalArchiveFile)
	for _, condition := range cluster.Status.Conditions {
		// If our current condition is archiving we can delete the file
		if condition.Type == string(apiv1.ConditionContinuousArchiving) && condition.Status == metav1.ConditionTrue {
			return fileutils.RemoveFile(filePath)
		}
	}

	return nil
}

// processConfigReloadAndManageRestart waits for the db to be up and
// the new configuration to be reloaded
func (r *InstanceReconciler) processConfigReloadAndManageRestart(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	status, err := r.instance.WaitForConfigReload(ctx)
	if err != nil {
		return err
	}

	if status == nil || !status.PendingRestart {
		// Everything fine
		return nil
	}

	// if there is a pending restart, the instance is a primary and
	// the restart is due to a decrease of sensible parameters,
	// we will need to restart the primary instance in place
	phase := apiv1.PhaseApplyingConfiguration
	phaseReason := "PostgreSQL configuration changed"
	if status.IsPrimary && status.PendingRestartForDecrease {
		if cluster.GetPrimaryUpdateStrategy() == apiv1.PrimaryUpdateStrategyUnsupervised {
			return r.triggerRestartForDecrease(ctx, cluster)
		}
		reason := "decrease of hot standby sensitive parameters"
		contextLogger.Info("Waiting for the user to request a restart of the primary instance or a switchover "+
			"to complete the rolling update",
			"cluster", cluster.Name, "primaryPod", status.Pod.Name, "reason", reason)
		phase = apiv1.PhaseWaitingForUser
		phaseReason = "User must issue a supervised switchover"
	}
	if phase == apiv1.PhaseApplyingConfiguration &&
		(cluster.Status.Phase == apiv1.PhaseApplyingConfiguration ||
			(status.IsPrimary && cluster.Spec.Instances > 1)) {
		// I'm not the first instance spotting the configuration
		// change, everything is fine and there is no need to signal
		// the operator again.
		// We also don't want to trigger the reconciliation loop from the primary
		// for parameters which are not to be considered sensible for hot standbys,
		// so, we will wait for replicas to trigger it first.
		return nil
	}

	return clusterstatus.PatchWithOptimisticLock(
		ctx,
		r.client,
		cluster,
		clusterstatus.SetPhase(phase, phaseReason),
		clusterstatus.SetClusterReadyCondition,
	)
}

// triggerRestartForDecrease triggers an in-place restart and then asks
// the operator to continue with the reconciliation. This is needed to
// apply a change in replica-sensitive parameters that need to be done
// on the primary node and, after that, to the replicas
func (r *InstanceReconciler) triggerRestartForDecrease(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	contextLogger.Info("Restarting primary in-place due to hot standby sensible parameters decrease")
	if err := r.Instance().RequestAndWaitRestartSmartFast(ctx, cluster.GetRestartTimeout()); err != nil {
		return err
	}

	phase := apiv1.PhaseApplyingConfiguration
	phaseReason := "Decrease of hot standby sensitive parameters"

	return clusterstatus.PatchWithOptimisticLock(
		ctx,
		r.client,
		cluster,
		clusterstatus.SetPhase(phase, phaseReason),
		clusterstatus.SetClusterReadyCondition,
	)
}

// Reconciler primary logic. DB needed.
func (r *InstanceReconciler) reconcilePrimary(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	if cluster.Status.TargetPrimary != r.instance.GetPodName() || cluster.IsReplica() {
		return nil
	}

	oldCluster := cluster.DeepCopy()
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}

	// If I'm not the primary, let's promote myself
	if !isPrimary {
		// Verify that the promotion token is met before promoting
		if err := r.verifyPromotionToken(cluster); err != nil {
			// Report that a promotion is still ongoing on the cluster
			cluster.Status.Phase = apiv1.PhaseReplicaClusterPromotion
			if err := r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster)); err != nil {
				return err
			}
			return err
		}

		cluster.LogTimestampsWithMessage(ctx, "Setting myself as primary")
		if err := r.handlePromotion(ctx, cluster); err != nil {
			return err
		}
	}

	// if the currentPrimary doesn't match the PodName we set the correct value.
	if cluster.Status.CurrentPrimary != r.instance.GetPodName() {
		cluster.Status.CurrentPrimary = r.instance.GetPodName()
		cluster.Status.CurrentPrimaryTimestamp = pgTime.GetCurrentTimestamp()

		if err := r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster)); err != nil {
			return err
		}

		if err := r.instance.DropConnections(); err != nil {
			return err
		}
		cluster.LogTimestampsWithMessage(ctx, "Finished setting myself as primary")
	}

	if cluster.Spec.ReplicaCluster != nil &&
		cluster.Spec.ReplicaCluster.PromotionToken != cluster.Status.LastPromotionToken {
		cluster.Status.LastPromotionToken = cluster.Spec.ReplicaCluster.PromotionToken
		if err := r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster)); err != nil {
			return err
		}

		contextLogger.Info("Updated last promotion token", "lastPromotionToken",
			cluster.Spec.ReplicaCluster.PromotionToken)
	}

	// If it is already the current primary, everything is ok
	return nil
}

func (r *InstanceReconciler) handlePromotion(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("I'm the target primary, wait for the wal_receiver to be terminated")
	if r.instance.GetPodName() != cluster.Status.CurrentPrimary {
		// if the cluster is not replicating it means it's doing a failover and
		// we have to wait for wal receivers to be down
		err := r.waitForWalReceiverDown(ctx)
		if err != nil {
			return err
		}
	}

	contextLogger.Info("I'm the target primary, applying WALs and promoting my instance")
	// I must promote my instance here
	err := r.instance.PromoteAndWait(ctx)
	if err != nil {
		return fmt.Errorf("error promoting instance: %w", err)
	}
	return nil
}

// Reconciler designated primary logic for replica clusters
func (r *InstanceReconciler) reconcileDesignatedPrimary(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (changed bool, err error) {
	// If I'm already the current designated primary everything is ok.
	if cluster.Status.CurrentPrimary == r.instance.GetPodName() &&
		!r.instance.RequiresDesignatedPrimaryTransition {
		return false, nil
	}

	// We need to ensure that this instance is replicating from the correct server
	changed, err = r.instance.RefreshReplicaConfiguration(ctx, cluster, r.client)
	if err != nil {
		return changed, err
	}

	// I'm the primary, need to inform the operator
	log.FromContext(ctx).Info("Setting myself as the current designated primary")

	cluster.Status.CurrentPrimary = r.instance.GetPodName()
	cluster.Status.CurrentPrimaryTimestamp = pgTime.GetCurrentTimestamp()
	if r.instance.RequiresDesignatedPrimaryTransition {
		externalcluster.SetDesignatedPrimaryTransitionCompleted(cluster)
	}

	if err := r.client.Status().Update(ctx, cluster); err != nil {
		return changed, err
	}

	return changed, nil
}

// waitForWalReceiverDown wait until the wal receiver is down, and it's used
// to grab all the WAL files from a replica
func (r *InstanceReconciler) waitForWalReceiverDown(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	// This is not really exponential backoff as RetryUntilWalReceiverDown
	// doesn't contain any increment
	return wait.ExponentialBackoff(RetryUntilWalReceiverDown, func() (done bool, err error) {
		status, err := r.instance.IsWALReceiverActive()
		if err != nil {
			return true, err
		}

		if !status {
			return true, nil
		}

		contextLogger.Info("WAL receiver is still active, waiting")
		return false, nil
	})
}

// refreshCredentialsFromSecret updates the PostgreSQL users credentials
// in the primary pod with the content from the secrets
func (r *InstanceReconciler) refreshCredentialsFromSecret(
	ctx context.Context,
	cluster *apiv1.Cluster,
) error {
	// We only update the password in the primary pod
	primary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}
	if !primary {
		return nil
	}

	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	if cluster.GetEnableSuperuserAccess() {
		err = r.reconcileUser(ctx, "postgres", cluster.GetSuperuserSecretName(), db)
		if err != nil {
			return err
		}
	} else {
		err = postgresutils.DisableSuperuserPassword(db)
		if err != nil {
			return err
		}
	}

	if cluster.ShouldCreateApplicationDatabase() {
		err = r.reconcileUser(ctx, cluster.GetApplicationDatabaseOwner(), cluster.GetApplicationSecretName(), db)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *InstanceReconciler) reconcileUser(ctx context.Context, username string, secretName string, db *sql.DB) error {
	var secret corev1.Secret
	err := r.GetClient().Get(
		ctx,
		client.ObjectKey{Namespace: r.instance.GetNamespaceName(), Name: secretName},
		&secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if r.secretVersions[secret.Name] == secret.ResourceVersion {
		// Everything fine, we already applied this secret
		return nil
	}

	usernameFromSecret, password, err := utils.GetUserPasswordFromSecret(&secret)
	if err != nil {
		return err
	}

	if username != usernameFromSecret {
		return fmt.Errorf("wrong username '%v' in secret, expected '%v'", usernameFromSecret, username)
	}

	err = postgresutils.SetUserPassword(username, password, db)
	if err != nil {
		return err
	}

	r.secretVersions[secret.Name] = secret.ResourceVersion

	return nil
}

func (r *InstanceReconciler) refreshPGHBA(ctx context.Context, cluster *apiv1.Cluster) (
	postgresHBAChanged bool,
	err error,
) {
	var ldapBindPassword string
	if ldapSecretName := cluster.GetLDAPSecretName(); ldapSecretName != "" {
		ldapBindPasswordSecret := corev1.Secret{}
		err := r.GetClient().Get(ctx,
			types.NamespacedName{
				Name:      ldapSecretName,
				Namespace: r.instance.GetNamespaceName(),
			}, &ldapBindPasswordSecret)
		if err != nil {
			return false, err
		}
		secretKey := cluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth.BindPassword.Key
		ldapBindPasswordByte, ok := ldapBindPasswordSecret.Data[secretKey]
		if !ok {
			return false, fmt.Errorf("missing key inside bind+search secret: %s", secretKey)
		}
		ldapBindPassword = string(ldapBindPasswordByte)
	}
	// Generate pg_hba.conf file
	return r.instance.RefreshPGHBA(ctx, cluster, ldapBindPassword)
}

func (r *InstanceReconciler) shouldRequeueForMissingTopology(
	ctx context.Context,
	cluster *apiv1.Cluster,
) shoudRequeue {
	contextLogger := log.FromContext(ctx)

	syncReplicaConstraint := cluster.Spec.PostgresConfiguration.SyncReplicaElectionConstraint
	if !syncReplicaConstraint.Enabled {
		return false
	}
	if primary, _ := r.instance.IsPrimary(); !primary {
		return false
	}

	topologyStatus := cluster.Status.Topology
	if !topologyStatus.SuccessfullyExtracted || len(topologyStatus.Instances) != cluster.Spec.Instances {
		contextLogger.Info("missing topology information while syncReplicaElectionConstraint are enabled, " +
			"will requeue to calculate correctly the synchronous names")
		return true
	}

	return false
}

// dropStaleReplicationConnections is responsible for terminating all existing
// replication connections following a role change in a replica cluster.
//
// For context, demoting a PostgreSQL instance involves shutting it down,
// adjusting the necessary signal files and configuration, and then
// restarting it, which inherently disconnects all existing connections.
//
// In a replica cluster, demotion is unnecessary since it comprises replicas
// only. In this scenario, only the primary_conninfo parameter needs to be
// modified, which doesn't require a shutdown.
// However, this also implies that replicas receiving data from the old
// primary won't have their connections terminated.
//
// Consequently, high-availability replicas connected to the previous primary
// will remain connected, necessitating manual intervention to terminate
// those connections and re-establish them with the new endpoint.
//
// The dropStaleReplicationConnections function addresses this requirement.
func (r *InstanceReconciler) dropStaleReplicationConnections(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	if !cluster.IsReplica() {
		return ctrl.Result{}, nil
	}

	if cluster.Status.CurrentPrimary == r.instance.GetPodName() {
		return ctrl.Result{}, nil
	}

	conn, err := r.instance.GetSuperUserDB()
	if err != nil {
		return ctrl.Result{}, err
	}

	result, err := conn.ExecContext(
		ctx,
		`SELECT pg_catalog.pg_terminate_backend(pid)
		FROM pg_catalog.pg_stat_replication
		WHERE application_name LIKE $1`,
		fmt.Sprintf("%v-%%", cluster.Name),
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("while executing pg_terminate_backend: %w", err)
	}

	terminatedConnections, err := result.RowsAffected()
	if err != nil {
		return ctrl.Result{}, err
	}

	if terminatedConnections > 0 {
		// given that we have executed a pg_terminate_backend, we request a new reconciliation loop to ensure that
		// everything is in order and no leftovers that needs to be dropped are present.
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}
