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
	"database/sql"
	"fmt"
	"strings"
	"time"

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
		contextLog.Info("skipping the role synchronization in replicas")
	}
	go func() {
		config := <-sr.instance.RoleSynchronizerChan()
		contextLog.Info("setting up role synchronizer loop")
		updateInterval := config.GetUpdateInterval()
		ticker := time.NewTicker(updateInterval)

		defer func() {
			ticker.Stop()
			contextLog.Info("Terminated RoleSynchronizer loop")
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case config = <-sr.instance.RoleSynchronizerChan():
			case <-ticker.C:
			}

			// If the spec contains no roles to manage, stop the timer,
			// the process will resume through the wakeUp channel if necessary
			if config == nil || len(config.Roles) == 0 {
				ticker.Stop()
				// we set updateInterval to 0 to make sure the Ticker will be reset
				// if the feature is enabled again
				updateInterval = 0
				continue
			}

			// Update the ticker if the update interval has changed
			newUpdateInterval := config.GetUpdateInterval()
			if updateInterval != newUpdateInterval {
				ticker.Reset(newUpdateInterval)
				updateInterval = newUpdateInterval
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

	return sr.synchronizeRoles(ctx, roleManager, config)
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
	pass, err := getPassword(ctx, sr, role)
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
	pass, err := getPassword(ctx, sr, role)
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
) error {
	rolesByAction, err := evaluateRoleActions(ctx, roleManager, config)
	if err != nil {
		return fmt.Errorf("while syncrhonizing managed roles: %w", err)
	}

	return sr.applyRoleActions(
		ctx,
		roleManager,
		config,
		rolesByAction,
	)
}

// applyRoleActions applies the actions to reconcile roles in the DB with the Spec
func (sr *RoleSynchronizer) applyRoleActions(
	ctx context.Context,
	roleManager RoleManager,
	config *apiv1.ManagedConfiguration,
	rolesByAction map[roleAction][]apiv1.RoleConfiguration,
) error {
	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
	contextLog.Info("synchronizing roles",
		"podName", sr.instance.PodName,
		"managedConfig", config)

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
)

// evaluateRoleActions evaluates the action needed for each role in the DB and/or the Spec.
// It has no side-effects
func evaluateRoleActions(
	ctx context.Context,
	roleManager RoleManager,
	config *apiv1.ManagedConfiguration,
) (map[roleAction][]apiv1.RoleConfiguration, error) {
	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
	contextLog.Info("evaluating the role actions")

	rolesInDB, err := roleManager.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("while evaluating the roles: %w", err)
	}

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
		case isInSpec:
			// TODO: this is very aggressive. We update the role each time. This
			// is due to the complexity of decrypting-encrypting SCRAM-SHA-256 so we
			// can compare if passwords have changed.
			// We have another ticket open to implement encryption/decription and then
			// we can issue updates only when the role in DB has drifted from spec
			rolesByAction[roleUpdate] = append(rolesByAction[roleUpdate], inSpec)
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

	return rolesByAction, nil
}

// getRoleStatus gets the status of every role in the Spec and/or in the DB
func getRoleStatus(
	ctx context.Context,
	roleManager RoleManager,
	config *apiv1.ManagedConfiguration,
) (map[string]apiv1.RoleStatus, error) {
	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
	contextLog.Info("getting the managed roles status")

	rolesByAction, err := evaluateRoleActions(ctx, roleManager, config)
	if err != nil {
		return nil, fmt.Errorf("while getting the ManagedRoles status: %w", err)
	}

	statusByAction := map[roleAction]apiv1.RoleStatus{
		roleCreate:       apiv1.RoleStatusPendingReconciliation,
		roleDelete:       apiv1.RoleStatusPendingReconciliation,
		roleUpdate:       apiv1.RoleStatusPendingReconciliation,
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

	return statusByRole, nil
}

// getPassword retrieves the password stored in the Kubernetes secret for the
// RoleConfiguration
func getPassword(ctx context.Context, sr *RoleSynchronizer,
	roleInSpec apiv1.RoleConfiguration,
) (sql.NullString, error) {
	secretName := roleInSpec.GetRoleSecretsName()
	// no secrets defined, will keep roleInSpec.Password nil
	if secretName == "" {
		return sql.NullString{}, nil
	}

	var secret corev1.Secret
	err := sr.client.Get(
		ctx,
		client.ObjectKey{Namespace: sr.instance.Namespace, Name: secretName},
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

// managedToDatabase map the RoleConfiguration to DatabaseRole
func managedToDatabase(role apiv1.RoleConfiguration, password sql.NullString) DatabaseRole {
	return DatabaseRole{
		Name:            role.Name,
		Comment:         role.Comment,
		Superuser:       role.Superuser,
		CreateDB:        role.CreateDB,
		CreateRole:      role.CreateRole,
		Inherit:         role.Inherit,
		Login:           role.Login,
		Replication:     role.Replication,
		BypassRLS:       role.BypassRLS,
		ConnectionLimit: role.ConnectionLimit,
		ValidUntil:      role.ValidUntil,
		InRoles:         role.InRoles,
		password:        password,
	}
}
