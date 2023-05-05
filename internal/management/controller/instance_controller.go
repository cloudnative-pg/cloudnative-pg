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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/controllers"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/roles"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/reconciler"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/archiver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	postgresManagement "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/metrics"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/metricserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	pkgUtils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	userSearchFunctionName = "user_search"
	userSearchFunction     = "SELECT usename, passwd FROM pg_shadow WHERE usename=$1;"
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
	contextLogger, ctx := log.SetupLogger(ctx)

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

	// Print the Cluster
	contextLogger.Debug("Reconciling Cluster", "cluster", cluster)

	// Reconcile PostgreSQL instance parameters
	r.reconcileInstance(cluster)

	// Takes care of the `.check-empty-wal-archive` file inside the PGDATA
	// which, if present, before running the WAL archiver verifies that
	// the backup object store is empty. This file is created immediately
	// after initdb and removed after the first WAL is archived.
	if err := r.reconcileCheckWalArchiveFile(cluster); err != nil {
		return reconcile.Result{}, err
	}

	// Refresh the cache
	requeue := r.updateCacheFromCluster(ctx, cluster)

	// Reconcile monitoring section
	r.reconcileMetrics(cluster)
	r.reconcileMonitoringQueries(ctx, cluster)

	// Reconcile secrets and cryptographic material
	// This doesn't need the PG connection, but it needs to reload it in case of changes
	reloadNeeded := r.RefreshSecrets(ctx, cluster)

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

	if result := r.reconcileFencing(cluster); result != nil {
		contextLogger.Info("Fencing status changed, will not proceed with the reconciliation loop")
		return *result, nil
	}

	if r.instance.IsFenced() || r.instance.MightBeUnavailable() {
		contextLogger.Info("Instance could be down, will not proceed with the reconciliation loop")
		return reconcile.Result{}, nil
	}

	if r.instance.IsServerHealthy() != nil {
		contextLogger.Info("Instance is still down, will retry in 1 second")
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	r.configureSlotReplicator(cluster)

	if result, err := reconciler.ReconcileReplicationSlots(
		ctx,
		r.instance.PodName,
		infrastructure.NewPostgresManager(r.instance.ConnectionPool()),
		cluster,
	); err != nil || !result.IsZero() {
		return result, err
	}

	if r.instance.PodName == cluster.Status.CurrentPrimary {
		result, err := roles.Reconcile(ctx, r.instance, cluster, r.client)
		if err != nil || !result.IsZero() {
			return result, err
		}
	}

	restarted, err := r.reconcilePrimary(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	restartedFromOldPrimary, err := r.reconcileOldPrimary(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	restarted = restarted || restartedFromOldPrimary

	if r.IsDBUp(ctx) != nil {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	restartedInplace, err := r.restartPrimaryInplaceIfRequested(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}
	restarted = restarted || restartedInplace

	// from now on the database can be assumed as running

	if reloadNeeded && !restarted {
		contextLogger.Info("reloading the instance")
		if err = r.instance.Reload(); err != nil {
			return reconcile.Result{}, fmt.Errorf("while reloading the instance: %w", err)
		}
		if err = r.waitForConfigurationReload(ctx, cluster); err != nil {
			return reconcile.Result{}, fmt.Errorf("cannot apply new PostgreSQL configuration: %w", err)
		}
	}

	if err = r.refreshCredentialsFromSecret(ctx, cluster); err != nil {
		return reconcile.Result{}, fmt.Errorf("while updating database owner password: %w", err)
	}

	if err := r.reconcileDatabases(ctx, cluster); err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot reconcile database configurations: %w", err)
	}

	// Extremely important.
	// It could happen that current primary is reconciled before all the topology is extracted by the operator.
	// We should detect that and schedule the instance manager for another run otherwise we will end up having
	// an incoherent state of electable synchronous_names inside the configuration.
	// This is only relevant if syncReplicaElectionConstraint is enabled
	if !requeue {
		requeue = r.shouldRequeueForMissingTopology(cluster)
	}

	if requeue {
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return reconcile.Result{}, nil
}

func (r *InstanceReconciler) configureSlotReplicator(cluster *apiv1.Cluster) {
	switch r.instance.PodName {
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
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return false, err
	}
	if isPrimary && cluster.Status.Phase == apiv1.PhaseInplacePrimaryRestart {
		if err := r.instance.RequestAndWaitRestartSmartFast(); err != nil {
			return true, err
		}
		oldCluster := cluster.DeepCopy()
		cluster.Status.Phase = apiv1.PhaseHealthy
		cluster.Status.PhaseReason = "Primary instance restarted in-place"
		return true, r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
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

	// Reconcile PostgreSQL configuration
	// This doesn't need the PG connection, but it needs to reload it in case of changes
	reloadConfig, err := r.instance.RefreshConfigurationFilesFromCluster(cluster, false)
	if err != nil {
		return false, err
	}
	reloadNeeded = reloadNeeded || reloadConfig

	reloadReplicaConfig, err := r.refreshReplicaConfiguration(ctx, cluster)
	if err != nil {
		return false, err
	}
	reloadNeeded = reloadNeeded || reloadReplicaConfig
	return reloadNeeded, nil
}

func (r *InstanceReconciler) reconcileFencing(cluster *apiv1.Cluster) *reconcile.Result {
	fencingRequired := cluster.IsInstanceFenced(r.instance.PodName)
	isFenced := r.instance.IsFenced()
	switch {
	case !isFenced && fencingRequired:
		// fencing required and not enabled yet, request fencing and stop
		r.instance.RequestFencingOn()
		return &reconcile.Result{}
	case isFenced && !fencingRequired:
		// fencing enabled and not required anymore, request to disable fencing and continue
		err := r.instance.RequestAndWaitFencingOff()
		if err != nil {
			log.Error(err, "while waiting for the instance to be restarted after lifting the fence")
		}
		return &reconcile.Result{}
	}
	return nil
}

func handleErrNextLoop(err error) (reconcile.Result, error) {
	if errors.Is(err, controllers.ErrNextLoop) {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}
	return reconcile.Result{}, err
}

// initialize will handle initialization tasks
func (r *InstanceReconciler) initialize(ctx context.Context, cluster *apiv1.Cluster) error {
	// we check there are no parameters that would prevent a follower to start
	if err := r.verifyParametersForFollower(cluster); err != nil {
		return err
	}

	if err := r.verifyPgDataCoherenceForPrimary(ctx, cluster); err != nil {
		return err
	}

	r.instance.SetFencing(cluster.IsInstanceFenced(r.instance.PodName))

	return nil
}

// verifyParametersForFollower enforces that the follower's settings for the enforced
// parameters are higher than those of the primary.
// This could not be the case if the cluster spec value for one of those parameters
// is decreased shortly after having been increased. The follower would be restarting
// towards a high level, then write the lower value to the local config
func (r *InstanceReconciler) verifyParametersForFollower(cluster *apiv1.Cluster) error {
	if isPrimary, _ := r.instance.IsPrimary(); isPrimary {
		return nil
	}

	// we use a file as a flag to ensure the pod has been restarted already. I.e. on
	// newly created pod we don't need to check the enforced parameters
	filename := path.Join(r.instance.PgData, fmt.Sprintf("%s-%s", constants.Startup, r.instance.PodName))
	exists, err := fileutils.FileExists(filename)
	if err != nil {
		return err
	}
	// if the file did not exist, the pod was newly created and we can skip out
	if !exists {
		_, err := fileutils.WriteFileAtomic(filename, []byte(nil), 0o600)
		return err
	}
	log.Info("Found previous run flag", "filename", filename)
	enforcedParams, err := postgresManagement.GetEnforcedParametersThroughPgControldata(r.instance.PgData)
	if err != nil {
		return err
	}

	clusterParams := cluster.Spec.PostgresConfiguration.Parameters
	options := make(map[string]string)
	for key, enforcedparam := range enforcedParams {
		clusterparam, found := clusterParams[key]
		if !found {
			continue
		}
		enforcedparamInt, err := strconv.Atoi(enforcedparam)
		if err != nil {
			return err
		}
		clusterparamInt, err := strconv.Atoi(clusterparam)
		if err != nil {
			return err
		}
		// if the values from `pg_controldata` are higher than the cluster spec,
		// they are the safer choice, so set them in config
		if enforcedparamInt > clusterparamInt {
			options[key] = enforcedparam
		}
	}
	if len(options) == 0 {
		return nil
	}
	log.Info("Updating some enforced parameters that would prevent the instance to start",
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

	if cluster.Status.TargetPrimary == r.instance.PodName {
		return false, nil
	}

	isPrimary, err := r.instance.IsPrimary()
	if err != nil || !isPrimary {
		return false, err
	}

	contextLogger.Info("This is an old primary node. Requesting a checkpoint before demotion")

	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		contextLogger.Error(err, "Cannot connect to primary server")
	} else {
		_, err = db.Exec("CHECKPOINT")
		if err != nil {
			contextLogger.Error(err, "Error while requesting a checkpoint")
		}
	}

	contextLogger.Info("This is an old primary node. Shutting it down to get it demoted to a replica")

	// Here we need to invoke a fast shutdown on the instance, and wait the instance
	// manager to be stopped.
	// When the Pod will restart, we will demote as a replica of the new primary
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
		if err = r.reconcilePoolers(ctx, db, databaseName, cluster.Status.PoolerIntegrations); err != nil {
			errors = append(errors,
				fmt.Errorf("could not reconcile extensions for database %s: %w", databaseName, err))
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

	_, err = tx.Exec("SET LOCAL synchronous_commit TO local")
	if err != nil {
		return err
	}

	for _, extension := range postgres.ManagedExtensions {
		extensionIsUsed := extension.IsUsed(userSettings)

		row := tx.QueryRow("SELECT COUNT(*) > 0 FROM pg_extension WHERE extname = $1", extension.Name)
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
func (r *InstanceReconciler) reconcilePoolers(
	ctx context.Context, db *sql.DB, dbName string, integrations *apiv1.PoolerIntegrations,
) (err error) {
	if integrations == nil || len(integrations.PgBouncerIntegration.Secrets) == 0 {
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		// This is a no-op when the transaction is committed
		_ = tx.Rollback()
	}()

	_, err = tx.Exec("SET LOCAL synchronous_commit TO local")
	if err != nil {
		return err
	}

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
		_, err = tx.Exec(fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", dbName, apiv1.PGBouncerPoolerUserName))
		if err != nil {
			return err
		}
	}

	var existsFunction bool
	row = tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) > 0 FROM pg_proc WHERE proname='%s' and prosrc='%s'",
		userSearchFunctionName,
		userSearchFunction))
	err = row.Scan(&existsFunction)
	if err != nil {
		return err
	}
	if !existsFunction {
		_, err = tx.Exec(fmt.Sprintf("CREATE OR REPLACE FUNCTION %s(uname TEXT) "+
			"RETURNS TABLE (usename name, passwd text) "+
			"as '%s' "+
			"LANGUAGE sql SECURITY DEFINER",
			userSearchFunctionName,
			userSearchFunction))
		if err != nil {
			return err
		}
		_, err = tx.Exec(fmt.Sprintf("REVOKE ALL ON FUNCTION %s(text) FROM public;", userSearchFunctionName))
		if err != nil {
			return err
		}
		_, err = tx.Exec(fmt.Sprintf("GRANT EXECUTE ON FUNCTION %s(text) TO %s",
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
	if cluster.Status.TargetPrimary != r.instance.PodName {
		if !isPrimary {
			// We need to ensure that this instance is replicating from the correct server
			return r.refreshReplicaConfiguration(ctx, cluster)
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

// reconcileMetrics updates any required metrics
func (r *InstanceReconciler) reconcileMetrics(
	cluster *apiv1.Cluster,
) {
	exporter := r.metricsServerExporter
	// We should never reset the SwitchoverRequired metrics as it needs the primary instance restarts,
	// however, if the cluster is healthy we make sure it is set to 0.
	if cluster.Status.CurrentPrimary == r.instance.PodName {
		if cluster.Status.Phase == apiv1.PhaseWaitingForUser {
			exporter.Metrics.SwitchoverRequired.Set(1)
		} else {
			exporter.Metrics.SwitchoverRequired.Set(0)
		}
	}

	exporter.Metrics.SyncReplicas.WithLabelValues("min").Set(float64(cluster.Spec.MinSyncReplicas))
	exporter.Metrics.SyncReplicas.WithLabelValues("max").Set(float64(cluster.Spec.MaxSyncReplicas))

	syncReplicas, _ := cluster.GetSyncReplicasData()
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
			client.ObjectKey{Namespace: r.instance.Namespace, Name: reference.Name},
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
		err := r.GetClient().Get(ctx, client.ObjectKey{Namespace: r.instance.Namespace, Name: reference.Name}, &secret)
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

// RefreshSecrets is called when the PostgreSQL secrets are changed
// and will refresh the contents of the file inside the Pod, without
// reloading the actual PostgreSQL instance.
//
// It returns a boolean flag telling if something changed. Usually
// the invoker will check that flag and reload the PostgreSQL
// instance it is up.
//
// This function manages its own errors by logging them, so the
// user cannot easily tell if the operation has been done completely.
// The rationale behind this is:
//
//  1. when invoked at the startup of the instance manager, PostgreSQL
//     is not up. If this raise an error, then PostgreSQL won't
//     be able to start correctly (TLS certs are missing, i.e.),
//     making no difference between returning an error or not
//
//  2. when invoked inside the reconciliation loop, if the operation
//     raise an error, it's pointless to retry. The only way to recover
//     from such an error is wait for the CNPG operator to refresh the
//     resource version of the secrets to be used, and in that case a
//     reconciliation loop will be started again.
func (r *InstanceReconciler) RefreshSecrets(
	ctx context.Context,
	cluster *apiv1.Cluster,
) bool {
	contextLogger := log.FromContext(ctx)

	changed := false

	serverSecretChanged, err := r.refreshServerCertificateFiles(ctx, cluster)
	if err == nil {
		changed = changed || serverSecretChanged
	} else if !apierrors.IsNotFound(err) {
		contextLogger.Error(err, "Error while getting server secret")
	}

	replicationSecretChanged, err := r.refreshReplicationUserCertificate(ctx, cluster)
	if err == nil {
		changed = changed || replicationSecretChanged
	} else if !apierrors.IsNotFound(err) {
		contextLogger.Error(err, "Error while getting streaming replication secret")
	}

	clientCaSecretChanged, err := r.refreshClientCA(ctx, cluster)
	if err == nil {
		changed = changed || clientCaSecretChanged
	} else if !apierrors.IsNotFound(err) {
		contextLogger.Error(err, "Error while getting cluster CA Client secret")
	}

	serverCaSecretChanged, err := r.refreshServerCA(ctx, cluster)
	if err == nil {
		changed = changed || serverCaSecretChanged
	} else if !apierrors.IsNotFound(err) {
		contextLogger.Error(err, "Error while getting cluster CA Server secret")
	}

	barmanEndpointCaSecretChanged, err := r.refreshBarmanEndpointCA(ctx, cluster)
	if err == nil {
		changed = changed || barmanEndpointCaSecretChanged
	} else if !apierrors.IsNotFound(err) {
		contextLogger.Error(err, "Error while getting barman endpoint CA secret")
	}

	return changed
}

// reconcileInstance sets PostgreSQL instance parameters to current values
func (r *InstanceReconciler) reconcileInstance(cluster *apiv1.Cluster) {
	r.instance.PgCtlTimeoutForPromotion = cluster.GetPgCtlTimeoutForPromotion()
	r.instance.MaxSwitchoverDelay = cluster.GetMaxSwitchoverDelay()
	r.instance.MaxStopDelay = cluster.GetMaxStopDelay()
}

func (r *InstanceReconciler) reconcileCheckWalArchiveFile(cluster *apiv1.Cluster) error {
	filePath := filepath.Join(r.instance.PgData, archiver.CheckEmptyWalArchiveFile)
	for _, condition := range cluster.Status.Conditions {
		// If our current condition is archiving we can delete the file
		if condition.Type == string(apiv1.ConditionContinuousArchiving) && condition.Status == metav1.ConditionTrue {
			return fileutils.RemoveFile(filePath)
		}
	}

	return nil
}

// waitForConfigurationReload waits for the db to be up and
// the new configuration to be reloaded
func (r *InstanceReconciler) waitForConfigurationReload(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	// This function could also be called while the server is being
	// started up, so we are not sure that the server is really active.
	// Let's wait for that.
	if r.instance.ConfigSha256 == "" {
		return nil
	}

	err := r.instance.WaitForSuperuserConnectionAvailable()
	if err != nil {
		return fmt.Errorf("while applying new configuration: %w", err)
	}

	err = r.instance.WaitForConfigReloaded()
	if err != nil {
		return fmt.Errorf("while waiting for new configuration to be reloaded: %w", err)
	}

	status, err := r.instance.GetStatus()
	if err != nil {
		return fmt.Errorf("while applying new configuration: %w", err)
	}
	if status.MightBeUnavailableMaskedError != "" {
		return fmt.Errorf(
			"while applying new configuration encountered an error masked by mightBeUnavailable: %s",
			status.MightBeUnavailableMaskedError,
		)
	}

	if !status.PendingRestart {
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
			contextLogger.Info("Restarting primary in-place due to hot standby sensible parameters decrease")
			return r.Instance().RequestAndWaitRestartSmartFast()
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

	oldCluster := cluster.DeepCopy()
	cluster.Status.Phase = phase
	cluster.Status.PhaseReason = phaseReason
	return r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
}

// refreshCertificateFilesFromSecret receive a secret and rewrite the file
// corresponding to the server certificate
func (r *InstanceReconciler) refreshCertificateFilesFromSecret(
	ctx context.Context,
	secret *corev1.Secret,
	certificateLocation string,
	privateKeyLocation string,
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	certificate, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return false, fmt.Errorf("missing %s field in Secret", corev1.TLSCertKey)
	}

	privateKey, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return false, fmt.Errorf("missing %s field in Secret", corev1.TLSPrivateKeyKey)
	}

	certificateIsChanged, err := fileutils.WriteFileAtomic(certificateLocation, certificate, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing server certificate: %w", err)
	}

	if certificateIsChanged {
		contextLogger.Info("Refreshed configuration file",
			"filename", certificateLocation,
			"secret", secret.Name)
	}

	privateKeyIsChanged, err := fileutils.WriteFileAtomic(privateKeyLocation, privateKey, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing server private key: %w", err)
	}

	if certificateIsChanged {
		contextLogger.Info("Refreshed configuration file",
			"filename", privateKeyLocation,
			"secret", secret.Name)
	}

	return certificateIsChanged || privateKeyIsChanged, nil
}

// refreshCAFromSecret receive a secret and rewrite the ca.crt file to the provided location
func (r *InstanceReconciler) refreshCAFromSecret(
	ctx context.Context,
	secret *corev1.Secret,
	destLocation string,
) (bool, error) {
	caCertificate, ok := secret.Data[certs.CACertKey]
	if !ok {
		return false, fmt.Errorf("missing %s entry in Secret", certs.CACertKey)
	}

	changed, err := fileutils.WriteFileAtomic(destLocation, caCertificate, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing server certificate: %w", err)
	}

	if changed {
		log.FromContext(ctx).Info("Refreshed configuration file",
			"filename", destLocation,
			"secret", secret.Name)
	}

	return changed, nil
}

// refreshFileFromSecret receive a secret and rewrite the file corresponding to the key to the provided location
func (r *InstanceReconciler) refreshFileFromSecret(
	ctx context.Context,
	secret *corev1.Secret,
	key, destLocation string,
) (bool, error) {
	contextLogger := log.FromContext(ctx)
	data, ok := secret.Data[key]
	if !ok {
		return false, fmt.Errorf("missing %s entry in Secret", key)
	}

	changed, err := fileutils.WriteFileAtomic(destLocation, data, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing file: %w", err)
	}

	if changed {
		contextLogger.Info("Refreshed configuration file",
			"filename", destLocation,
			"secret", secret.Name,
			"key", key)
	}

	return changed, nil
}

// Reconciler primary logic. DB needed.
func (r *InstanceReconciler) reconcilePrimary(ctx context.Context, cluster *apiv1.Cluster) (restarted bool, err error) {
	if cluster.Status.TargetPrimary != r.instance.PodName || cluster.IsReplica() {
		return false, nil
	}

	oldCluster := cluster.DeepCopy()
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return false, err
	}

	// If I'm not the primary, let's promote myself
	if !isPrimary {
		cluster.LogTimestampsWithMessage(ctx, "Setting myself as primary")
		if err := r.promoteAndWait(ctx, cluster); err != nil {
			return false, err
		}
		restarted = true
	}

	// if the currentPrimary doesn't match the PodName we set the correct value.
	if cluster.Status.CurrentPrimary != r.instance.PodName {
		cluster.Status.CurrentPrimary = r.instance.PodName
		cluster.Status.CurrentPrimaryTimestamp = pkgUtils.GetCurrentTimestamp()

		if err := r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster)); err != nil {
			return restarted, err
		}

		if err := r.instance.DropConnections(); err != nil {
			return restarted, err
		}

		cluster.LogTimestampsWithMessage(ctx, "Finished setting myself as primary")
		return restarted, nil
	}

	// If it is already the current primary, everything is ok
	return restarted, nil
}

func (r *InstanceReconciler) promoteAndWait(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("I'm the target primary, wait for the wal_receiver to be terminated")
	if r.instance.PodName != cluster.Status.CurrentPrimary {
		// if the cluster is not replicating it means it's doing a failover and
		// we have to wait for wal receivers to be down
		err := r.waitForWalReceiverDown()
		if err != nil {
			return err
		}
	}

	contextLogger.Info("I'm the target primary, applying WALs and promoting my instance")
	// I must promote my instance here
	err := r.instance.PromoteAndWait()
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
	if cluster.Status.CurrentPrimary == r.instance.PodName {
		return false, nil
	}

	// We need to ensure that this instance is replicating from the correct server
	changed, err = r.refreshReplicaConfiguration(ctx, cluster)
	if err != nil {
		return changed, err
	}

	// I'm the primary, need to inform the operator
	log.FromContext(ctx).Info("Setting myself as the current designated primary")

	oldCluster := cluster.DeepCopy()
	cluster.Status.CurrentPrimary = r.instance.PodName
	cluster.Status.CurrentPrimaryTimestamp = pkgUtils.GetCurrentTimestamp()
	return changed, r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
}

// waitForWalReceiverDown wait until the wal receiver is down, and it's used
// to grab all the WAL files from a replica
func (r *InstanceReconciler) waitForWalReceiverDown() error {
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

		log.Info("WAL receiver is still active, waiting")
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
		client.ObjectKey{Namespace: r.instance.Namespace, Name: secretName},
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
				Namespace: r.instance.Namespace,
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
	return r.instance.RefreshPGHBA(cluster, ldapBindPassword)
}

func (r *InstanceReconciler) shouldRequeueForMissingTopology(cluster *apiv1.Cluster) shoudRequeue {
	syncReplicaConstraint := cluster.Spec.PostgresConfiguration.SyncReplicaElectionConstraint
	if !syncReplicaConstraint.Enabled {
		return false
	}
	if primary, _ := r.instance.IsPrimary(); !primary {
		return false
	}

	topologyStatus := cluster.Status.Topology
	if !topologyStatus.SuccessfullyExtracted || len(topologyStatus.Instances) != cluster.Spec.Instances {
		log.Info("missing topology information while syncReplicaElectionConstraint are enabled, " +
			"will requeue to calculate correctly the synchronous names")
		return true
	}

	return false
}
