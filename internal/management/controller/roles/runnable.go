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
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
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
	instance instanceInterface
	client   client.Client
}

// NewRoleSynchronizer creates a new RoleSynchronizer
func NewRoleSynchronizer(instance *postgres.Instance, client client.Client) *RoleSynchronizer {
	runner := &RoleSynchronizer{
		instance: instance,
		client:   client,
	}
	return runner
}

// Start starts running the RoleSynchronizer
func (sr *RoleSynchronizer) Start(ctx context.Context) error {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Info("starting up the runnable")
	isPrimary, err := sr.instance.IsPrimary()
	if err != nil {
		return err
	}
	if !isPrimary {
		contextLog.Info("skipping the RoleSynchronizer in replicas")
	}
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

			err := sr.reconcile(ctx, config)
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

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from a panic: %s", r)
		}
	}()

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
	appliedState, irreconcilableRoles, err := sr.synchronizeRoles(ctx, superUserDB, config, rolePasswords)
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
	updatedCluster.Status.ManagedRolesStatus.CannotReconcile = irreconcilableRoles
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
//   - any roles that had expectable postgres errors
//   - any unexpected error
func (sr *RoleSynchronizer) synchronizeRoles(
	ctx context.Context,
	db *sql.DB,
	config *apiv1.ManagedConfiguration,
	storedPasswordState map[string]apiv1.PasswordState,
) (map[string]apiv1.PasswordState, map[string][]string, error) {
	latestSecretResourceVersion, err := getPasswordSecretResourceVersion(
		ctx, sr.client, config.Roles, sr.instance.GetNamespaceName())
	if err != nil {
		return nil, nil, err
	}
	rolesInDB, err := List(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	rolesByAction := evaluateNextRoleActions(
		ctx, config, rolesInDB, storedPasswordState, latestSecretResourceVersion)

	passwordStates, irreconcilableRoles, err := sr.applyRoleActions(ctx, db, rolesByAction)
	if err != nil {
		return nil, nil, err
	}

	// Merge the status from database into spec. We should keep all the status
	// otherwise in the next loop the user without status will be marked as need update
	for role, stateInDatabase := range passwordStates {
		storedPasswordState[role] = stateInDatabase
	}
	return storedPasswordState, irreconcilableRoles, nil
}

// applyRoleActions applies the actions to reconcile roles in the DB with the Spec
// It returns the apiv1.PasswordState for each role, as well as a map of roles that
// cannot be reconciled for expectable errors, e.g. dropping a role owning content
//
// NOTE: applyRoleActions will carry on after an expectable error, i.e. an error
// due to an invalid request for postgres. This is so that other actions will not
// be blocked by a user error.
// It will, however, error out on unexpected errors.
func (sr *RoleSynchronizer) applyRoleActions(
	ctx context.Context,
	db *sql.DB,
	rolesByAction rolesByAction,
) (map[string]apiv1.PasswordState, map[string][]string, error) {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Debug("applying role actions")

	irreconcilableRoles := make(map[string][]string)
	appliedChanges := make(map[string]apiv1.PasswordState)
	handleRoleError := func(errToEvaluate error, roleName string, action roleAction) error {
		// log unexpected errors, collect expectable PostgreSQL errors
		if errToEvaluate == nil {
			return nil
		}
		roleError, err := parseRoleError(errToEvaluate, roleName, action)
		if err != nil {
			contextLog.Error(err, "while performing "+string(action), "role", roleName)
			return err
		}

		irreconcilableRoles[roleName] = append(irreconcilableRoles[roleName], roleError.Error())
		return nil
	}

	actionsCreateUpdate := []roleAction{roleCreate, roleUpdate}
	for _, action := range actionsCreateUpdate {
		for _, role := range rolesByAction[action] {
			appliedState, err := sr.applyRoleCreateUpdate(ctx, db, role, action)
			if err == nil {
				appliedChanges[role.Name] = appliedState
			}
			if unhandledErr := handleRoleError(err, role.Name, action); unhandledErr != nil {
				return nil, nil, unhandledErr
			}
		}
	}

	for _, role := range rolesByAction[roleSetComment] {
		// NOTE: adding/updating a comment on a role does not alter its TransactionID
		err := UpdateComment(ctx, db, role.toDatabaseRole())
		if unhandledErr := handleRoleError(err, role.Name, roleSetComment); unhandledErr != nil {
			return nil, nil, unhandledErr
		}
	}

	for _, role := range rolesByAction[roleUpdateMemberships] {
		// NOTE: revoking / granting to a role does not alter its TransactionID
		dbRole := role.toDatabaseRole()
		grants, revokes, err := getRoleMembershipDiff(ctx, db, role, dbRole)
		if unhandledErr := handleRoleError(err, role.Name, roleUpdateMemberships); unhandledErr != nil {
			return nil, nil, unhandledErr
		}

		err = UpdateMembership(ctx, db, dbRole, grants, revokes)
		if unhandledErr := handleRoleError(err, role.Name, roleUpdateMemberships); unhandledErr != nil {
			return nil, nil, unhandledErr
		}
	}

	for _, role := range rolesByAction[roleDelete] {
		err := Delete(ctx, db, role.toDatabaseRole())
		if unhandledErr := handleRoleError(err, role.Name, roleDelete); unhandledErr != nil {
			return nil, nil, unhandledErr
		}
	}

	return appliedChanges, irreconcilableRoles, nil
}

func getRoleMembershipDiff(
	ctx context.Context,
	db *sql.DB,
	role roleConfigurationAdapter,
	dbRole DatabaseRole,
) ([]string, []string, error) {
	inRoleInDB, err := GetParentRoles(ctx, db, dbRole)
	if err != nil {
		return nil, nil, err
	}
	rolesToGrant := getRolesToGrant(inRoleInDB, role.InRoles)
	rolesToRevoke := getRolesToRevoke(inRoleInDB, role.InRoles)
	return rolesToGrant, rolesToRevoke, nil
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
	var passVersion string
	databaseRole := role.toDatabaseRole()
	switch {
	case role.PasswordSecret == nil && !role.DisablePassword:
		databaseRole.ignorePassword = true
	case role.PasswordSecret == nil && role.DisablePassword:
		databaseRole.password = sql.NullString{}
	case role.PasswordSecret != nil && role.DisablePassword:
		// this case should be prevented by the validation webhook,
		// and is an error
		return apiv1.PasswordState{},
			fmt.Errorf("cannot reconcile: password both provided and disabled: %s",
				role.PasswordSecret.Name)
	case role.PasswordSecret != nil && !role.DisablePassword:
		passwordSecret, err := getPassword(ctx, sr.client, role, sr.instance.GetNamespaceName())
		if err != nil {
			return apiv1.PasswordState{}, err
		}

		databaseRole.password = sql.NullString{Valid: true, String: passwordSecret.password}
		passVersion = passwordSecret.version
	}

	var err error
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
	username string
	password string
	version  string
}

// getPassword retrieves the password stored in the Kubernetes secret for the
// RoleConfiguration
func getPassword(
	ctx context.Context,
	cl client.Client,
	roleInSpec roleConfigurationAdapter,
	namespace string,
) (passwordSecret, error) {
	secretName := roleInSpec.GetRoleSecretsName()
	// no secrets defined, will keep roleInSpec.Password nil
	if secretName == "" {
		return passwordSecret{}, nil
	}

	var secret corev1.Secret
	err := cl.Get(ctx,
		client.ObjectKey{Namespace: namespace, Name: secretName},
		&secret)
	if err != nil {
		return passwordSecret{}, fmt.Errorf("failed to get password secret %s: %w", secretName, err)
	}
	usernameFromSecret, passwordFromSecret, err := utils.GetUserPasswordFromSecret(&secret)
	if err != nil {
		return passwordSecret{}, err
	}
	if strings.TrimSpace(roleInSpec.Name) != strings.TrimSpace(usernameFromSecret) {
		err := fmt.Errorf("wrong username '%v' in secret, expected '%v'", usernameFromSecret, roleInSpec.Name)
		return passwordSecret{}, err
	}
	return passwordSecret{
			strings.TrimSpace(usernameFromSecret),
			strings.TrimSpace(passwordFromSecret),
			secret.GetResourceVersion(),
		},
		nil
}

// getPasswordSecretResourceVersion returns a list of resource version of the passwords secrets for managed roles
// stored as Kubernetes secrets
func getPasswordSecretResourceVersion(
	ctx context.Context,
	client client.Client,
	rolesInSpec []apiv1.RoleConfiguration,
	namespace string,
) (map[string]string, error) {
	re := make(map[string]string)
	for _, role := range rolesInSpec {
		if role.PasswordSecret == nil || role.DisablePassword {
			continue
		}
		passwordSecret, err := getPassword(ctx, client, roleConfigurationAdapter{RoleConfiguration: role}, namespace)
		if err != nil {
			return nil, err
		}
		re[role.Name] = passwordSecret.version
	}
	return re, nil
}

func getRolesToGrant(inRoleInDB, inRoleInSpec []string) []string {
	if len(inRoleInSpec) == 0 {
		return nil
	}
	if len(inRoleInDB) == 0 {
		return inRoleInSpec
	}
	var roleToGrant []string
	for _, v := range inRoleInSpec {
		if !slices.Contains(inRoleInDB, v) {
			roleToGrant = append(roleToGrant, v)
		}
	}
	return roleToGrant
}

func getRolesToRevoke(inRoleInDB, inRoleInSpec []string) []string {
	if len(inRoleInDB) == 0 {
		return nil
	}
	if len(inRoleInSpec) == 0 {
		return inRoleInDB
	}
	var roleToRevoke []string
	for _, v := range inRoleInDB {
		if !slices.Contains(inRoleInSpec, v) {
			roleToRevoke = append(roleToRevoke, v)
		}
	}
	return roleToRevoke
}
