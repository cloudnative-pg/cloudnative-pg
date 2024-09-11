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
	"time"

	"github.com/jackc/pgx/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance *postgres.Instance
}

// errClusterIsReplica is raised when the database object
// cannot be reconciled because it belongs to a replica cluster
var errClusterIsReplica = fmt.Errorf("waiting for the cluster to become primary")

// databaseReconciliationInterval is the time between the
// database reconciliation loop failures
const databaseReconciliationInterval = 30 * time.Second

// databaseFinalizerName is the name of the finalizer
// triggering the deletion of the database
const databaseFinalizerName = utils.MetadataNamespace + "/deleteDatabase"

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases/status,verbs=get;update;patch

// Reconcile is the database reconciliation loop
func (r *DatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug("Reconciliation loop start")
	defer func() {
		contextLogger.Debug("Reconciliation loop end")
	}()

	// Get the database object
	var database apiv1.Database
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &database); err != nil {
		// This is a deleted object, there's nothing
		// to do since we don't manage any finalizers.
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// This is not for me!
	if database.Spec.ClusterRef.Name != r.instance.ClusterName {
		return ctrl.Result{}, nil
	}

	// If everything is reconciled, we're done here
	if database.Generation == database.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The cluster has been deleted.
			// We just need to wait for this instance manager to be terminated
			contextLogger.Debug("Could not find Cluster")
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("could not fetch Cluster: %w", err)
	}

	// This is not for me, at least now
	if cluster.Status.CurrentPrimary != r.instance.PodName {
		return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, nil
	}

	// Still not for me, we're waiting for a switchover
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, nil
	}

	// Cannot do anything on a replica cluster
	if cluster.IsReplica() {
		return r.failedReconciliation(
			ctx,
			&database,
			errClusterIsReplica,
		)
	}

	// Add the finalizer if we don't have it
	// nolint:nestif
	if database.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(&database, databaseFinalizerName) {
			if err := r.Update(ctx, &database); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// This database is being deleted
		if controllerutil.ContainsFinalizer(&database, databaseFinalizerName) {
			if database.Spec.ReclaimPolicy == apiv1.DatabaseReclaimDelete {
				if err := r.dropPgDatabase(ctx, &database); err != nil {
					return ctrl.Result{}, err
				}
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(&database, databaseFinalizerName)
			if err := r.Update(ctx, &database); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}
	}

	if err = r.alignPgDatabase(
		ctx,
		&database,
	); err != nil {
		return r.failedReconciliation(
			ctx,
			&database,
			err,
		)
	}

	return r.succeededRenconciliation(
		ctx,
		&database,
	)
}

// failedReconciliation marks the reconciliation as failed and logs the corresponding error
func (r *DatabaseReconciler) failedReconciliation(
	ctx context.Context,
	database *apiv1.Database,
	err error,
) (ctrl.Result, error) {
	oldDatabase := database.DeepCopy()
	database.Status.Error = err.Error()
	database.Status.Ready = false

	var statusError *instance.StatusError
	if errors.As(err, &statusError) {
		// The body line of the instance manager contain the human
		// readable error
		database.Status.Error = statusError.Body
	}

	if err := r.Client.Status().Patch(ctx, database, client.MergeFrom(oldDatabase)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: databaseReconciliationInterval,
	}, nil
}

// succeededReconciliation marks the reconciliation as succeeded
func (r *DatabaseReconciler) succeededRenconciliation(
	ctx context.Context,
	database *apiv1.Database,
) (ctrl.Result, error) {
	oldDatabase := database.DeepCopy()
	database.Status.Error = ""
	database.Status.Ready = true
	database.Status.ObservedGeneration = database.Generation

	if err := r.Client.Status().Patch(ctx, database, client.MergeFrom(oldDatabase)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: databaseReconciliationInterval,
	}, nil
}

// NewDatabaseReconciler creates a new databare reconciler
func NewDatabaseReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *DatabaseReconciler {
	return &DatabaseReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Database{}).
		Complete(r)
}

// GetCluster gets the managed cluster through the client
func (r *DatabaseReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := r.Client.Get(ctx,
		types.NamespacedName{
			Namespace: r.instance.Namespace,
			Name:      r.instance.ClusterName,
		},
		&cluster)
	if err != nil {
		return nil, err
	}

	return &cluster, nil
}

func (r *DatabaseReconciler) alignPgDatabase(ctx context.Context, obj *apiv1.Database) error {
	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while connecting to the database: %w", err)
	}

	row := db.QueryRowContext(
		ctx,
		`
		SELECT count(*)
		FROM pg_database
	        WHERE datname = $1
		`,
		obj.Spec.Name)
	if row.Err() != nil {
		return fmt.Errorf("while getting DB status: %w", err)
	}

	var count int
	if err = row.Scan(&count); err != nil {
		return fmt.Errorf("while getting DB status (scan): %w", err)
	}

	if count > 0 {
		if err = r.patchDatabase(ctx, db, obj); err != nil {
			return err
		}
		return nil
	}

	return r.createDatabase(ctx, db, obj)
}

func (r *DatabaseReconciler) createDatabase(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Database,
) error {
	sqlCreateDatabase := fmt.Sprintf("CREATE DATABASE %s ", obj.Spec.Name)
	if obj.Spec.IsTemplate != nil {
		sqlCreateDatabase += fmt.Sprintf(" IS_TEMPLATE %v", *obj.Spec.IsTemplate)
	}
	if len(obj.Spec.Owner) > 0 {
		sqlCreateDatabase += fmt.Sprintf(" OWNER %s", pgx.Identifier{obj.Spec.Owner}.Sanitize())
	}
	if len(obj.Spec.Tablespace) > 0 {
		sqlCreateDatabase += fmt.Sprintf(" TABLESPACE %s", pgx.Identifier{obj.Spec.Tablespace}.Sanitize())
	}
	if obj.Spec.AllowConnections != nil {
		sqlCreateDatabase += fmt.Sprintf(" ALLOW_CONNECTIONS %v", *obj.Spec.AllowConnections)
	}
	if obj.Spec.ConnectionLimit != nil {
		sqlCreateDatabase += fmt.Sprintf(" CONNECTION LIMIT %v", *obj.Spec.ConnectionLimit)
	}

	_, err := db.ExecContext(ctx, sqlCreateDatabase)

	return err
}

func (r *DatabaseReconciler) patchDatabase(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Database,
) error {
	if len(obj.Spec.Owner) > 0 {
		changeOwnerSQL := fmt.Sprintf(
			"ALTER DATABASE %s OWNER TO %s",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			pgx.Identifier{obj.Spec.Owner}.Sanitize())

		if _, err := db.ExecContext(ctx, changeOwnerSQL); err != nil {
			return fmt.Errorf("alter database owner to: %w", err)
		}
	}

	if obj.Spec.IsTemplate != nil {
		changeIsTemplateSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH IS_TEMPLATE %v",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			*obj.Spec.IsTemplate)

		if _, err := db.ExecContext(ctx, changeIsTemplateSQL); err != nil {
			return fmt.Errorf("alter database with is_template: %w", err)
		}
	}

	if obj.Spec.AllowConnections != nil {
		changeAllowConnectionsSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH ALLOW_CONNECTIONS %v",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			*obj.Spec.AllowConnections)

		if _, err := db.ExecContext(ctx, changeAllowConnectionsSQL); err != nil {
			return fmt.Errorf("alter database with allow_connections: %w", err)
		}
	}

	if obj.Spec.ConnectionLimit != nil {
		changeConnectionsLimitSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH CONNECTION LIMIT %v",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			*obj.Spec.ConnectionLimit)

		if _, err := db.ExecContext(ctx, changeConnectionsLimitSQL); err != nil {
			return fmt.Errorf("alter database with connection limit: %w", err)
		}
	}

	if len(obj.Spec.Tablespace) > 0 {
		changeTablespaceSQL := fmt.Sprintf(
			"ALTER DATABASE %s SET TABLESPACE %s",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			pgx.Identifier{obj.Spec.Tablespace}.Sanitize())

		if _, err := db.ExecContext(ctx, changeTablespaceSQL); err != nil {
			return fmt.Errorf("alter database set tablespace: %w", err)
		}
	}

	return nil
}

func (r *DatabaseReconciler) dropPgDatabase(ctx context.Context, obj *apiv1.Database) error {
	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while connecting to the database: %w", err)
	}

	_, err = db.ExecContext(
		ctx,
		fmt.Sprintf("DROP DATABASE %s", pgx.Identifier{obj.Spec.Name}.Sanitize()),
	)
	return err
}
