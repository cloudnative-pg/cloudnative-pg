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
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PublicationReconciler reconciles a Publication object
type PublicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance *postgres.Instance
}

// publicationReconciliationInterval is the time between the
// publication reconciliation loop failures
const publicationReconciliationInterval = 30 * time.Second

// publicationFinalizerName is the name of the finalizer
// triggering the deletion of the publication
const publicationFinalizerName = utils.MetadataNamespace + "/deletePublication"

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=publications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=publications/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Publication object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *PublicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug("Reconciliation loop start")
	defer func() {
		contextLogger.Debug("Reconciliation loop end")
	}()

	// Get the publication object
	var publication apiv1.Publication
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &publication); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
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

	// This is not for me!
	if publication.Spec.ClusterRef.Name != r.instance.GetClusterName() {
		return ctrl.Result{}, nil
	}

	// This is not for me, at least now
	if cluster.Status.CurrentPrimary != r.instance.GetPodName() {
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
			&publication,
			errClusterIsReplica,
		)
	}

	// Add the finalizer if we don't have it
	// nolint:nestif
	if publication.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(&publication, publicationFinalizerName) {
			if err := r.Update(ctx, &publication); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// This publication is being deleted
		if controllerutil.ContainsFinalizer(&publication, publicationFinalizerName) {
			if publication.Spec.ReclaimPolicy == apiv1.PublicationReclaimDelete {
				if err := r.dropPublication(ctx, &publication); err != nil {
					return ctrl.Result{}, err
				}
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(&publication, publicationFinalizerName)
			if err := r.Update(ctx, &publication); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}
	}

	// If everything is reconciled, we're done here
	if publication.Generation == publication.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	if err := r.alignPublication(
		ctx,
		&publication,
	); err != nil {
		return r.failedReconciliation(
			ctx,
			&publication,
			err,
		)
	}

	return r.succeededRenconciliation(
		ctx,
		&publication,
	)
}

// NewPublicationReconciler creates a new publication reconciler
func NewPublicationReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *PublicationReconciler {
	return &PublicationReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PublicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Publication{}).
		Complete(r)
}

// failedReconciliation marks the reconciliation as failed and logs the corresponding error
func (r *PublicationReconciler) failedReconciliation(
	ctx context.Context,
	publication *apiv1.Publication,
	err error,
) (ctrl.Result, error) {
	oldPublication := publication.DeepCopy()
	publication.Status.Error = err.Error()
	publication.Status.Ready = false

	var statusError *instance.StatusError
	if errors.As(err, &statusError) {
		// The body line of the instance manager contain the human
		// readable error
		publication.Status.Error = statusError.Body
	}

	if err := r.Client.Status().Patch(ctx, publication, client.MergeFrom(oldPublication)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: databaseReconciliationInterval,
	}, nil
}

// succeededReconciliation marks the reconciliation as succeeded
func (r *PublicationReconciler) succeededRenconciliation(
	ctx context.Context,
	publication *apiv1.Publication,
) (ctrl.Result, error) {
	oldPublication := publication.DeepCopy()
	publication.Status.Error = ""
	publication.Status.Ready = true
	publication.Status.ObservedGeneration = publication.Generation

	if err := r.Client.Status().Patch(ctx, publication, client.MergeFrom(oldPublication)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: publicationReconciliationInterval,
	}, nil
}

// GetCluster gets the managed cluster through the client
func (r *PublicationReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := r.Client.Get(ctx,
		types.NamespacedName{
			Namespace: r.instance.GetNamespaceName(),
			Name:      r.instance.GetClusterName(),
		},
		&cluster)
	if err != nil {
		return nil, err
	}

	return &cluster, nil
}

func (r *PublicationReconciler) alignPublication(ctx context.Context, obj *apiv1.Publication) error {
	db, err := r.instance.ConnectionPool().Connection(obj.Spec.DBName)
	if err != nil {
		return fmt.Errorf("while getting DB connection: %w", err)
	}

	row := db.QueryRowContext(
		ctx,
		`
		SELECT count(*)
		FROM pg_publication
	        WHERE pubname = $1
		`,
		obj.Spec.Name)
	if row.Err() != nil {
		return fmt.Errorf("while getting publication status: %w", row.Err())
	}

	var count int
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("while getting publication status (scan): %w", err)
	}

	if count > 0 {
		if err := r.patchPublication(ctx, db, obj); err != nil {
			return fmt.Errorf("while patching publication: %w", err)
		}
		return nil
	}

	if err := r.createPublication(ctx, db, obj); err != nil {
		return fmt.Errorf("while creating publication: %w", err)
	}

	return nil
}

func (r *PublicationReconciler) patchPublication(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Publication,
) error {
	sqls := toPublicationAlterSQL(obj)
	for _, sqlQuery := range sqls {
		if _, err := db.ExecContext(ctx, sqlQuery); err != nil {
			return err
		}
	}

	return nil
}

func (r *PublicationReconciler) createPublication(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Publication,
) error {
	sqls := toPublicationCreateSQL(obj)
	for _, sqlQuery := range sqls {
		if _, err := db.ExecContext(ctx, sqlQuery); err != nil {
			return err
		}
	}
	return nil
}

func toPublicationCreateSQL(obj *apiv1.Publication) []string {
	result := make([]string, 0, 2)

	result = append(result,
		fmt.Sprintf(
			"CREATE PUBLICATION %s %s",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			toPublicationTargetSQL(&obj.Spec.Target),
		),
	)

	if len(obj.Spec.Owner) > 0 {
		result = append(result,
			fmt.Sprintf(
				"ALTER PUBLICATION %s OWNER to %s",
				pgx.Identifier{obj.Spec.Name}.Sanitize(),
				toPublicationTargetSQL(&obj.Spec.Target),
			),
		)
	}

	if len(obj.Spec.Parameters) > 0 {
		result = append(result,
			fmt.Sprintf("%s WITH (%s)", result, obj.Spec.Parameters),
		)
	}

	return result
}

func toPublicationAlterSQL(obj *apiv1.Publication) []string {
	result := make([]string, 0, 3)
	result = append(result,
		fmt.Sprintf(
			"ALTER PUBLICATION %s SET %s",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			toPublicationTargetSQL(&obj.Spec.Target),
		),
	)

	if len(obj.Spec.Owner) > 0 {
		result = append(result,
			fmt.Sprintf(
				"ALTER PUBLICATION %s OWNER TO %s",
				pgx.Identifier{obj.Spec.Name}.Sanitize(),
				pgx.Identifier{obj.Spec.Owner}.Sanitize(),
			),
		)
	}

	if len(obj.Spec.Parameters) > 0 {
		result = append(result,
			fmt.Sprintf(
				"ALTER PUBLICATION %s SET (%s)",
				result,
				obj.Spec.Parameters,
			),
		)
	}

	return result
}

func (r *PublicationReconciler) dropPublication(ctx context.Context, obj *apiv1.Publication) error {
	db, err := r.instance.ConnectionPool().Connection(obj.Spec.DBName)
	if err != nil {
		return fmt.Errorf("while getting DB connection: %w", err)
	}

	if _, err := db.ExecContext(
		ctx,
		fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", pgx.Identifier{obj.Spec.Name}.Sanitize()),
	); err != nil {
		return fmt.Errorf("while dropping publication: %w", err)
	}

	return nil
}

func toPublicationTargetSQL(obj *apiv1.PublicationTarget) string {
	if obj.AllTables != nil {
		return "FOR ALL TABLES"
	}

	result := ""
	for _, object := range obj.Objects {
		if len(result) > 0 {
			result += ", "
		}
		result += toPublicationObjectSQL(&object)
	}

	if len(result) > 0 {
		result = fmt.Sprintf("FOR %s", result)
	}
	return result
}

func toPublicationObjectSQL(obj *apiv1.PublicationTargetObject) string {
	if len(obj.Schema) > 0 {
		return fmt.Sprintf("TABLES IN SCHEMA %s", pgx.Identifier{obj.Schema}.Sanitize())
	}

	return fmt.Sprintf("TABLE %s", strings.Join(obj.TableExpression, ", "))
}
