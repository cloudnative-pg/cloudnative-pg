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
	"github.com/lib/pq"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// SubscriptionReconciler reconciles a Subscription object
type SubscriptionReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance *postgres.Instance
}

// subscriptionReconciliationInterval is the time between the
// subscription reconciliation loop failures
const subscriptionReconciliationInterval = 30 * time.Second

// subscriptionFinalizerName is the name of the finalizer
// triggering the deletion of the subscription
const subscriptionFinalizerName = utils.MetadataNamespace + "/deleteSubscription"

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=subscriptions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=subscriptions/status,verbs=get;update;patch

// Reconcile is the subscription reconciliation loop
func (r *SubscriptionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug("Reconciliation loop start")
	defer func() {
		contextLogger.Debug("Reconciliation loop end")
	}()

	// Get the subscription object
	var subscription apiv1.Subscription
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &subscription); err != nil {
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
	if subscription.Spec.ClusterRef.Name != r.instance.ClusterName {
		return ctrl.Result{}, nil
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
			&subscription,
			errClusterIsReplica,
		)
	}

	// Add the finalizer if we don't have it
	// nolint:nestif
	if subscription.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(&subscription, subscriptionFinalizerName) {
			if err := r.Update(ctx, &subscription); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// This subscription is being deleted
		if controllerutil.ContainsFinalizer(&subscription, subscriptionFinalizerName) {
			if err := r.dropSubscription(ctx, &subscription); err != nil {
				return ctrl.Result{}, err
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(&subscription, subscriptionFinalizerName)
			if err := r.Update(ctx, &subscription); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}
	}

	// If everything is reconciled, we're done here
	if subscription.Generation == subscription.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	// Let's get the connection string
	connString, err := getSubscriptionConnectionString(
		cluster,
		subscription.Spec.ExternalClusterName,
		"", // TODO: should we have a way to force dbname?
	)
	if err != nil {
		return r.failedReconciliation(
			ctx,
			&subscription,
			err,
		)
	}

	if err := r.alignSubscription(
		ctx,
		&subscription,
		connString,
	); err != nil {
		return r.failedReconciliation(
			ctx,
			&subscription,
			err,
		)
	}

	return r.succeededReconciliation(
		ctx,
		&subscription,
	)
}

// NewSubscriptionReconciler creates a new subscription reconciler
func NewSubscriptionReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *SubscriptionReconciler {
	return &SubscriptionReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *SubscriptionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Subscription{}).
		Complete(r)
}

// failedReconciliation marks the reconciliation as failed and logs the corresponding error
func (r *SubscriptionReconciler) failedReconciliation(
	ctx context.Context,
	subscription *apiv1.Subscription,
	err error,
) (ctrl.Result, error) {
	oldSubscription := subscription.DeepCopy()
	subscription.Status.Error = err.Error()
	subscription.Status.Ready = false

	var statusError *instance.StatusError
	if errors.As(err, &statusError) {
		// The body line of the instance manager contain the human
		// readable error
		subscription.Status.Error = statusError.Body
	}

	if err := r.Client.Status().Patch(ctx, subscription, client.MergeFrom(oldSubscription)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: databaseReconciliationInterval,
	}, nil
}

// succeededReconciliation marks the reconciliation as succeeded
func (r *SubscriptionReconciler) succeededReconciliation(
	ctx context.Context,
	subscription *apiv1.Subscription,
) (ctrl.Result, error) {
	oldSubscription := subscription.DeepCopy()
	subscription.Status.Error = ""
	subscription.Status.Ready = true
	subscription.Status.ObservedGeneration = subscription.Generation

	if err := r.Client.Status().Patch(ctx, subscription, client.MergeFrom(oldSubscription)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: subscriptionReconciliationInterval,
	}, nil
}

// GetCluster gets the managed cluster through the client
func (r *SubscriptionReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
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

func (r *SubscriptionReconciler) alignSubscription(
	ctx context.Context,
	obj *apiv1.Subscription,
	connString string,
) error {
	db, err := r.instance.ConnectionPool().Connection(obj.Spec.DBName)
	if err != nil {
		return fmt.Errorf("while getting DB connection: %w", err)
	}

	row := db.QueryRowContext(
		ctx,
		`
		SELECT count(*)
		FROM pg_subscription
	    WHERE subname = $1
		`,
		obj.Spec.Name)
	if row.Err() != nil {
		return fmt.Errorf("while getting subscription status: %w", row.Err())
	}

	var count int
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("while getting subscription status (scan): %w", err)
	}

	if count > 0 {
		if err := r.patchSubscription(ctx, db, obj, connString); err != nil {
			return fmt.Errorf("while patching subscription: %w", err)
		}
		return nil
	}

	if err := r.createSubscription(ctx, db, obj, connString); err != nil {
		return fmt.Errorf("while creating subscription: %w", err)
	}

	return nil
}

func (r *SubscriptionReconciler) patchSubscription(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Subscription,
	connString string,
) error {
	sqls := toSubscriptionAlterSQL(obj, connString)
	for _, sqlQuery := range sqls {
		if _, err := db.ExecContext(ctx, sqlQuery); err != nil {
			return err
		}
	}

	return nil
}

func (r *SubscriptionReconciler) createSubscription(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Subscription,
	connString string,
) error {
	sqls := toSubscriptionCreateSQL(obj, connString)
	for _, sqlQuery := range sqls {
		if _, err := db.ExecContext(ctx, sqlQuery); err != nil {
			return err
		}
	}

	return nil
}

func toSubscriptionCreateSQL(obj *apiv1.Subscription, connString string) []string {
	result := make([]string, 0, 2)

	createQuery := fmt.Sprintf(
		"CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s",
		pgx.Identifier{obj.Spec.Name}.Sanitize(),
		pq.QuoteLiteral(connString),
		pgx.Identifier{obj.Spec.PublicationName}.Sanitize(),
	)
	if len(obj.Spec.Parameters) > 0 {
		createQuery = fmt.Sprintf("%s WITH (%s)", createQuery, obj.Spec.Parameters)
	}
	result = append(result, createQuery)

	if len(obj.Spec.Owner) > 0 {
		result = append(result,
			fmt.Sprintf(
				"ALTER SUBSCRIPTION %s OWNER TO %s",
				pgx.Identifier{obj.Spec.Name}.Sanitize(),
				pgx.Identifier{obj.Spec.Owner}.Sanitize(),
			),
		)
	}

	return result
}

func toSubscriptionAlterSQL(obj *apiv1.Subscription, connString string) []string {
	result := make([]string, 0, 4)

	setPublicationSQL := fmt.Sprintf(
		"ALTER SUBSCRIPTION %s SET PUBLICATION %s",
		pgx.Identifier{obj.Spec.Name}.Sanitize(),
		pgx.Identifier{obj.Spec.PublicationName}.Sanitize(),
	)

	setConnStringSQL := fmt.Sprintf(
		"ALTER SUBSCRIPTION %s SET CONNECTION %s",
		pgx.Identifier{obj.Spec.Name}.Sanitize(),
		pq.QuoteLiteral(connString),
	)
	result = append(result, setPublicationSQL, setConnStringSQL)

	if len(obj.Spec.Owner) > 0 {
		result = append(result,
			fmt.Sprintf(
				"ALTER SUBSCRIPTION %s OWNER TO %s",
				pgx.Identifier{obj.Spec.Name}.Sanitize(),
				pgx.Identifier{obj.Spec.Owner}.Sanitize(),
			),
		)
	}

	if len(obj.Spec.Parameters) > 0 {
		result = append(result,
			fmt.Sprintf(
				"ALTER SUBSCRIPTION %s SET (%s)",
				result,
				obj.Spec.Parameters,
			),
		)
	}

	return result
}

func (r *SubscriptionReconciler) dropSubscription(ctx context.Context, obj *apiv1.Subscription) error {
	db, err := r.instance.ConnectionPool().Connection(obj.Spec.DBName)
	if err != nil {
		return fmt.Errorf("while getting DB connection: %w", err)
	}

	if _, err := db.ExecContext(
		ctx,
		fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", pgx.Identifier{obj.Spec.Name}.Sanitize()),
	); err != nil {
		return fmt.Errorf("while dropping subscription: %w", err)
	}

	return nil
}

// getSubscriptionConnectionString gets the connection string to be used to connect to
// the specified external cluster, while connected to a pod of the specified
// cluster.
func getSubscriptionConnectionString(
	cluster *apiv1.Cluster,
	externalClusterName string,
	databaseName string,
) (string, error) {
	externalCluster, ok := cluster.ExternalCluster(externalClusterName)
	if !ok {
		return "", fmt.Errorf("external cluster %s not found in the cluster %s", externalClusterName, cluster.Name)
	}

	return external.GetServerConnectionString(&externalCluster, databaseName), nil
}
