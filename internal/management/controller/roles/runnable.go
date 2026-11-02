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

package roles

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// roleAction encodes the action necessary for a role, i.e. ignore, or CRUD
type roleAction string

// possible role actions
const (
	roleIsReconciled      roleAction = "RECONCILED"
	roleCreate            roleAction = "CREATE"
	roleDelete            roleAction = "DELETE"
	roleUpdate            roleAction = "UPDATE"
	roleIgnore            roleAction = "IGNORE"
	roleIsReserved        roleAction = "RESERVED"
	roleSetComment        roleAction = "SET_COMMENT"
	roleUpdateMemberships roleAction = "UPDATE_MEMBERSHIPS"
)

type instanceInterface interface {
	GetSuperUserDB() (*sql.DB, error)
	IsPrimary() (bool, error)
	RoleSynchronizerChan() <-chan *apiv1.ManagedConfiguration
	IsReady() error
	GetClusterName() string
	GetNamespaceName() string
}

// A RoleSynchronizer is a Kubernetes manager.Runnable
// that makes sure the Roles in the PostgreSQL databases are in sync with the spec
//
// c.f. https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager#Runnable
type RoleSynchronizer struct {
	instance                instanceInterface
	client                  client.Client
	getPostgresMajorVersion func() (int, error)
}

// NewRoleSynchronizer creates a new RoleSynchronizer
func NewRoleSynchronizer(instance *postgres.Instance, client client.Client) *RoleSynchronizer {
	runner := &RoleSynchronizer{
		instance: instance,
		client:   client,
		getPostgresMajorVersion: func() (int, error) {
			version, err := instance.GetPgVersion()
			return int(version.Major()), err //nolint:gosec
		},
	}
	return runner
}

// Start starts running the RoleSynchronizer
func (sr *RoleSynchronizer) Start(ctx context.Context) error {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Info("starting up the runnable")

	go func() {
		var config *apiv1.ManagedConfiguration
		contextLog.Info("setting up RoleSynchronizer loop")

		defer func() {
			contextLog.Info("Terminated RoleSynchronizer loop")
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case config = <-sr.instance.RoleSynchronizerChan():
			}
			contextLog.Debug("RoleSynchronizer loop triggered")

			// If the spec contains no roles to manage, stop the timer,
			// the process will resume through the wakeUp channel if necessary
			if config == nil || len(config.Roles) == 0 {
				continue
			}

			// Skip replicas and designated primaries: only true primaries write roles.
			isPrimary, err := sr.instance.IsPrimary()
			if err != nil {
				contextLog.Error(err, "checking primary status")
				continue
			}
			if !isPrimary {
				contextLog.Debug("skipping role reconciliation on a non-primary instance")
				continue
			}

			err = sr.reconcile(ctx, config)
			if err != nil {
				contextLog.Error(err, "synchronizing roles", "config", config)
				continue
			}
		}
	}()
	<-ctx.Done()
	return nil
}

// reconcile applied any necessary changes to the database to bring it in line
// with the spec. It also updates the cluster Status with the latest applied changes
func (sr *RoleSynchronizer) reconcile(ctx context.Context, config *apiv1.ManagedConfiguration) error {
	var err error
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Debug("reconciling managed roles")

	if err := sr.instance.IsReady(); err != nil {
		contextLog.Debug("database not ready, skipping roles reconciling")
		return nil
	}

	var remoteCluster apiv1.Cluster
	if err = sr.client.Get(ctx, types.NamespacedName{
		Name:      sr.instance.GetClusterName(),
		Namespace: sr.instance.GetNamespaceName(),
	}, &remoteCluster); err != nil {
		return err
	}

	rolePasswords := remoteCluster.Status.ManagedRolesStatus.PasswordStatus
	if rolePasswords == nil {
		rolePasswords = map[string]apiv1.PasswordState{}
	}
	superUserDB, err := sr.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting superuser connection: %w", err)
	}
	appliedState, unreconciledRoles, err := sr.synchronizeRoles(ctx, superUserDB, config, rolePasswords)
	if err != nil {
		return fmt.Errorf("while syncrhonizing managed roles: %w", err)
	}

	if err = sr.client.Get(ctx, types.NamespacedName{
		Name:      sr.instance.GetClusterName(),
		Namespace: sr.instance.GetNamespaceName(),
	}, &remoteCluster); err != nil {
		return err
	}
	updatedCluster := remoteCluster.DeepCopy()
	updatedCluster.Status.ManagedRolesStatus.PasswordStatus = appliedState
	updatedCluster.Status.ManagedRolesStatus.CannotReconcile = unreconciledRoles
	return sr.client.Status().Patch(ctx, updatedCluster, client.MergeFrom(&remoteCluster))
}

func getRoleNames(roles []roleConfigurationAdapter) []string {
	names := make([]string, len(roles))
	for i, role := range roles {
		names[i] = role.Name
	}
	return names
}

// synchronizeRoles aligns roles in the database to the spec
// It returns
//   - the PasswordState for any updated roles
//   - the per-role errors encountered while applying role actions (e.g.
//     unrealizable postgres operations, missing password secret)
//   - any error that prevents the whole synchronization from running
//     (e.g. listing the roles in the database failed)
func (sr *RoleSynchronizer) synchronizeRoles(
	ctx context.Context,
	db *sql.DB,
	config *apiv1.ManagedConfiguration,
	storedPasswordState map[string]apiv1.PasswordState,
) (map[string]apiv1.PasswordState, map[string][]string, error) {
	latestSecretResourceVersion := getPasswordSecretResourceVersion(
		ctx, sr.client, config.Roles, sr.instance.GetNamespaceName())
	rolesInDB, err := List(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	rolesByAction := evaluateNextRoleActions(
		ctx, config, rolesInDB, storedPasswordState, latestSecretResourceVersion)

	passwordStates, unreconciledRoles := sr.applyRoleActions(ctx, db, rolesByAction)

	// Merge the status from database into spec. We should keep all the status
	// otherwise in the next loop the user without status will be marked as need update
	for role, stateInDatabase := range passwordStates {
		storedPasswordState[role] = stateInDatabase
	}
	return storedPasswordState, unreconciledRoles, nil
}

// applyRoleActions applies the actions to reconcile roles in the DB with the Spec.
// It returns the apiv1.PasswordState for each successfully applied role, and a
// map collecting the errors encountered per role. Both expectable errors
// (e.g. dropping a role that owns content) and unexpected errors are recorded
// in the map, so that an error on one role does not block the reconciliation
// of the others.
func (sr *RoleSynchronizer) applyRoleActions(
	ctx context.Context,
	db *sql.DB,
	rolesByAction rolesByAction,
) (map[string]apiv1.PasswordState, map[string][]string) {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Debug("applying role actions")

	unreconciledRoles := make(map[string][]string)
	appliedChanges := make(map[string]apiv1.PasswordState)
	handleRoleError := func(errToEvaluate error, roleName string, action roleAction) {
		if errToEvaluate == nil {
			return
		}
		roleError, err := parseRoleError(errToEvaluate, roleName, action)
		if err == nil {
			unreconciledRoles[roleName] = append(unreconciledRoles[roleName], roleError.Error())
		} else {
			unreconciledRoles[roleName] = append(unreconciledRoles[roleName], errToEvaluate.Error())
		}
	}

	actionsCreateUpdate := []roleAction{roleCreate, roleUpdate}
	for _, action := range actionsCreateUpdate {
		for _, role := range rolesByAction[action] {
			appliedState, err := sr.applyRoleCreateUpdate(ctx, db, role, action)
			if err == nil {
				appliedChanges[role.Name] = appliedState
			}
			handleRoleError(err, role.Name, action)
		}
	}

	for _, role := range rolesByAction[roleSetComment] {
		// NOTE: adding/updating a comment on a role does not alter its TransactionID
		err := UpdateComment(ctx, db, role.toDatabaseRole())
		handleRoleError(err, role.Name, roleSetComment)
	}

	for _, role := range rolesByAction[roleUpdateMemberships] {
		// NOTE: revoking / granting to a role does not alter its TransactionID
		dbRole := role.toDatabaseRole()
		pgMajorVersion, err := sr.getPostgresMajorVersion()
		if err != nil {
			handleRoleError(err, role.Name, roleUpdateMemberships)
			continue
		}
		changes, err := GetRoleMembershipDiff(ctx, db, dbRole, nil, pgMajorVersion)
		handleRoleError(err, role.Name, roleUpdateMemberships)

		err = UpdateMembership(ctx, db, dbRole, changes, pgMajorVersion)
		handleRoleError(err, role.Name, roleUpdateMemberships)
	}

	for _, role := range rolesByAction[roleDelete] {
		err := Delete(ctx, db, role.toDatabaseRole())
		handleRoleError(err, role.Name, roleDelete)
	}

	return appliedChanges, unreconciledRoles
}

// GetRoleMembershipDiff returns the changes that make the role a member of
// exactly the roles in its InRoles, with the PostgreSQL defaults, and in
// roleGrants, with the options they set
func GetRoleMembershipDiff(
	ctx context.Context,
	db *sql.DB,
	dbRole DatabaseRole,
	roleGrants []apiv1.RoleGrant,
	pgMajorVersion int,
) (MembershipChanges, error) {
	inRoleInDB, err := GetParentRoles(ctx, db, dbRole, pgMajorVersion)
	if err != nil {
		return MembershipChanges{}, err
	}
	inRoleInSpec := make([]apiv1.RoleGrant, 0, len(dbRole.InRoles)+len(roleGrants))
	for _, name := range dbRole.InRoles {
		inRoleInSpec = append(inRoleInSpec, apiv1.RoleGrant{Name: name})
	}
	inRoleInSpec = append(inRoleInSpec, roleGrants...)
	return getMembershipChanges(inRoleInDB, inRoleInSpec), nil
}

// applyRoleCreateUpdate creates/updates a role, getting the password from Kubernetes
// secrets if so set.
// Returns the PasswordState, as well as any error encountered
func (sr *RoleSynchronizer) applyRoleCreateUpdate(
	ctx context.Context,
	db *sql.DB,
	role roleConfigurationAdapter,
	action roleAction,
) (apiv1.PasswordState, error) {
	databaseRole := role.toDatabaseRole()
	passVersion, err := databaseRole.ApplyPassword(ctx, sr.client,
		role.GetRoleSecretName(), role.DisablePassword, sr.instance.GetNamespaceName())
	if err != nil {
		return apiv1.PasswordState{}, err
	}

	switch action {
	case roleCreate:
		err = Create(ctx, db, databaseRole)
	case roleUpdate:
		err = Update(ctx, db, databaseRole)
	}
	if err != nil {
		return apiv1.PasswordState{}, err
	}

	transactionID, err := GetLastTransactionID(ctx, db, databaseRole)
	if err != nil {
		return apiv1.PasswordState{}, err
	}

	return apiv1.PasswordState{
		TransactionID:         transactionID,
		SecretResourceVersion: passVersion,
	}, nil
}

// passwordSecret contains the decoded credentials from a Secret
type passwordSecret struct {
	username    string
	password    string
	version     string
	passthrough bool
}

// getPassword retrieves the password stored in the Kubernetes secret for the
// RoleConfiguration
func getPassword(
	ctx context.Context,
	cl client.Client,
	roleName string,
	secretName string,
	namespace string,
) (passwordSecret, error) {
	// no secrets defined, will keep the role password nil
	if secretName == "" {
		return passwordSecret{}, nil
	}

	var secret corev1.Secret
	err := cl.Get(ctx,
		client.ObjectKey{Namespace: namespace, Name: secretName},
		&secret)
	if err != nil {
		return passwordSecret{},
			fmt.Errorf("failed to get password secret %s: %w", secretName, err)
	}
	usernameFromSecret, passwordFromSecret, err := utils.GetUserPasswordFromSecret(&secret)
	if err != nil {
		return passwordSecret{}, err
	}
	if strings.TrimSpace(roleName) != strings.TrimSpace(usernameFromSecret) {
		return passwordSecret{},
			fmt.Errorf("the username in secret %q does not match role %q", secretName, roleName)
	}
	return passwordSecret{
			username:    strings.TrimSpace(usernameFromSecret),
			password:    strings.TrimSpace(passwordFromSecret),
			version:     secret.GetResourceVersion(),
			passthrough: cnpgutils.IsPasswordPassthroughEnabled(&secret.ObjectMeta),
		},
		nil
}

// getPasswordSecretResourceVersion returns a list of resource version of the password secrets for managed roles
// stored as Kubernetes secrets. Roles whose secret cannot be fetched are
// omitted from the result; the action evaluator will route them through the
// update path so the failure surfaces in CannotReconcile.
func getPasswordSecretResourceVersion(
	ctx context.Context,
	cl client.Client,
	rolesInSpec []apiv1.RoleConfiguration,
	namespace string,
) map[string]string {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	re := make(map[string]string)
	for _, role := range rolesInSpec {
		if role.PasswordSecret == nil || role.DisablePassword {
			continue
		}
		passwordSecret, err := getPassword(ctx, cl, role.Name, role.GetRoleSecretName(), namespace)
		if err != nil {
			contextLog.Debug("could not fetch password secret for role; will be flagged for reconciliation",
				"role", role.Name, "secret", role.PasswordSecret.Name, "err", err.Error())
			continue
		}
		re[role.Name] = passwordSecret.version
	}
	return re
}

// getMembershipChanges compares the grants of membership in the DB with the
// memberships in the spec. A membership missing in the DB is granted, without
// the admin option unless the spec turns it on. Every grant in the DB of a
// membership in the spec is changed to the options the spec sets, as any of
// them can give the member what the spec denies, and every grant of a
// membership missing in the spec is revoked, whoever granted it.
func getMembershipChanges(inRoleInDB []Membership, inRoleInSpec []apiv1.RoleGrant) MembershipChanges {
	var changes MembershipChanges
	for _, wanted := range inRoleInSpec {
		granted := false
		for _, grant := range inRoleInDB {
			if grant.Name != wanted.Name {
				continue
			}
			granted = true
			if update := optionsToChange(grant, wanted); update.hasOptions() {
				changes.Update = append(changes.Update, update)
			}
		}
		if !granted {
			newGrant := Membership{Name: wanted.Name, Inherit: wanted.Inherit, Set: wanted.Set}
			if ptr.Deref(wanted.Admin, false) {
				newGrant.Admin = wanted.Admin
			}
			changes.Grant = append(changes.Grant, newGrant)
		}
	}
	for _, grant := range inRoleInDB {
		if !slices.ContainsFunc(inRoleInSpec, func(r apiv1.RoleGrant) bool { return r.Name == grant.Name }) {
			changes.Revoke = append(changes.Revoke, grant)
		}
	}
	return changes
}

// optionsToChange returns the grant with only the options the spec sets to a
// value the grant does not have
func optionsToChange(grant Membership, wanted apiv1.RoleGrant) Membership {
	toChange := func(inDB, inSpec *bool) *bool {
		if inSpec == nil || ptr.Equal(inDB, inSpec) {
			return nil
		}
		return inSpec
	}
	return Membership{
		Name:    grant.Name,
		Grantor: grant.Grantor,
		Admin:   toChange(grant.Admin, wanted.Admin),
		Inherit: toChange(grant.Inherit, wanted.Inherit),
		Set:     toChange(grant.Set, wanted.Set),
	}
}
