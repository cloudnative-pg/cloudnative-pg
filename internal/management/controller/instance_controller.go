/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	postgresManagement "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metrics"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metricsserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
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
func (r *InstanceReconciler) Reconcile(ctx context.Context, event *watch.Event) error {
	r.log.V(2).Info(
		"Reconciliation loop",
		"eventType", event.Type,
		"type", event.Object.GetObjectKind().GroupVersionKind())

	cluster, err := utils.ObjectToCluster(event.Object)
	if err != nil {
		return fmt.Errorf("error decoding cluster resource: %w", err)
	}

	r.reconcileMetrics(cluster)

	// Reconcile monitoring section
	if cluster.Spec.Monitoring != nil {
		r.reconcileMonitoringQueries(ctx, cluster)
	}

	// Reconcile replica role
	if err := r.reconcileClusterRole(ctx, event, cluster); err != nil {
		return err
	}

	// Reconcile PostgreSQL configuration
	if err = r.reconcileConfiguration(ctx, cluster); err != nil {
		return fmt.Errorf("cannot apply new PostgreSQL configuration: %w", err)
	}

	if err = r.reconcileDatabases(ctx, cluster); err != nil {
		return fmt.Errorf("cannot reconcile database configurations: %w", err)
	}

	// Reconcile PostgreSQL instance parameters
	r.reconcileInstance(cluster)

	// Reconcile secrets and cryptographic material
	return r.reconcileSecrets(ctx, cluster)
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
		r.log.Error(err, "Cannot connect to primary server")
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

	if !extensionStatusChanged {
		// Nothing to do now, the list of extensions isn't changed
		return nil
	}

	databases, errors := r.getAllAccessibleDatabases(ctx, db)
	for _, databaseName := range databases {
		db, err := r.instance.ConnectionPool().Connection(databaseName)
		if err != nil {
			errors = append(errors,
				fmt.Errorf("could not connect to database %s: %w", databaseName, err))
			continue
		}
		if err = r.reconcileExtensions(ctx, db, cluster.Spec.PostgresConfiguration.Parameters); err != nil {
			errors = append(errors,
				fmt.Errorf("could not reconcile extensions for database %s: %w", databaseName, err))
			continue
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
	tx, err := db.BeginTx(ctx, nil)
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
			_, err = db.Exec(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", extension.Name))
		} else {
			_, err = db.Exec(fmt.Sprintf("DROP EXTENSION IF EXISTS %s", extension.Name))
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

// reconcileClusterRole applies the role written in the cluster status to this instance
func (r *InstanceReconciler) reconcileClusterRole(
	ctx context.Context, event *watch.Event, cluster *apiv1.Cluster) error {
	// Reconcile replica role
	if cluster.Status.TargetPrimary != r.instance.PodName {
		return r.reconcileReplica(ctx, cluster)
	}

	// Reconcile designated primary role
	if cluster.IsReplica() {
		return r.reconcileDesignatedPrimary(ctx, cluster)
	}

	// This is a primary server
	err := r.reconcilePrimary(ctx, cluster)
	if err != nil {
		return err
	}

	// Apply all the settings required by the operator
	if event.Type == watch.Added {
		return r.configureInstancePermissions()
	}

	return nil
}

// reconcileMetrics updates any required metrics
func (r *InstanceReconciler) reconcileMetrics(
	cluster *apiv1.Cluster,
) {
	exporter := metricsserver.GetExporter()
	// We should never reset the SwitchoverRequired metrics as it needs the primary instance restarts,
	// however, if the cluster is healthy we make sure it is set to 0.
	if cluster.Status.CurrentPrimary == r.instance.PodName {
		if cluster.Status.Phase == apiv1.PhaseWaitingForUser {
			exporter.Metrics.SwitchoverRequired.Set(1)
		} else {
			exporter.Metrics.SwitchoverRequired.Set(0)
		}
	}

	files, err := filepath.Glob(filepath.Join(specs.PgWalArchiveStatusPath, "*.ready"))
	if err != nil {
		log.Log.Error(err, "while reading directory", "path", specs.PgWalArchiveStatusPath)
		exporter.Metrics.Error.Set(1)
		exporter.Metrics.PgCollectionErrors.WithLabelValues("Collect.ArchiveCommandQueue").Inc()
		exporter.Metrics.ArchiveCommandQueueSize.Set(-1)
	} else {
		exporter.Metrics.ArchiveCommandQueueSize.Set(float64(len(files)))
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
	r.log.Info("Reconciling custom monitoring queries")

	configuration := cluster.Spec.Monitoring

	dbname := "postgres"
	if cluster.ShouldCreateApplicationDatabase() {
		dbname = cluster.Spec.Bootstrap.InitDB.Database
	}

	queries := metrics.NewQueriesCollector("cnp", r.instance, dbname)

	for _, reference := range configuration.CustomQueriesConfigMap {
		var configMap corev1.ConfigMap
		err := r.GetClient().Get(
			ctx,
			client.ObjectKey{Namespace: r.instance.Namespace, Name: reference.Name},
			&configMap)
		if err != nil {
			r.log.Info("Unable to get configMap containing custom monitoring queries",
				"reference", reference,
				"clusterName", r.instance.ClusterName,
				"namespace", r.instance.Namespace,
				"error", err.Error())
			continue
		}

		data, ok := configMap.Data[reference.Key]
		if !ok {
			r.log.Info("Missing key in configMap",
				"reference", reference,
				"clusterName", r.instance.ClusterName,
				"namespace", r.instance.Namespace)
			continue
		}

		err = queries.ParseQueries([]byte(data))
		if err != nil {
			r.log.Info("Error while parsing custom queries in ConfigMap",
				"reference", reference,
				"clusterName", r.instance.ClusterName,
				"namespace", r.instance.Namespace,
				"error", err.Error())
			continue
		}
	}

	for _, reference := range configuration.CustomQueriesSecret {
		var secret corev1.Secret
		err := r.GetClient().Get(ctx, client.ObjectKey{Namespace: r.instance.Namespace, Name: reference.Name}, &secret)
		if err != nil {
			r.log.Info("Unable to get secret containing custom monitoring queries",
				"reference", reference,
				"clusterName", r.instance.ClusterName,
				"namespace", r.instance.Namespace,
				"error", err.Error())
			continue
		}

		data, ok := secret.Data[reference.Key]
		if !ok {
			r.log.Info("Missing key in secret",
				"reference", reference,
				"clusterName", r.instance.ClusterName,
				"namespace", r.instance.Namespace)
			continue
		}

		err = queries.ParseQueries(data)
		if err != nil {
			r.log.Info("Error while parsing custom queries in Secret",
				"reference", reference,
				"clusterName", r.instance.ClusterName,
				"namespace", r.instance.Namespace,
				"error", err.Error())
			continue
		}
	}

	metricsserver.GetExporter().SetCustomQueries(queries)
}

// reconcileSecret is called when the PostgreSQL secrets are changes
func (r *InstanceReconciler) reconcileSecrets(
	ctx context.Context,
	cluster *apiv1.Cluster,
) error {
	changed := false

	serverSecretChanged, err := r.RefreshServerCertificateFiles(ctx, cluster)
	if err == nil {
		changed = changed || serverSecretChanged
	} else if !apierrors.IsNotFound(err) {
		r.log.Error(err, "Error while getting server secret")
	}

	replicationSecretChanged, err := r.RefreshReplicationUserCertificate(ctx, cluster)
	if err == nil {
		changed = changed || replicationSecretChanged
	} else if !apierrors.IsNotFound(err) {
		r.log.Error(err, "Error while getting streaming replication secret")
	}

	clientCaSecretChanged, err := r.RefreshClientCA(ctx, cluster)
	if err == nil {
		changed = changed || clientCaSecretChanged
	} else if !apierrors.IsNotFound(err) {
		r.log.Error(err, "Error while getting cluster CA Client secret")
	}

	serverCaSecretChanged, err := r.RefreshServerCA(ctx, cluster)
	if err == nil {
		changed = changed || serverCaSecretChanged
	} else if !apierrors.IsNotFound(err) {
		r.log.Error(err, "Error while getting cluster CA Server secret")
	}

	barmanEndpointCaSecretChanged, err := r.RefreshBarmanEndpointCA(ctx, cluster)
	if err == nil {
		changed = changed || barmanEndpointCaSecretChanged
	} else if !apierrors.IsNotFound(err) {
		r.log.Error(err, "Error while getting barman endpoint CA secret")
	}

	if changed {
		r.log.Info("reloading the TLS crypto material")
		err = r.instance.Reload()
		if err != nil {
			return fmt.Errorf("while applying new certificates: %w", err)
		}
	}

	err = r.refreshCredentialsFromSecret(ctx, cluster)
	if err != nil {
		return fmt.Errorf("while updating database owner password: %w", err)
	}

	return nil
}

// reconcileInstance sets PostgreSQL instance parameters to current values
func (r *InstanceReconciler) reconcileInstance(cluster *apiv1.Cluster) {
	r.instance.PgCtlTimeoutForPromotion = cluster.GetPgCtlTimeoutForPromotion()
}

// reconcileConfiguration reconcile the PostgreSQL configuration from
// the cluster object to the instance
func (r *InstanceReconciler) reconcileConfiguration(ctx context.Context, cluster *apiv1.Cluster) error {
	changed, err := r.instance.RefreshConfigurationFilesFromCluster(cluster)
	if err != nil {
		return err
	}

	if !changed {
		return nil
	}

	// This function could also be called while the server is being
	// started up, so we are not sure that the server is really active.
	// Let's wait for that.
	err = r.instance.WaitForSuperuserConnectionAvailable()
	if err != nil {
		return fmt.Errorf("while applying new configuration: %w", err)
	}

	// Ok, now we're ready to SIGHUP this server
	err = r.instance.Reload()
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

	// I'm not the first instance spotting the configuration
	// change, everything if fine and there is no need to signal
	// the operator again
	if cluster.Status.Phase == apiv1.PhaseApplyingConfiguration ||
		// don't trigger the reconciliation loop from the primary,
		// let's wait for replicas to trigger it first
		(status.IsPrimary && cluster.Spec.Instances > 1) {
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
	secret *corev1.Secret,
	certificateLocation string,
	privateKeyLocation string,
) (bool, error) {
	certificate, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return false, fmt.Errorf("missing %s field in Secret", corev1.TLSCertKey)
	}

	privateKey, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return false, fmt.Errorf("missing %s field in Secret", corev1.TLSPrivateKeyKey)
	}

	certificateIsChanged, err := fileutils.WriteFile(certificateLocation, certificate, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing server certificate: %w", err)
	}

	if certificateIsChanged {
		r.log.Info("Refreshed configuration file",
			"filename", certificateLocation,
			"secret", secret.Name)
	}

	privateKeyIsChanged, err := fileutils.WriteFile(privateKeyLocation, privateKey, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing server private key: %w", err)
	}

	if certificateIsChanged {
		r.log.Info("Refreshed configuration file",
			"filename", privateKeyLocation,
			"secret", secret.Name)
	}

	return certificateIsChanged || privateKeyIsChanged, nil
}

// refreshCAFromSecret receive a secret and rewrite the ca.crt file to the provided location
func (r *InstanceReconciler) refreshCAFromSecret(secret *corev1.Secret, destLocation string) (bool, error) {
	caCertificate, ok := secret.Data[certs.CACertKey]
	if !ok {
		return false, fmt.Errorf("missing %s entry in Secret", certs.CACertKey)
	}

	changed, err := fileutils.WriteFile(destLocation, caCertificate, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing server certificate: %w", err)
	}

	if changed {
		r.log.Info("Refreshed configuration file",
			"filename", destLocation,
			"secret", secret.Name)
	}

	return changed, nil
}

// refreshFileFromSecret receive a secret and rewrite the file corresponding to the key to the provided location
func (r *InstanceReconciler) refreshFileFromSecret(secret *corev1.Secret, key, destLocation string) (bool, error) {
	data, ok := secret.Data[key]
	if !ok {
		return false, fmt.Errorf("missing %s entry in Secret", key)
	}

	changed, err := fileutils.WriteFile(destLocation, data, 0o600)
	if err != nil {
		return false, fmt.Errorf("while writing file: %w", err)
	}

	if changed {
		r.log.Info("Refreshed configuration file",
			"filename", destLocation,
			"secret", secret.Name,
			"key", key)
	}

	return changed, nil
}

// Reconciler primary logic
func (r *InstanceReconciler) reconcilePrimary(ctx context.Context, cluster *apiv1.Cluster) error {
	oldCluster := cluster.DeepCopy()
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}

	// If I'm not the primary, let's promote myself
	if !isPrimary {
		r.log.Info("I'm the target primary, wait for the wal_receiver to be terminated")
		if r.instance.PodName != cluster.Status.CurrentPrimary {
			// if the cluster is not replicating it means it's doing a failover and
			// we have to wait for wal receivers to be down
			err = r.waitForWalReceiverDown()
			if err != nil {
				return err
			}
		}
		r.log.Info("I'm the target primary, applying WALs and promoting my instance")
		// I must promote my instance here
		err = r.instance.PromoteAndWait()
		if err != nil {
			return fmt.Errorf("error promoting instance: %w", err)
		}
	}

	// If it is already the current primary, everything is ok
	if cluster.Status.CurrentPrimary != r.instance.PodName {
		cluster.Status.CurrentPrimary = r.instance.PodName
		r.log.Info("Setting myself as the current primary")
		return r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
	}

	return nil
}

// Reconciler designated primary logic for replica clusters
func (r *InstanceReconciler) reconcileDesignatedPrimary(ctx context.Context, cluster *apiv1.Cluster) error {
	// If I'm already the current designated primary everything is ok.
	if cluster.Status.CurrentPrimary == r.instance.PodName {
		return nil
	}

	// We need to ensure that this instance is replicating from the correct server
	if err := r.refreshParentServer(ctx, cluster); err != nil {
		return err
	}

	// I'm the primary, need to inform the operator
	r.log.Info("Setting myself as the current designated primary")

	oldCluster := cluster.DeepCopy()
	cluster.Status.CurrentPrimary = r.instance.PodName
	return r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
}

// Reconciler replica logic
func (r *InstanceReconciler) reconcileReplica(ctx context.Context, cluster *apiv1.Cluster) error {
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}

	if !isPrimary {
		// We need to ensure that this instance is replicating from the correct server
		return r.refreshParentServer(ctx, cluster)
	}

	r.log.Info("This is an old primary node. Requesting a checkpoint before demotion")

	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		r.log.Error(err, "Cannot connect to primary server")
	} else {
		_, err = db.Exec("CHECKPOINT")
		if err != nil {
			r.log.Error(err, "Error while requesting a checkpoint")
		}
	}

	r.log.Info("This is an old primary node. Shutting it down to get it demoted to a replica")

	// I was the primary, but now I'm not the primary anymore.
	// Here we need to invoke a fast shutdown on the instance, and wait the the pod
	// restart to demote as a replica of the new primary
	return r.instance.Shutdown()
}

// refreshParentServer will ensure that this replica instance is actually replicating from the correct
// parent server, which is the external server for the designated primary and the designated primary
// for the replicas
func (r *InstanceReconciler) refreshParentServer(ctx context.Context, cluster *apiv1.Cluster) error {
	// Let's update the replication configuration
	changed, err := r.WriteReplicaConfiguration(ctx, cluster)
	if err != nil {
		return err
	}

	// Reload the replication configuration if configuration is changed
	if changed {
		return r.instance.Reload()
	}

	return nil
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

		r.log.Info("WAL receiver is still active, waiting")
		return false, nil
	})
}

// configureInstancePermissions creates the expected users and databases in a new
// PostgreSQL instance
func (r *InstanceReconciler) configureInstancePermissions() error {
	var err error

	majorVersion, err := postgres.GetMajorVersion(r.instance.PgData)
	if err != nil {
		return fmt.Errorf("while getting major version: %w", err)
	}

	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting a connection to the instance: %w", err)
	}

	r.log.Info("Verifying connection to DB")
	err = r.instance.WaitForSuperuserConnectionAvailable()
	if err != nil {
		r.log.Error(err, "DB not available")
		os.Exit(1)
	}

	r.log.V(2).Info("Validating DB configuration")

	// A transaction is required to temporarily disable synchronous replication
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("creating a new transaction to setup the instance: %w", err)
	}

	_, err = tx.Exec("SET LOCAL synchronous_commit TO LOCAL")
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	hasSuperuser, err := r.configureStreamingReplicaUser(tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	err = r.configurePgRewindPrivileges(majorVersion, hasSuperuser, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// configureStreamingReplicaUser makes sure the the streaming replication user exists
// and has the required rights
func (r *InstanceReconciler) configureStreamingReplicaUser(tx *sql.Tx) (bool, error) {
	var hasLoginRight, hasReplicationRight, hasSuperuser bool
	row := tx.QueryRow("SELECT rolcanlogin, rolreplication, rolsuper FROM pg_roles WHERE rolname = $1",
		apiv1.StreamingReplicationUser)
	err := row.Scan(&hasLoginRight, &hasReplicationRight, &hasSuperuser)
	if err != nil {
		if err == sql.ErrNoRows {
			_, err = tx.Exec(fmt.Sprintf(
				"CREATE USER %v REPLICATION",
				pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
			if err != nil {
				return false, fmt.Errorf("CREATE USER %v error: %w", apiv1.StreamingReplicationUser, err)
			}
		} else {
			return false, fmt.Errorf("while creating streaming replication user: %w", err)
		}
	}

	if !hasLoginRight || !hasReplicationRight {
		_, err = tx.Exec(fmt.Sprintf(
			"ALTER USER %v LOGIN REPLICATION",
			pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
		if err != nil {
			return false, fmt.Errorf("ALTER USER %v error: %w", apiv1.StreamingReplicationUser, err)
		}
	}
	return hasSuperuser, nil
}

// configurePgRewindPrivileges ensures that the StreamingReplicationUser has enough rights to execute pg_rewind
func (r *InstanceReconciler) configurePgRewindPrivileges(majorVersion int, hasSuperuser bool, tx *sql.Tx) error {
	// We need the superuser bit for the streaming-replication user since pg_rewind in PostgreSQL <= 10
	// will require it.
	if majorVersion <= 10 {
		if !hasSuperuser {
			_, err := tx.Exec(fmt.Sprintf(
				"ALTER USER %v SUPERUSER",
				pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
			if err != nil {
				return fmt.Errorf("ALTER USER %v error: %w", apiv1.StreamingReplicationUser, err)
			}
		}
		return nil
	}

	// Ensure the user has rights to execute the functions needed for pg_rewind
	var hasPgRewindPrivileges bool
	row := tx.QueryRow(
		`
			SELECT has_function_privilege($1, 'pg_ls_dir(text, boolean, boolean)', 'execute') AND
			       has_function_privilege($2, 'pg_stat_file(text, boolean)', 'execute') AND
			       has_function_privilege($3, 'pg_read_binary_file(text)', 'execute') AND
			       has_function_privilege($4, 'pg_read_binary_file(text, bigint, bigint, boolean)', 'execute')`,
		apiv1.StreamingReplicationUser,
		apiv1.StreamingReplicationUser,
		apiv1.StreamingReplicationUser,
		apiv1.StreamingReplicationUser)
	err := row.Scan(&hasPgRewindPrivileges)
	if err != nil {
		return fmt.Errorf("while getting streaming replication user privileges: %w", err)
	}

	if !hasPgRewindPrivileges {
		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_ls_dir(text, boolean, boolean) TO %v",
			pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_stat_file(text, boolean) TO %v",
			pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_read_binary_file(text) TO %v",
			pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_read_binary_file(text, bigint, bigint, boolean) TO %v",
			pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}
	}

	return nil
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

	// Let's get the credentials from the secrets
	if err = r.reconcileUser(ctx, cluster.GetSuperuserSecretName(), tx); err != nil {
		return err
	}
	if err = r.reconcileUser(ctx, cluster.GetApplicationSecretName(), tx); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *InstanceReconciler) reconcileUser(ctx context.Context, secretName string, tx *sql.Tx) error {
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

	username, password, err := utils.GetUserPasswordFromSecret(&secret)
	if err != nil {
		return err
	}

	_, err = tx.Exec(fmt.Sprintf("ALTER ROLE %v WITH PASSWORD %v",
		username,
		pq.QuoteLiteral(password)))
	if err == nil {
		r.secretVersions[secret.Name] = secret.ResourceVersion
	}
	return err
}
