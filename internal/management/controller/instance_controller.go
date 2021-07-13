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
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metrics"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metricsserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
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

	// Reconcile PostgreSQL instance parameters
	r.reconcileInstance(cluster)

	// Reconcile secrets and cryptographic material
	return r.reconcileSecrets(ctx, cluster)
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

	if changed {
		r.log.Info("reloading the TLS crypto material")
		err = r.instance.Reload()
		if err != nil {
			return fmt.Errorf("while applying new certificates: %w", err)
		}
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

// refreshConfigurationFilesFromObject receive an unstructured object representing
// a secret and rewrite the file corresponding to the server certificate
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
