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

package roles

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// A RoleSynchronizer is a Kubernetes manager.Runnable
// that makes sure the Roles in the PostgreSQL databases are in sync with the spec
//
// c.f. https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager#Runnable
type RoleSynchronizer struct {
	instance *postgres.Instance
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
	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
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
			contextLog.Info("RoleSynchronizer loop triggered")

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

	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
	contextLog.Info("reconciling managed roles")

	superUserDB, err := sr.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while reconciling managed roles: %w", err)
	}
	roleManager := NewPostgresRoleManager(superUserDB)

	var rolePasswords map[string]apiv1.PasswordState
	appliedState, err := sr.synchronizeRoles(ctx, roleManager, config, rolePasswords)
	if err != nil {
		return fmt.Errorf("while syncrhonizing managed roles: %w", err)
	}

	var remoteCluster apiv1.Cluster
	err = sr.client.Get(ctx, types.NamespacedName{
		Name:      sr.instance.ClusterName,
		Namespace: sr.instance.Namespace,
	}, &remoteCluster)

	updatedCluster := remoteCluster.DeepCopy()
	updatedCluster.Status.RolePasswordStatus = appliedState
	return sr.client.Status().Patch(ctx, updatedCluster, client.MergeFrom(&remoteCluster))
}

func getRoleNames(roles []apiv1.RoleConfiguration) []string {
	names := make([]string, len(roles))
	for i, role := range roles {
		names[i] = role.Name
	}
	return names
}

func (sr *RoleSynchronizer) dropRolesFromSpec(
	ctx context.Context,
	roleManager RoleManager,
	roles []apiv1.RoleConfiguration,
) error {
	for _, role := range roles {
		err := roleManager.Delete(ctx, newDatabaseRoleBuilder().withRole(role).build())
		if err != nil {
			return fmt.Errorf("while delete role %s: %w", role.Name, err)
		}
	}
	return nil
}

func (sr *RoleSynchronizer) updateRoleCommentFromSpec(
	ctx context.Context,
	roleManager RoleManager,
	roles []apiv1.RoleConfiguration,
) error {
	for _, role := range roles {
		err := roleManager.UpdateComment(ctx, newDatabaseRoleBuilder().withRole(role).build())
		if err != nil {
			return fmt.Errorf("while update comments for role %s: %w", role.Name, err)
		}
	}
	return nil
}

// synchronizeRoles aligns roles in the database to the spec
func (sr *RoleSynchronizer) synchronizeRoles(
	ctx context.Context,
	roleManager RoleManager,
	config *apiv1.ManagedConfiguration,
	storedPasswordState map[string]apiv1.PasswordState,
) (map[string]apiv1.PasswordState, error) {
	hashes, err := getPasswordHashes(ctx, sr.client, config.Roles, sr.instance.Namespace)
	if err != nil {
		return nil, err
	}
	rolesInDB, err := roleManager.List(ctx)
	if err != nil {
		return nil, err
	}
	rolesByAction := evaluateRoleActions(ctx, config, rolesInDB, storedPasswordState, hashes)
	if err != nil {
		return nil, fmt.Errorf("while syncrhonizing managed roles: %w", err)
	}

	res, err := sr.applyRoleActions(
		ctx,
		roleManager,
		rolesByAction,
	)
	if err != nil {
		return nil, fmt.Errorf("while synchronizing roles in primary: %w", err)
	}

	return res, nil
}

// applyRoleActions applies the actions to reconcile roles in the DB with the Spec
// nolint: gocognit
func (sr *RoleSynchronizer) applyRoleActions(
	ctx context.Context,
	roleManager RoleManager,
	rolesByAction map[roleAction][]apiv1.RoleConfiguration,
) (map[string]apiv1.PasswordState, error) {
	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
	contextLog.Info("applying role actions")

	appliedChanges := make(map[string]apiv1.PasswordState)
	for action, roles := range rolesByAction {
		contextLog.Info("roles in DB out of sync with Spec, evaluating action",
			"roles", getRoleNames(roles), "action", action)

		switch action {
		case roleIgnore, roleIsReconciled, roleIsReserved:
			contextLog.Debug("no action required", "action", action)
			continue
		case roleCreate, roleUpdate:
			for _, role := range roles {
				pass, err := getPassword(ctx, sr.client, role, sr.instance.Namespace)
				if err != nil {
					return nil, err
				}

				databaseRole := newDatabaseRoleBuilder().withRole(role).withPassword(pass).build()
				switch action {
				case roleCreate:
					if err := roleManager.Create(ctx, databaseRole); err != nil {
						return nil, err
					}
				case roleUpdate:
					if err := roleManager.Update(ctx, databaseRole); err != nil {
						return nil, err
					}
				default:
					return nil, fmt.Errorf("unsupported roleAction %s", action)
				}

				transactionID, err := roleManager.GetLastTransactionID(ctx, databaseRole)
				if err != nil {
					return nil, err
				}
				appliedChanges[role.Name] = apiv1.PasswordState{
					TransactionID: transactionID,
					PasswordHash:  hashPassword(pass),
				}
			}
		case roleDelete:
			if err := sr.dropRolesFromSpec(ctx, roleManager, roles); err != nil {
				return nil, err
			}
		case roleSetComment:
			if err := sr.updateRoleCommentFromSpec(ctx, roleManager, roles); err != nil {
				return nil, err
			}
		}
	}

	return appliedChanges, nil
}

// roleAction encodes the action necessary for a role, i.e. ignore, or CRUD
type roleAction string

// possible role actions
const (
	roleIsReconciled roleAction = "RECONCILED"
	roleCreate       roleAction = "CREATE"
	roleDelete       roleAction = "DELETE"
	roleUpdate       roleAction = "UPDATE"
	roleIgnore       roleAction = "IGNORE"
	roleIsReserved   roleAction = "RESERVED"
	roleSetComment   roleAction = "SET_COMMENT"
)

// evaluateRoleActions evaluates the action needed for each role in the DB and/or the Spec.
// It has no side effects
func evaluateRoleActions(
	ctx context.Context,
	config *apiv1.ManagedConfiguration,
	rolesInDB []DatabaseRole,
	lastPasswordState map[string]apiv1.PasswordState,
	passwordsInSpec map[string][]byte,
) map[roleAction][]apiv1.RoleConfiguration {
	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
	contextLog.Info("evaluating role actions")

	rolesInSpec := config.Roles
	// set up a map name -> role for the spec roles
	roleInSpecNamed := make(map[string]apiv1.RoleConfiguration)
	for _, r := range rolesInSpec {
		roleInSpecNamed[r.Name] = r
	}

	rolesByAction := make(map[roleAction][]apiv1.RoleConfiguration)
	// 1. find the next actions for the roles in the DB
	roleInDBNamed := make(map[string]DatabaseRole)
	for _, role := range rolesInDB {
		roleInDBNamed[role.Name] = role
		inSpec, isInSpec := roleInSpecNamed[role.Name]
		switch {
		case ReservedRoles[role.Name]:
			rolesByAction[roleIsReserved] = append(rolesByAction[roleIsReserved], apiv1.RoleConfiguration{Name: role.Name})
		case isInSpec && inSpec.Ensure == apiv1.EnsureAbsent:
			rolesByAction[roleDelete] = append(rolesByAction[roleDelete], apiv1.RoleConfiguration{Name: role.Name})
		case isInSpec && (!role.isEquivalent(inSpec) || role.passwordNeedsUpdating(lastPasswordState, passwordsInSpec)):
			rolesByAction[roleUpdate] = append(rolesByAction[roleUpdate], inSpec)
		case isInSpec && !role.isCommentEqual(inSpec):
			rolesByAction[roleSetComment] = append(rolesByAction[roleSetComment], inSpec)
		case !isInSpec:
			rolesByAction[roleIgnore] = append(rolesByAction[roleIgnore], apiv1.RoleConfiguration{Name: role.Name})
		default:
			rolesByAction[roleIsReconciled] = append(rolesByAction[roleIsReconciled], apiv1.RoleConfiguration{Name: role.Name})
		}
	}

	// 2. get status of roles in spec missing from the DB
	for _, r := range rolesInSpec {
		_, isInDB := roleInDBNamed[r.Name]
		if isInDB {
			continue // covered by the previous loop
		}
		if r.Ensure == apiv1.EnsurePresent {
			rolesByAction[roleCreate] = append(rolesByAction[roleCreate], r)
		} else {
			rolesByAction[roleIsReconciled] = append(rolesByAction[roleIsReconciled], r)
		}
	}

	return rolesByAction
}

// getRoleStatus gets the status of every role in the Spec and/or in the DB
func getRoleStatus(
	ctx context.Context,
	roleManager RoleManager,
	config *apiv1.ManagedConfiguration,
	storedPasswordState map[string]apiv1.PasswordState,
	passwordHashes map[string][]byte,
) (map[apiv1.RoleStatus][]apiv1.RoleConfiguration, error) {
	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
	contextLog.Info("getting the managed roles status")

	rolesInDB, err := roleManager.List(ctx)
	if err != nil {
		return nil, err
	}

	rolesByAction := evaluateRoleActions(ctx, config, rolesInDB, storedPasswordState, passwordHashes)

	statusByAction := map[roleAction]apiv1.RoleStatus{
		roleCreate:       apiv1.RoleStatusPendingReconciliation,
		roleDelete:       apiv1.RoleStatusPendingReconciliation,
		roleUpdate:       apiv1.RoleStatusPendingReconciliation,
		roleSetComment:   apiv1.RoleStatusPendingReconciliation,
		roleIsReconciled: apiv1.RoleStatusReconciled,
		roleIgnore:       apiv1.RoleStatusNotManaged,
		roleIsReserved:   apiv1.RoleStatusReserved,
	}

	rolesByStatus := make(map[apiv1.RoleStatus][]apiv1.RoleConfiguration)
	for action, roles := range rolesByAction {
		// NOTE: several actions map to the PendingReconciliation status, so
		// we need to append the roles in each action
		rolesByStatus[statusByAction[action]] = append(rolesByStatus[statusByAction[action]], roles...)
	}

	return rolesByStatus, nil
}

// getPassword retrieves the password stored in the Kubernetes secret for the
// RoleConfiguration
func getPassword(
	ctx context.Context,
	cl client.Client,
	roleInSpec apiv1.RoleConfiguration,
	namespace string,
) (string, error) {
	secretName := roleInSpec.GetRoleSecretsName()
	// no secrets defined, will keep roleInSpec.Password nil
	if secretName == "" {
		return "", nil
	}

	var secret corev1.Secret
	err := cl.Get(ctx,
		client.ObjectKey{Namespace: namespace, Name: secretName},
		&secret)
	if err != nil {
		return "", err
	}
	usernameFromSecret, passwordFromSecret, err := utils.GetUserPasswordFromSecret(&secret)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(roleInSpec.Name) != strings.TrimSpace(usernameFromSecret) {
		err := fmt.Errorf("wrong username '%v' in secret, expected '%v'", usernameFromSecret, roleInSpec.Name)
		return "", err
	}
	return strings.TrimSpace(passwordFromSecret), nil
}

func hashPassword(pass string) []byte {
	if pass == "" {
		return nil
	}

	hash := sha256.Sum256([]byte(pass))
	return hash[:]
}

// getPasswordHashes returns a list of hashes of the passwords for managed roles
// stored as Kubernetes secrets
func getPasswordHashes(
	ctx context.Context,
	client client.Client,
	rolesInSpec []apiv1.RoleConfiguration,
	namespace string,
) (map[string][]byte, error) {
	re := make(map[string][]byte)
	for _, role := range rolesInSpec {
		pass, err := getPassword(ctx, client, role, namespace)
		if err != nil {
			return nil, err
		}
		re[role.Name] = hashPassword(pass)
	}
	return re, nil
}
