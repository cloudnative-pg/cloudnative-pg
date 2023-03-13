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
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
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

func (sr *RoleSynchronizer) reconcile(ctx context.Context, config *apiv1.ManagedConfiguration) error {
	var err error

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from a panic: %s", r)
		}
	}()

	superUserDB, err := sr.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while reconciling managed roles: %w", err)
	}
	roleManager := NewPostgresRoleManager(superUserDB)

	var rolePasswords map[string]apiv1.PasswordState
	return sr.synchronizeRoles(ctx, roleManager, config, rolePasswords)
}

func getRoleNames(roles []apiv1.RoleConfiguration) []string {
	names := make([]string, len(roles))
	for i, role := range roles {
		names[i] = role.Name
	}
	return names
}

func (sr *RoleSynchronizer) createRoleFromSpec(
	ctx context.Context,
	roleManager RoleManager,
	role apiv1.RoleConfiguration,
) error {
	pass, err := getPassword(ctx, sr.client, role, sr.instance.Namespace)
	if err != nil {
		return fmt.Errorf("while getting role password for %s: %w", role.Name, err)
	}
	return roleManager.Create(ctx, managedToDatabase(role, pass))
}

func (sr *RoleSynchronizer) updateRoleFromSpec(
	ctx context.Context,
	roleManager RoleManager,
	role apiv1.RoleConfiguration,
) error {
	pass, err := getPassword(ctx, sr.client, role, sr.instance.Namespace)
	if err != nil {
		return fmt.Errorf("while getting role password for %s: %w", role.Name, err)
	}
	return roleManager.Update(ctx, managedToDatabase(role, pass))
}

// synchronizeRoles aligns roles in the database to the spec
func (sr *RoleSynchronizer) synchronizeRoles(
	ctx context.Context,
	roleManager RoleManager,
	config *apiv1.ManagedConfiguration,
	storedPasswordState map[string]apiv1.PasswordState,
) error {
	wrapErr := func(err error) error {
		return fmt.Errorf("while syncrhonizing managed roles: %w", err)
	}
	hashes, err := getPasswordHashes(ctx, sr.client, config.Roles, sr.instance.Namespace)
	if err != nil {
		return wrapErr(err)
	}
	rolesInDB, err := roleManager.List(ctx)
	if err != nil {
		return wrapErr(err)
	}
	rolesByAction := evaluateRoleActions(ctx, config, rolesInDB, storedPasswordState, hashes)
	if err != nil {
		return fmt.Errorf("while syncrhonizing managed roles: %w", err)
	}

	return sr.applyRoleActions(
		ctx,
		roleManager,
		rolesByAction,
	)
}

// applyRoleActions applies the actions to reconcile roles in the DB with the Spec
func (sr *RoleSynchronizer) applyRoleActions(
	ctx context.Context,
	roleManager RoleManager,
	rolesByAction map[roleAction][]apiv1.RoleConfiguration,
) error {
	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
	contextLog.Info("synchronizing roles",
		"podName", sr.instance.PodName)

	wrapErr := func(err error) error {
		return fmt.Errorf("while synchronizing roles in primary: %w", err)
	}

	// Note that the roleIgnore, roleIsReconciled, and roleIsReserved require no action
	for action, roles := range rolesByAction {
		switch action {
		case roleCreate:
			contextLog.Info("roles in Spec missing from the DB. Creating",
				"roles", getRoleNames(roles))
			for _, role := range roles {
				err := sr.createRoleFromSpec(ctx, roleManager, role)
				if err != nil {
					return wrapErr(err)
				}
			}
		case roleUpdate:
			contextLog.Info("roles in DB out of sync with Spec. Updating",
				"roles", getRoleNames(roles))
			for _, role := range roles {
				err := sr.updateRoleFromSpec(ctx, roleManager, role)
				if err != nil {
					return wrapErr(err)
				}
			}
		case roleDelete:
			contextLog.Info("roles in DB marked as Ensure:Absent in Spec. Deleting",
				"roles", getRoleNames(roles))
			for _, role := range roles {
				err := roleManager.Delete(ctx, managedToDatabase(role, sql.NullString{}))
				if err != nil {
					return wrapErr(err)
				}
			}
		}
	}

	return nil
}

// roleAction encodes the action necessary for a role, i.e. ignore, or CRUD
type roleAction int

// possible role actions
const (
	roleIsReconciled roleAction = iota
	roleCreate
	roleDelete
	roleUpdate
	roleIgnore
	roleIsReserved
	roleSetPassword
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
	contextLog.Info("evaluating the role actions")

	rolesInSpec := config.Roles
	// set up a map name -> role for the spec roles
	roleInSpecNamed := make(map[string]apiv1.RoleConfiguration)
	for _, r := range rolesInSpec {
		roleInSpecNamed[r.Name] = r
	}

	passwordNeedsUpdating := getPasswordEvaluator(lastPasswordState, passwordsInSpec)

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
		case isInSpec && !areEquivalent(role, inSpec):
			rolesByAction[roleUpdate] = append(rolesByAction[roleUpdate], inSpec)
		case isInSpec && passwordNeedsUpdating(role):
			rolesByAction[roleSetPassword] = append(rolesByAction[roleSetPassword], inSpec)
		case !isInSpec:
			rolesByAction[roleIgnore] = append(rolesByAction[roleIgnore], apiv1.RoleConfiguration{Name: role.Name})
		default:
			rolesByAction[roleIsReconciled] = append(rolesByAction[roleIsReconciled], apiv1.RoleConfiguration{Name: role.Name})
		}
	}

	contextLog.Info("roles in spec", "role", rolesInSpec)
	// 2. get status of roles in spec missing from the DB
	for _, r := range rolesInSpec {
		_, isInDB := roleInDBNamed[r.Name]
		if isInDB {
			continue // covered by the previous loop
		}
		contextLog.Info("roles in spec but not db", "role", r.Name)
		if r.Ensure == apiv1.EnsurePresent {
			rolesByAction[roleCreate] = append(rolesByAction[roleCreate], r)
		} else {
			rolesByAction[roleIsReconciled] = append(rolesByAction[roleIsReconciled], r)
		}
	}

	return rolesByAction
}

// getPasswordEvaluator creates a function that will evaluate whether a
// DatabaseRole needs to be updated
func getPasswordEvaluator(
	storedPasswordState map[string]apiv1.PasswordState,
	passwordsInSpec map[string][]byte,
) func(role DatabaseRole) bool {
	return func(role DatabaseRole) bool {
		return !bytes.Equal(storedPasswordState[role.Name].PasswordHash, passwordsInSpec[role.Name]) ||
			storedPasswordState[role.Name].TransactionID != role.transactionID
	}
}

// getRoleStatus gets the status of every role in the Spec and/or in the DB
func getRoleStatus(
	ctx context.Context,
	roleManager RoleManager,
	config *apiv1.ManagedConfiguration,
	storedPasswordState map[string]apiv1.PasswordState,
	passwordHashes map[string][]byte,
) (map[string]apiv1.RoleStatus, map[string]apiv1.PasswordState) {
	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
	contextLog.Info("getting the managed roles status")

	rolesInDB, err := roleManager.List(ctx)
	if err != nil {
		return nil, nil
	}

	rolesByAction := evaluateRoleActions(ctx, config, rolesInDB, storedPasswordState, passwordHashes)
	if err != nil {
		return nil, nil
	}

	statusByAction := map[roleAction]apiv1.RoleStatus{
		roleCreate:       apiv1.RoleStatusPendingReconciliation,
		roleDelete:       apiv1.RoleStatusPendingReconciliation,
		roleUpdate:       apiv1.RoleStatusPendingReconciliation,
		roleSetPassword:  apiv1.RoleStatusPendingReconciliation,
		roleIsReconciled: apiv1.RoleStatusReconciled,
		roleIgnore:       apiv1.RoleStatusNotManaged,
		roleIsReserved:   apiv1.RoleStatusReserved,
	}

	statusByRole := make(map[string]apiv1.RoleStatus)
	for action, roles := range rolesByAction {
		for _, role := range roles {
			statusByRole[role.Name] = statusByAction[action]
		}
	}

	passwordSate := make(map[string]apiv1.PasswordState)
	for _, role := range rolesInDB {
		passwordSate[role.Name] = apiv1.PasswordState{
			TransactionID: role.transactionID,
			PasswordHash:  passwordHashes[role.Name],
		}
	}

	return statusByRole, passwordSate
}

// getPassword retrieves the password stored in the Kubernetes secret for the
// RoleConfiguration
func getPassword(ctx context.Context, cl client.Client,
	roleInSpec apiv1.RoleConfiguration, namespace string,
) (sql.NullString, error) {
	secretName := roleInSpec.GetRoleSecretsName()
	// no secrets defined, will keep roleInSpec.Password nil
	if secretName == "" {
		return sql.NullString{}, nil
	}

	var secret corev1.Secret
	err := cl.Get(ctx,
		client.ObjectKey{Namespace: namespace, Name: secretName},
		&secret)
	if err != nil {
		return sql.NullString{}, err
	}
	usernameFromSecret, passwordFromSecret, err := utils.GetUserPasswordFromSecret(&secret)
	if err != nil {
		return sql.NullString{}, err
	}
	if strings.TrimSpace(roleInSpec.Name) != strings.TrimSpace(usernameFromSecret) {
		err := fmt.Errorf("wrong username '%v' in secret, expected '%v'", usernameFromSecret, roleInSpec.Name)
		return sql.NullString{}, err
	}
	return sql.NullString{
		Valid:  true,
		String: strings.TrimSpace(passwordFromSecret),
	}, nil
}

func getPasswordHashes(ctx context.Context, client client.Client,
	rolesInSpec []apiv1.RoleConfiguration, namespace string,
) (map[string][]byte, error) {
	re := make(map[string][]byte)
	for _, role := range rolesInSpec {
		pass, err := getPassword(ctx, client, role, namespace)
		if err != nil {
			return nil, err
		}
		encoder := sha256.New()
		_, err = encoder.Write([]byte(pass.String))
		if err != nil {
			return nil, err
		}
		re[role.Name] = encoder.Sum(nil)
	}
	return re, nil
}

// areEquivalent checks a subset of the attributes of roles in DB and Spec
// leaving passwords and role membership (InRoles) to be done separately
func areEquivalent(inDB DatabaseRole, inSpec apiv1.RoleConfiguration) bool {
	reducedEntries := []struct {
		Name            string
		Comment         string
		Superuser       bool
		CreateDB        bool
		CreateRole      bool
		Inherit         bool
		Login           bool
		Replication     bool
		BypassRLS       bool
		ConnectionLimit int64
		ValidUntil      string
	}{
		{
			Name:            inDB.Name,
			Comment:         inDB.Comment,
			Superuser:       inDB.Superuser,
			CreateDB:        inDB.CreateDB,
			CreateRole:      inDB.CreateDB,
			Inherit:         inDB.Inherit,
			Login:           inDB.Login,
			Replication:     inDB.Replication,
			BypassRLS:       inDB.BypassRLS,
			ConnectionLimit: inDB.ConnectionLimit,
			ValidUntil:      inDB.ValidUntil,
		},
		{
			Name:            inSpec.Name,
			Comment:         inSpec.Comment,
			Superuser:       inSpec.Superuser,
			CreateDB:        inSpec.CreateDB,
			CreateRole:      inSpec.CreateDB,
			Inherit:         inSpec.GetRoleInherit(),
			Login:           inSpec.Login,
			Replication:     inSpec.Replication,
			BypassRLS:       inSpec.BypassRLS,
			ConnectionLimit: inSpec.ConnectionLimit,
			ValidUntil:      inSpec.ValidUntil,
		},
	}
	return reducedEntries[0] == reducedEntries[1]
}

// managedToDatabase map the RoleConfiguration to DatabaseRole
func managedToDatabase(role apiv1.RoleConfiguration, password sql.NullString) DatabaseRole {
	return DatabaseRole{
		Name:            role.Name,
		Comment:         role.Comment,
		Superuser:       role.Superuser,
		CreateDB:        role.CreateDB,
		CreateRole:      role.CreateRole,
		Inherit:         role.GetRoleInherit(),
		Login:           role.Login,
		Replication:     role.Replication,
		BypassRLS:       role.BypassRLS,
		ConnectionLimit: role.ConnectionLimit,
		ValidUntil:      role.ValidUntil,
		InRoles:         role.InRoles,
		password:        password,
	}
}
