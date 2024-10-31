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
	"errors"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/roles"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// RoleReconciler reconciles a Role object defined by apiv1.Role (rather than in spec.managed)
type RoleReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance instanceInterface
}

// type instanceInterface interface {
// 	GetSuperUserDB() (*sql.DB, error)
// 	GetClusterName() string
// 	GetPodName() string
// 	GetNamespaceName() string
// }

// errClusterIsReplica is raised when the role object
// cannot be reconciled because it belongs to a replica cluster
// var errClusterIsReplica = fmt.Errorf("waiting for the cluster to become primary")

// roleReconciliationInterval is the time between the
// role reconciliation loop failures
const roleReconciliationInterval = 30 * time.Second

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=roles/status,verbs=get;update;patch

// Reconcile is the role reconciliation loop
func (r *RoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	contextLogger.Debug("Reconciliation loop start")
	defer func() {
		contextLogger.Debug("Reconciliation loop end")
	}()

	// Get the role object
	var role apiv1.Role
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &role); err != nil {
		// This is a deleted object, there's nothing
		// to do since we don't manage any finalizers.
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// This is not for me!
	if role.Spec.ClusterRef.Name != r.instance.GetClusterName() {
		return ctrl.Result{}, nil
	}

	// If everything is reconciled, we're done here
	if role.Generation == role.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The cluster has been deleted.
			// We just need to wait for this instance manager to be terminated
			contextLogger.Debug("Could not find Cluster")
			return ctrl.Result{}, fmt.Errorf("could not find Cluster: %w", err)
		}

		return ctrl.Result{}, fmt.Errorf("could not fetch Cluster: %w", err)
	}

	// This is not for me, at least now
	if cluster.Status.CurrentPrimary != r.instance.GetPodName() {
		return ctrl.Result{RequeueAfter: roleReconciliationInterval}, nil
	}

	// Still not for me, we're waiting for a switchover
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return ctrl.Result{RequeueAfter: roleReconciliationInterval}, nil
	}

	// Cannot do anything on a replica cluster
	if cluster.IsReplica() {
		return r.failedReconciliation(
			ctx,
			&role,
			errClusterIsReplica,
		)
	}

	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return ctrl.Result{},
			fmt.Errorf("while connecting to the database to reconcile role %v: %w", role, err)
	}

	// Add the finalizer if we don't have it
	// nolint:nestif
	if role.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(&role, utils.RoleFinalizerName) {
			if err := r.Update(ctx, &role); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// This role is being deleted
		if controllerutil.ContainsFinalizer(&role, utils.RoleFinalizerName) {
			if role.Spec.ReclaimPolicy == apiv1.RoleReclaimDelete {
				dbRole := roleAdapter{
					RoleSpec: role.Spec,
				}.toDatabaseRole()
				if err := roles.Delete(ctx, db, dbRole); err != nil {
					return ctrl.Result{}, err
				}
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(&role, utils.RoleFinalizerName)
			if err := r.Update(ctx, &role); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// TODO: the logic to ensure only one manager can be done after the POC
	// Make sure the target PG Role is not being managed by another Role Object
	// if err := r.ensureOnlyOneManager(ctx, role); err != nil {
	// 	return r.failedReconciliation(
	// 		ctx,
	// 		&role,
	// 		err,
	// 	)
	// }

	passVersion, err := r.reconcileRole(
		ctx,
		&role,
	)
	if err != nil {
		return r.failedReconciliation(ctx, &role, err)
	}

	return r.succeededReconciliation(ctx, &role, passVersion)
}

// failedReconciliation marks the reconciliation as failed and logs the corresponding error
func (r *RoleReconciler) failedReconciliation(
	ctx context.Context,
	role *apiv1.Role,
	err error,
) (ctrl.Result, error) {
	oldRole := role.DeepCopy()
	role.Status.Message = err.Error()
	role.Status.Applied = ptr.To(false)

	var statusError *instance.StatusError
	if errors.As(err, &statusError) {
		// The body line of the instance manager contains the human
		// readable error
		role.Status.Message = statusError.Body
	}

	if err := r.Client.Status().Patch(ctx, role, client.MergeFrom(oldRole)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: roleReconciliationInterval,
	}, nil
}

// succeededReconciliation marks the reconciliation as succeeded
func (r *RoleReconciler) succeededReconciliation(
	ctx context.Context,
	role *apiv1.Role,
	passVersion string,
) (ctrl.Result, error) {
	oldRole := role.DeepCopy()
	role.Status.Message = ""
	role.Status.Applied = ptr.To(true)
	role.Status.ObservedGeneration = role.Generation
	role.Status.PasswordState.SecretResourceVersion = passVersion

	if err := r.Client.Status().Patch(ctx, role, client.MergeFrom(oldRole)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: roleReconciliationInterval,
	}, nil
}

// NewRoleReconciler creates a new role reconciler
func NewRoleReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *RoleReconciler {
	return &RoleReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *RoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Role{}).
		Named("instance-role-reconciler").
		Complete(r)
}

// GetCluster gets the managed cluster through the client
func (r *RoleReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
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

func (r *RoleReconciler) reconcileRole(ctx context.Context, role *apiv1.Role) (string, error) {
	contextLogger := log.FromContext(ctx)
	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		contextLogger.Error(err, "while connecting to postgres", "role", role)
		return "", fmt.Errorf("while connecting to the database to reconcile role %q: %w", role.Spec.Name, err)
	}

	rolesInDB, err := roles.List(ctx, db)
	if err != nil {
		return "", fmt.Errorf("while listing roles in postgres: %w", err)
	}

	dbRole := roleAdapter{
		RoleSpec: role.Spec,
	}.toDatabaseRole()
	passwordVersion, err := dbRole.ApplyPassword(ctx, r.Client, &role.Spec, r.instance.GetNamespaceName())
	if err != nil {
		return "", fmt.Errorf("while getting the role password: %w", err)
	}
	for _, r := range rolesInDB {
		if r.Name == role.Spec.Name {
			toGrant, toRevoke, err := roles.GetRoleMembershipDiff(ctx, db, role.Spec.InRoles, dbRole)
			if err != nil {
				return "", fmt.Errorf("while getting the membership updates required: %w", err)
			}
			err = roles.UpdateMembership(ctx, db, dbRole, toGrant, toRevoke)
			if err != nil {
				return "", fmt.Errorf("while getting the membership updates required: %w", err)
			}
			return passwordVersion, roles.Update(ctx, db, dbRole)
		}
	}

	return passwordVersion, roles.Create(ctx, db, dbRole)
}
