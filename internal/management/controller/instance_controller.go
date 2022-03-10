/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/controllers"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/walrestore"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	barmanCredentials "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman/credentials"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	postgresManagement "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metrics"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver/metricserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	pkgUtils "github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
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

// Reconcile is the main reconciliation loop for the instance
func (r *InstanceReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// set up a convenient contextLog object so we don't have to type request over and over again
	contextLogger, ctx := log.SetupLogger(ctx)

	// if the context has already been cancelled,
	// trying to reconcile would just lead to misleading errors being reported
	if err := ctx.Err(); err != nil {
		contextLogger.Warning("Context cancelled, will not reconcile", "err", err)
		return ctrl.Result{}, nil
	}
	result := reconcile.Result{}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The cluster has been deleted.
			// We just need to wait for this instance manager to be terminated
			contextLogger.Debug("Could not find Cluster")
			return result, nil
		}

		return result, fmt.Errorf("could not fetch Cluster: %w", err)
	}

	// Print the Cluster
	contextLogger.Debug("Reconciling Cluster", "cluster", cluster)

	// Reconcile PostgreSQL instance parameters
	r.reconcileInstance(cluster)

	// Refresh the cache
	r.updateCacheFromCluster(ctx, cluster)

	// Reconcile monitoring section
	if r.metricsServerExporter != nil {
		r.reconcileMetrics(cluster)
		r.reconcileMonitoringQueries(ctx, cluster)
	} else {
		result.RequeueAfter = 1 * time.Second
	}

	// Reconcile secrets and cryptographic material
	// This doesn't need the PG connection, but it needs to reload it in case of changes
	reloadNeeded := r.RefreshSecrets(ctx, cluster)

	// Reconcile PostgreSQL configuration
	// This doesn't need the PG connection, but it needs to reload it in case of changes
	reloadConfig, err := r.instance.RefreshConfigurationFilesFromCluster(cluster)
	if err != nil {
		return result, err
	}
	reloadNeeded = reloadNeeded || reloadConfig

	reloadReplicaConfig, err := r.refreshReplicaConfiguration(ctx, cluster)
	if err != nil {
		return result, err
	}
	reloadNeeded = reloadNeeded || reloadReplicaConfig

	// here we execute initialization tasks that need to be executed only verifiedPrimaryPgDataCoherence successfully
	if !r.verifiedPrimaryPgDataCoherence.Load() {
		if err = r.verifyPgDataCoherenceForPrimary(ctx, cluster); err != nil {
			return handleErrNextLoop(err, result)
		}
		r.verifiedPrimaryPgDataCoherence.Store(true)
	}

	// Reconcile cluster role without DB
	reloadClusterRoleConfig, err := r.reconcileClusterRoleWithoutDB(ctx, cluster)
	if err != nil {
		return reconcile.Result{RequeueAfter: time.Second}, err
	}
	reloadNeeded = reloadNeeded || reloadClusterRoleConfig

	r.systemInitialization.Broadcast()

	err = r.instance.IsServerHealthy()
	if err != nil {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	restarted, err := r.reconcileOldPrimary(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.IsDBUp(ctx)
	if err != nil {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	// from now on the database can be assumed as running

	if reloadNeeded && !restarted {
		contextLogger.Info("reloading the instance")
		err = r.instance.Reload()
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("while reloading the instance: %w", err)
		}
		if err := r.waitForConfigurationReload(ctx, cluster); err != nil {
			return reconcile.Result{}, fmt.Errorf("cannot apply new PostgreSQL configuration: %w", err)
		}
	}

	err = r.refreshCredentialsFromSecret(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("while updating database owner password: %w", err)
	}

	if err := r.reconcileDatabases(ctx, cluster); err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot reconcile database configurations: %w", err)
	}

	return result, nil
}

func handleErrNextLoop(err error, result reconcile.Result) (reconcile.Result, error) {
	if errors.Is(err, controllers.ErrNextLoop) {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}
	return result, err
}

// reconcileOldPrimary shuts down the instance in case it is an old primary
func (r *InstanceReconciler) reconcileOldPrimary(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (restarted bool, err error) {
	contextLogger := log.FromContext(ctx)
	// db needed
	if cluster.Status.TargetPrimary == r.instance.PodName {
		if !cluster.IsReplica() {
			return r.reconcilePrimary(ctx, cluster)
		}
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

// updateCacheFromCluster refreshes the reconciler internal cache using the provided cluster
func (r *InstanceReconciler) updateCacheFromCluster(ctx context.Context, cluster *apiv1.Cluster) {
	cache.Store(cache.ClusterKey, cluster)

	// Populate the cache with the backup configuration
	if cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil {
		envArchive, err := barmanCredentials.EnvSetBackupCloudCredentials(
			ctx,
			r.GetClient(),
			cluster.Namespace,
			cluster.Spec.Backup.BarmanObjectStore,
			os.Environ())
		if err != nil {
			log.Error(err, "while getting backup credentials")
		} else {
			cache.Store(cache.WALArchiveKey, envArchive)
		}
	} else {
		cache.Delete(cache.WALArchiveKey)
	}

	// Populate the cache with the recover configuration
	_, env, barmanConfiguration, err := walrestore.GetRecoverConfiguration(cluster, r.instance.PodName)
	if errors.Is(err, walrestore.ErrNoBackupConfigured) {
		cache.Delete(cache.WALRestoreKey)
		return
	}
	if err != nil {
		log.Error(err, "while getting recover configuration")
		return
	}
	env = append(env, os.Environ()...)

	envRestore, err := barmanCredentials.EnvSetBackupCloudCredentials(
		ctx,
		r.GetClient(),
		cluster.Namespace,
		barmanConfiguration,
		env,
	)
	if err != nil {
		log.Error(err, "while getting recover credentials")
	}
	cache.Store(cache.WALRestoreKey, envRestore)
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
		if err := tx.Rollback(); err != nil {
			errors = append(errors, err)
		}
	}()

	databases, errors = postgresManagement.GetAllAccessibleDatabases(tx, "datallowconn")
	return databases, errors
}

// ReconcileExtensions reconciles the expected extensions for this
// PostgreSQL instance
func (r *InstanceReconciler) reconcileExtensions(
	ctx context.Context, db *sql.DB, userSettings map[string]string) (err error) {
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
		if !extension.SkipCreateExtension && extensionIsUsed {
			_, err = tx.Exec(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", extension.Name))
		} else {
			_, err = tx.Exec(fmt.Sprintf("DROP EXTENSION IF EXISTS %s", extension.Name))
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
	ctx context.Context, db *sql.DB, dbName string, integrations *apiv1.PoolerIntegrations) (err error) {
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
		postgres.PGBouncerPoolerUserName))
	err = row.Scan(&existsRole)
	if err != nil {
		return err
	}
	if !existsRole {
		_, err := tx.Exec(fmt.Sprintf("CREATE ROLE %s WITH LOGIN", postgres.PGBouncerPoolerUserName))
		if err != nil {
			return err
		}
		_, err = tx.Exec(fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", dbName, postgres.PGBouncerPoolerUserName))
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
			postgres.PGBouncerPoolerUserName))
		if err != nil {
			return err
		}
	}

	if !existsRole || !existsFunction {
		return tx.Commit()
	}
	return nil
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
	exporter.Metrics.SyncReplicas.WithLabelValues("expected").Set(float64(cluster.GetSyncReplicasNumber()))
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
	if cluster.ShouldCreateApplicationDatabase() {
		dbname = cluster.Spec.Bootstrap.InitDB.Database
	}

	queriesCollector := metrics.NewQueriesCollector("cnp", r.instance, dbname)
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
// 1. when invoked at the startup of the instance manager, PostgreSQL
//    is not up. If this raise an error, then PostgreSQL won't
//    be able to start correctly (TLS certs are missing, i.e.),
//    making no difference between returning an error or not
//
// 2. when invoked inside the reconciliation loop, if the operation
//    raise an error, it's pointless to retry. The only way to recover
//    from such an error is wait for the CNP operator to refresh the
//    resource version of the secrets to be used, and in that case a
//    reconciliation loop will be started again.
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

	if !status.PendingRestart {
		// Everything fine
		return nil
	}

	// if there is a pending restart, the instance is a primary and
	// the restart is due to a decrease of sensible parameters,
	// we will need to restart the primary instance in place
	if status.IsPrimary && status.PendingRestartForDecrease {
		contextLogger.Info("Restarting primary inplace due to hot standby sensible parameters decrease")
		if err := r.Instance().RequestAndWaitRestartSmartFast(); err != nil {
			return err
		}
	}

	if cluster.Status.Phase == apiv1.PhaseApplyingConfiguration ||
		(status.IsPrimary && cluster.Spec.Instances > 1) {
		// I'm not the first instance spotting the configuration
		// change, everything is fine and there is no need to signal
		// the operator again.
		// We also don't want to trigger the reconciliation loop from the primary
		// for parameters which are not to be considered sensible for hot standbys,
		// so, we will wait for replicas to trigger it first.
		return nil
	}

	oldCluster := cluster.DeepCopy()
	cluster.Status.Phase = apiv1.PhaseApplyingConfiguration
	cluster.Status.PhaseReason = "PostgreSQL configuration changed"
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

// Reconciler primary logic
func (r *InstanceReconciler) reconcilePrimary(ctx context.Context, cluster *apiv1.Cluster) (restarted bool, err error) {
	contextLogger := log.FromContext(ctx)
	oldCluster := cluster.DeepCopy()
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return false, err
	}

	if !isPrimary {
		// If I'm not the primary, let's promote myself
		err := r.promoteAndWait(ctx, cluster)
		if err != nil {
			return false, err
		}
		restarted = true
	}

	// If it is already the current primary, everything is ok
	if cluster.Status.CurrentPrimary != r.instance.PodName {
		cluster.Status.CurrentPrimary = r.instance.PodName
		cluster.Status.CurrentPrimaryTimestamp = pkgUtils.GetCurrentTimestamp()
		contextLogger.Info("Setting myself as the current primary")
		return restarted, r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
	}

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
	cluster *apiv1.Cluster) error {
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

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		// This has no effect if the transaction
		// is committed
		_ = tx.Rollback()
	}()

	_, err = tx.Exec("SET LOCAL synchronous_commit to LOCAL")
	if err != nil {
		return err
	}

	if cluster.GetEnableSuperuserAccess() {
		err = r.reconcileUser(ctx, "postgres", cluster.GetSuperuserSecretName(), tx)
		if err != nil {
			return err
		}
	} else {
		err = r.disableSuperuserPassword(tx)
		if err != nil {
			return err
		}
	}

	if cluster.ShouldCreateApplicationDatabase() {
		err = r.reconcileUser(ctx, cluster.Spec.Bootstrap.InitDB.Owner, cluster.GetApplicationSecretName(), tx)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *InstanceReconciler) reconcileUser(ctx context.Context, username string, secretName string, tx *sql.Tx) error {
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

	_, err = tx.Exec(fmt.Sprintf("ALTER ROLE %v WITH PASSWORD %v",
		username,
		pq.QuoteLiteral(password)))
	if err == nil {
		r.secretVersions[secret.Name] = secret.ResourceVersion
	} else {
		err = fmt.Errorf("while running ALTER ROLE %v WITH PASSWORD", username)
	}

	return err
}

func (r *InstanceReconciler) disableSuperuserPassword(tx *sql.Tx) error {
	_, err := tx.Exec("ALTER ROLE postgres WITH PASSWORD NULL")
	return err
}
