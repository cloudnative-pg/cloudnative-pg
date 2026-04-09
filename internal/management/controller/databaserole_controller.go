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
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
	pgpostgres "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// DatabaseRoleReconciler reconciles a Role object defined by apiv1.Role (rather than in spec.managed)
type DatabaseRoleReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance instanceInterface
}

// errClusterIsManagingRole is raised when a certain PostgreSQL role
// is already managed by the cluster in the cluster.spec.managed.roles section
var errClusterIsManagingRole = fmt.Errorf("database role is already managed by the CNPG cluster")

// databaseRoleReconciliationInterval is the time between the
// role reconciliation loop failures
const databaseRoleReconciliationInterval = 30 * time.Second

// Reconcile is the role reconciliation loop
func (r *DatabaseRoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	contextLogger.Debug("Reconciliation loop start")
	defer func() {
		contextLogger.Debug("Reconciliation loop end")
	}()

	// Get the role object
	var role apiv1.DatabaseRole
	if err := r.Get(ctx, client.ObjectKey{
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

	if result, err := r.shouldReconcile(ctx, &role); result != nil || err != nil {
		if result == nil {
			return ctrl.Result{}, err
		}
		return *result, err
	}

	// Add the finalizer if we don't have it
	if role.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(&role, utils.RoleFinalizerName) {
			if err := r.Update(ctx, &role); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		return r.handleDeletion(ctx, &role)
	}

	// ensure: absent is not supported for DatabaseRole CRDs. Users should
	// delete the K8s object with roleReclaimPolicy: delete instead.
	if role.Spec.Ensure == apiv1.EnsureAbsent {
		return r.failedReconciliation(ctx, &role, fmt.Errorf(
			"ensure: absent is not supported for DatabaseRole;"+
				" delete the resource with roleReclaimPolicy: delete instead"))
	}

	if res, err := detectConflictingManagers(ctx, r.Client, &role, &apiv1.DatabaseRoleList{}); err != nil ||
		!res.IsZero() {
		return res, err
	}

	if res, err := r.detectMissingPasswordSecret(ctx, &role); !res.IsZero() || err != nil {
		return res, err
	}

	passVersion, err := r.reconcileRole(
		ctx,
		&role,
	)
	if err != nil {
		return r.failedReconciliation(ctx, &role, err)
	}

	return r.succeededReconciliation(ctx, &role, passVersion)
}

// handleDeletion processes finalizer removal and optional role deletion
// when the DatabaseRole is being deleted. Only the ReclaimDelete policy
// requires a database connection; ReclaimRetain just removes the finalizer.
func (r *DatabaseRoleReconciler) handleDeletion(
	ctx context.Context,
	role *apiv1.DatabaseRole,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(role, utils.RoleFinalizerName) {
		return ctrl.Result{}, nil
	}

	if role.Spec.ReclaimPolicy == apiv1.DatabaseRoleReclaimDelete {
		db, err := r.instance.GetSuperUserDB()
		if err != nil {
			return r.failedReconciliation(ctx, role, fmt.Errorf(
				"while connecting to the database to delete role %q: %w",
				role.Spec.Name, err))
		}
		dbRole := roleAdapter{
			RoleConfiguration: role.Spec.RoleConfiguration,
		}.toDatabaseRole()
		if err := roles.Delete(ctx, db, dbRole); err != nil {
			return r.failedReconciliation(ctx, role, err)
		}
	}

	controllerutil.RemoveFinalizer(role, utils.RoleFinalizerName)
	if err := r.Update(ctx, role); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DatabaseRoleReconciler) detectMissingPasswordSecret(
	ctx context.Context,
	role *apiv1.DatabaseRole,
) (ctrl.Result, error) {
	// No password secret is configured, we can continue the reconciliation loop
	if role.Spec.GetRoleSecretName() == "" {
		return ctrl.Result{}, nil
	}

	secretObjectKey := types.NamespacedName{
		Namespace: role.Namespace,
		Name:      role.Spec.GetRoleSecretName(),
	}
	var secret corev1.Secret
	if err := r.Get(ctx, secretObjectKey, &secret); err != nil {
		return r.failedReconciliation(ctx, role, err)
	}

	return ctrl.Result{}, nil
}

// shouldReconcile checks if the role should be reconciled by this instance.
// Returns nil, nil if reconciliation should proceed.
// Returns a non-nil result or error if reconciliation should stop.
func (r *DatabaseRoleReconciler) shouldReconcile(ctx context.Context, role *apiv1.DatabaseRole) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	// This is not for me!
	if role.Spec.ClusterRef.Name != r.instance.GetClusterName() {
		return &ctrl.Result{}, nil
	}

	// If everything is reconciled and the password did not change, we're done here
	if r.isAlreadyReconciled(role) {
		return &ctrl.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The cluster has been deleted.
			// We just need to wait for this instance manager to be terminated
			contextLogger.Debug("Could not find Cluster")
			return nil, fmt.Errorf("could not find Cluster: %w", err)
		}
		return nil, fmt.Errorf("could not fetch Cluster: %w", err)
	}

	// This is not for me, at least now
	if cluster.Status.CurrentPrimary != r.instance.GetPodName() {
		return &ctrl.Result{RequeueAfter: databaseRoleReconciliationInterval}, nil
	}

	// Still not for me, we're waiting for a switchover
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return &ctrl.Result{RequeueAfter: databaseRoleReconciliationInterval}, nil
	}

	// If the role is already managed by the cluster, we stop the
	// reconciliation here.
	if isClusterManagingRole(cluster, role.Spec.Name) {
		result, err := r.failedReconciliation(ctx, role, errClusterIsManagingRole)
		return &result, err
	}

	// Cannot do anything on a replica cluster
	if cluster.IsReplica() {
		result, err := r.failedReconciliation(ctx, role, errClusterIsReplica)
		return &result, err
	}

	return nil, nil
}

// isAlreadyReconciled checks if the role has already been reconciled
// and the password secret has not changed
func (r *DatabaseRoleReconciler) isAlreadyReconciled(role *apiv1.DatabaseRole) bool {
	// Always reconcile if the object is being deleted, so that the
	// finalizer logic can run. Kubernetes does not bump metadata.generation
	// when setting DeletionTimestamp, so the generation check below would
	// otherwise short-circuit and block finalizer processing forever.
	if !role.DeletionTimestamp.IsZero() {
		return false
	}

	latestObservedSecretPasswordResourceVersion := ""
	if latestSecretChange := meta.FindStatusCondition(
		role.Status.Conditions,
		string(apiv1.ConditionPasswordSecretChange),
	); latestSecretChange != nil {
		latestObservedSecretPasswordResourceVersion = latestSecretChange.Message
	}

	return role.Generation == role.Status.ObservedGeneration &&
		role.Status.PasswordState.SecretResourceVersion == latestObservedSecretPasswordResourceVersion
}

// failedReconciliation marks the reconciliation as failed and logs the corresponding error
func (r *DatabaseRoleReconciler) failedReconciliation(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	err error,
) (ctrl.Result, error) {
	oldRole := role.DeepCopy()
	role.Status.Message = err.Error()
	role.Status.Applied = ptr.To(false)

	role.Status.Message = err.Error()

	if err := r.Client.Status().Patch(ctx, role, client.MergeFrom(oldRole)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: databaseRoleReconciliationInterval,
	}, nil
}

// succeededReconciliation marks the reconciliation as succeeded
func (r *DatabaseRoleReconciler) succeededReconciliation(
	ctx context.Context,
	role *apiv1.DatabaseRole,
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
		RequeueAfter: databaseRoleReconciliationInterval,
	}, nil
}

// NewDatabaseRoleReconciler creates a new role reconciler
func NewDatabaseRoleReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *DatabaseRoleReconciler {
	return &DatabaseRoleReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseRoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.DatabaseRole{}).
		Named("instance-role-reconciler").
		Complete(r)
}

// GetCluster gets the managed cluster through the client
func (r *DatabaseRoleReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := r.Get(ctx,
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

// isClusterManagingRole checks if the given role is already managed by the
// cluster in the cluster.spec.managed.roles section
func isClusterManagingRole(cluster *apiv1.Cluster, roleName string) bool {
	if cluster.Spec.Managed == nil {
		return false
	}

	for _, role := range cluster.Spec.Managed.Roles {
		if role.Name == roleName {
			return true
		}
	}

	return false
}

func (r *DatabaseRoleReconciler) reconcileRole(ctx context.Context, role *apiv1.DatabaseRole) (string, error) {
	contextLogger := log.FromContext(ctx)

	// Guard against reserved roles (belt-and-suspenders with CEL validation on the CRD)
	if pgpostgres.IsRoleReserved(role.Spec.Name) {
		return "", fmt.Errorf("role name %q is reserved and cannot be managed via DatabaseRole", role.Spec.Name)
	}

	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		contextLogger.Error(err, "while connecting to postgres", "role", role)
		return "", fmt.Errorf("while connecting to the database to reconcile role %q: %w", role.Spec.Name, err)
	}

	rolesInDB, err := roles.List(ctx, db)
	if err != nil {
		return "", fmt.Errorf("while listing roles in postgres: %w", err)
	}

	// Check if the role already exists in the database to determine the
	// correct validUntilNullIsInfinity setting
	var existingDBRole *roles.DatabaseRole
	for i := range rolesInDB {
		if rolesInDB[i].Name == role.Spec.Name {
			existingDBRole = &rolesInDB[i]
			break
		}
	}

	adapter := roleAdapter{
		RoleConfiguration: role.Spec.RoleConfiguration,
	}
	// When updating an existing role that has a non-null ValidUntil in the
	// database, a nil ValidUntil in the spec should translate to
	// VALID UNTIL 'infinity' (PostgreSQL cannot restore a NULL ValidUntil).
	if existingDBRole != nil {
		adapter.validUntilNullIsInfinity = existingDBRole.ValidUntil.Valid
	}
	dbRole := adapter.toDatabaseRole()

	passwordVersion, err := dbRole.ApplyPassword(
		ctx, r.Client, &role.Spec, r.instance.GetNamespaceName(),
	)
	if err != nil {
		return "", fmt.Errorf("while getting the role password: %w", err)
	}

	if existingDBRole != nil {
		toGrant, toRevoke, err := roles.GetRoleMembershipDiff(
			ctx, db, role.Spec.InRoles, dbRole,
		)
		if err != nil {
			return "", fmt.Errorf("while getting the membership updates required: %w", err)
		}
		if err = roles.UpdateMembership(ctx, db, dbRole, toGrant, toRevoke); err != nil {
			return "", fmt.Errorf("while updating membership: %w", err)
		}
		return passwordVersion, roles.Update(ctx, db, dbRole)
	}

	return passwordVersion, roles.Create(ctx, db, dbRole)
}
