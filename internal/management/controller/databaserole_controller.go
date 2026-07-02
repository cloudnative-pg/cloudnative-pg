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
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/roles"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	pgpostgres "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// DatabaseRoleReconciler reconciles a DatabaseRole object
type DatabaseRoleReconciler struct {
	client.Client

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
		// This is a deleted object that has already been finalized.
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Roles for other clusters are handled by their own instance managers.
	if role.Spec.ClusterRef.Name != r.instance.GetClusterName() {
		return ctrl.Result{}, nil
	}

	// Fetch the Cluster once; shared by shouldReconcile and handleDeletion.
	cluster, err := getClusterFromInstance(ctx, r.Client, r.instance)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("could not fetch Cluster: %w", err)
		}
		// Cluster gone: nothing to reconcile, and no finalizer to release here.
		// The operator strips DatabaseRole finalizers during cluster deletion,
		// before the Cluster object disappears (see notifyOwnedResourceDeletion).
		contextLogger.Debug("Could not find Cluster")
		return ctrl.Result{}, nil
	}

	if result, err := r.shouldReconcile(ctx, &role, cluster); result != nil || err != nil {
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
		return r.handleDeletion(ctx, &role, cluster)
	}

	// ensure: absent is not supported for DatabaseRole CRDs. Users should
	// delete the K8s object with databaseRoleReclaimPolicy: delete instead.
	if role.Spec.Ensure == apiv1.EnsureAbsent {
		return r.failedReconciliation(ctx, &role, fmt.Errorf(
			"ensure: absent is not supported for DatabaseRole;"+
				" delete the resource with databaseRoleReclaimPolicy: delete instead"))
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

// handleDeletion drops the role when this cluster owns it (see shouldDropRole)
// and then releases the finalizer.
func (r *DatabaseRoleReconciler) handleDeletion(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(role, utils.RoleFinalizerName) {
		return ctrl.Result{}, nil
	}

	dropRole := shouldDropRole(role, cluster)
	if dropRole {
		db, err := r.instance.GetSuperUserDB()
		if err != nil {
			return r.failedReconciliation(ctx, role, fmt.Errorf(
				"while connecting to the database to delete role %q: %w",
				role.Spec.Name, err))
		}
		dbRole := roles.DatabaseRoleFromConfiguration(role.Spec.RoleConfiguration, false)
		if err := roles.Delete(ctx, db, dbRole); err != nil {
			return r.failedReconciliation(ctx, role, err)
		}
	} else if role.Spec.ReclaimPolicy == apiv1.DatabaseRoleReclaimDelete {
		log.FromContext(ctx).Info(
			"not dropping role on deletion: not owned by this cluster "+
				"(managed inline, replica cluster, or never reconciled by this "+
				"object); releasing finalizer only",
			"role", role.Spec.Name)
	}

	controllerutil.RemoveFinalizer(role, utils.RoleFinalizerName)
	if err := r.Update(ctx, role); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// shouldDropRole reports whether a deleted DatabaseRole's role must be dropped:
// only when reclaimPolicy is delete, this object reconciled the role at least
// once (a conflicting duplicate never does, and must not drop the role owned by
// the surviving DatabaseRole), and this cluster owns it (not shadowed by an
// inline managed.roles entry, nor on a replica cluster).
func shouldDropRole(role *apiv1.DatabaseRole, cluster *apiv1.Cluster) bool {
	return role.Spec.ReclaimPolicy == apiv1.DatabaseRoleReclaimDelete &&
		role.HasReconciliations() &&
		!isClusterManagingRole(cluster, role.Spec.Name) && !cluster.IsReplica()
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
func (r *DatabaseRoleReconciler) shouldReconcile(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	cluster *apiv1.Cluster,
) (*ctrl.Result, error) {
	// If everything is reconciled and the password did not change, we're done
	// here, unless the cluster changed underneath the applied role.
	if r.isAlreadyReconciled(role) {
		return r.reconcileAppliedRole(ctx, role, cluster)
	}

	// This is not for me, at least now
	if cluster.Status.CurrentPrimary != r.instance.GetPodName() {
		return &ctrl.Result{RequeueAfter: databaseRoleReconciliationInterval}, nil
	}

	// Still not for me, we're waiting for a switchover
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return &ctrl.Result{RequeueAfter: databaseRoleReconciliationInterval}, nil
	}

	// The remaining gates only constrain the apply path; skip them while
	// deleting, or a role shadowed by inline managed.roles, or on a replica
	// cluster, would never reach handleDeletion and stay stuck in Terminating.
	if !role.DeletionTimestamp.IsZero() {
		return nil, nil
	}

	// If the role is already managed by the cluster, we stop the
	// reconciliation here.
	if isClusterManagingRole(cluster, role.Spec.Name) {
		result, err := r.failedReconciliation(ctx, role, errClusterIsManagingRole)
		return &result, err
	}

	// On a replica cluster the role is owned by the primary cluster, not here.
	// Report "unknown" (Applied=nil) like the sibling reconcilers, not a failure.
	if cluster.IsReplica() {
		result, err := r.unknownReconciliation(ctx, role, errClusterIsReplica)
		return &result, err
	}

	return nil, nil
}

// isAlreadyReconciled checks if the role has already been reconciled
// and the password secret has not changed
func (r *DatabaseRoleReconciler) isAlreadyReconciled(role *apiv1.DatabaseRole) bool {
	// If no password secret is configured, the condition comparison is
	// irrelevant — a stale condition from a previously-configured secret
	// must not cause a perpetual reconciliation loop.
	if role.Spec.GetRoleSecretName() == "" {
		return role.Generation == role.Status.ObservedGeneration
	}

	latestObservedSecretPasswordResourceVersion := ""
	if latestSecretChange := meta.FindStatusCondition(
		role.Status.Conditions,
		string(apiv1.ConditionPasswordSecretChange),
	); latestSecretChange != nil {
		latestObservedSecretPasswordResourceVersion = latestSecretChange.Message
	}

	return role.Generation == role.Status.ObservedGeneration &&
		role.Status.SecretResourceVersion == latestObservedSecretPasswordResourceVersion
}

// The status helpers below intentionally bypass the shared markAsReady /
// markAsFailed / markAsUnknown functions: those replace the whole status with
// Status().Update, but a DatabaseRole has two status writers. The operator
// maintains the PasswordSecretChange condition while the instance manager owns
// the other fields, so each side must merge-patch only the fields it touches
// or it would clobber the other writer's update.

// failedReconciliation marks the reconciliation as failed and logs the corresponding error
func (r *DatabaseRoleReconciler) failedReconciliation(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	err error,
) (ctrl.Result, error) {
	oldRole := role.DeepCopy()
	role.SetAsFailed(err)

	if patchErr := r.Client.Status().Patch(ctx, role, client.MergeFrom(oldRole)); patchErr != nil {
		return ctrl.Result{}, fmt.Errorf(
			"while setting the failed status: %w, original error: %w", patchErr, err)
	}

	return ctrl.Result{
		RequeueAfter: databaseRoleReconciliationInterval,
	}, nil
}

// unknownReconciliation marks the role's applied state as unknown (Applied=nil)
// because this instance is not the one managing it right now (for example on a
// read-only replica cluster, where the primary cluster owns the role). It mirrors
// the sibling Database/Publication/Subscription reconcilers, which report Unknown
// rather than a failure in this case.
func (r *DatabaseRoleReconciler) unknownReconciliation(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	err error,
) (ctrl.Result, error) {
	oldRole := role.DeepCopy()
	role.SetAsUnknown(err)

	if patchErr := r.Client.Status().Patch(ctx, role, client.MergeFrom(oldRole)); patchErr != nil {
		return ctrl.Result{}, fmt.Errorf(
			"while setting the unknown status: %w, original error: %w", patchErr, err)
	}

	return ctrl.Result{
		RequeueAfter: databaseRoleReconciliationInterval,
	}, nil
}

// reconcileAppliedRole re-evaluates the status of an already-reconciled role
// when its cluster moved in or out of the replica role, or an inline
// managed.roles entry took it over. The recorded reconciliation is always
// kept, so the object retains ownership of the PostgreSQL role it manages and
// a conflicting DatabaseRole cannot take over while the role is reported as not
// applied. A nil result asks the caller to re-apply the role through the
// regular flow.
func (r *DatabaseRoleReconciler) reconcileAppliedRole(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	cluster *apiv1.Cluster,
) (*ctrl.Result, error) {
	// Deletion is handled by the caller.
	if !role.DeletionTimestamp.IsZero() {
		return &ctrl.Result{}, nil
	}

	isDesignatedPrimary := cluster.Status.CurrentPrimary == r.instance.GetPodName()
	applied := role.Status.Applied

	// An inline managed.roles entry took over the role after it was applied:
	// the designated primary reports the conflict. Re-applying (adopting the
	// role back) once the inline entry is removed is handled by the re-apply
	// path below.
	if isDesignatedPrimary && isClusterManagingRole(cluster, role.Spec.Name) {
		result, err := r.failedReconciliation(ctx, role, errClusterIsManagingRole)
		return &result, err
	}

	if cluster.IsReplica() {
		// The cluster was demoted to a replica after the apply: the role is
		// owned by the primary cluster. The designated primary reports the
		// replica condition (Applied=nil).
		if isDesignatedPrimary && applied != nil {
			result, err := r.unknownReconciliation(ctx, role, errClusterIsReplica)
			return &result, err
		}
		// Keep polling while the designated primary awaits promotion, or while
		// the cluster status is still settling (for example a failover
		// concurrent with the demotion): status-only updates don't retrigger
		// the cluster watch.
		if isDesignatedPrimary || applied != nil {
			return &ctrl.Result{RequeueAfter: databaseRoleReconciliationInterval}, nil
		}
		return &ctrl.Result{}, nil
	}

	// Primary and not shadowed: if a previous transition (the replica
	// condition or an inline takeover) left the role not applied, re-apply it
	// through the regular flow.
	if isDesignatedPrimary && (applied == nil || !*applied) {
		return nil, nil
	}
	return &ctrl.Result{}, nil
}

// succeededReconciliation marks the reconciliation as succeeded
func (r *DatabaseRoleReconciler) succeededReconciliation(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	passVersion string,
) (ctrl.Result, error) {
	oldRole := role.DeepCopy()
	role.SetAsReady()
	role.Status.SecretResourceVersion = passVersion

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
		Watches(
			&apiv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToDatabaseRoles),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Named("instance-database-role").
		Complete(r)
}

// mapClusterToDatabaseRoles enqueues every DatabaseRole targeting this
// instance's cluster when the cluster spec changes (e.g. an inline
// managed.roles entry is added or removed).
func (r *DatabaseRoleReconciler) mapClusterToDatabaseRoles(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	if obj.GetName() != r.instance.GetClusterName() ||
		obj.GetNamespace() != r.instance.GetNamespaceName() {
		return nil
	}

	var roles apiv1.DatabaseRoleList
	if err := r.List(ctx, &roles, client.InNamespace(obj.GetNamespace())); err != nil {
		log.FromContext(ctx).Error(err, "while listing DatabaseRoles to react to a cluster change")
		return nil
	}

	requests := make([]reconcile.Request, 0, len(roles.Items))
	for i := range roles.Items {
		if roles.Items[i].Spec.ClusterRef.Name != obj.GetName() {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&roles.Items[i]),
		})
	}

	return requests
}

// isClusterManagingRole checks if the given role is already managed by the
// cluster in the cluster.spec.managed.roles section
func isClusterManagingRole(cluster *apiv1.Cluster, roleName string) bool {
	if cluster.Spec.Managed == nil {
		return false
	}

	for i := range cluster.Spec.Managed.Roles {
		if cluster.Spec.Managed.Roles[i].Name == roleName {
			return true
		}
	}

	return false
}

// updateExistingRole applies membership changes, attribute updates, and
// comment updates to an existing PostgreSQL role.
func updateExistingRole(
	ctx context.Context,
	db *sql.DB,
	dbRole roles.DatabaseRole,
	existingDBRole *roles.DatabaseRole,
) error {
	toGrant, toRevoke, err := roles.GetRoleMembershipDiff(
		ctx, db, dbRole.InRoles, dbRole,
	)
	if err != nil {
		return fmt.Errorf("while getting the membership updates required: %w", err)
	}
	if err = roles.UpdateMembership(ctx, db, dbRole, toGrant, toRevoke); err != nil {
		return fmt.Errorf("while updating membership: %w", err)
	}
	if err = roles.Update(ctx, db, dbRole); err != nil {
		return err
	}
	if existingDBRole.Comment != dbRole.Comment {
		if err = roles.UpdateComment(ctx, db, dbRole); err != nil {
			return fmt.Errorf("while updating comment: %w", err)
		}
	}
	return nil
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

	// When updating an existing role that has a non-null ValidUntil in the
	// database, a nil ValidUntil in the spec should translate to
	// VALID UNTIL 'infinity' (PostgreSQL cannot restore a NULL ValidUntil).
	validUntilNullIsInfinity := existingDBRole != nil && existingDBRole.ValidUntil.Valid
	dbRole := roles.DatabaseRoleFromConfiguration(role.Spec.RoleConfiguration, validUntilNullIsInfinity)

	passwordVersion, err := dbRole.ApplyPassword(
		ctx, r.Client, &role.Spec.RoleConfiguration, r.instance.GetNamespaceName(),
	)
	if err != nil {
		return "", fmt.Errorf("while getting the role password: %w", err)
	}

	if existingDBRole != nil {
		if err := updateExistingRole(ctx, db, dbRole, existingDBRole); err != nil {
			return "", err
		}
	} else if err := roles.Create(ctx, db, dbRole); err != nil {
		return "", err
	}

	return passwordVersion, nil
}
