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
	"reflect"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// DatabaseRole represents the role information read from / written to the Database
// The password management in the apiv1.RoleConfiguration assumes the use of Secrets,
// so cannot cleanly be mapped to Postgres
type DatabaseRole struct {
	Name            string           `json:"name"`
	Comment         string           `json:"comment,omitempty"`
	Superuser       bool             `json:"superuser,omitempty"`
	CreateDB        bool             `json:"createdb,omitempty"`
	CreateRole      bool             `json:"createrole,omitempty"`
	Inherit         bool             `json:"inherit,omitempty"` // defaults to true
	Login           bool             `json:"login,omitempty"`
	Replication     bool             `json:"replication,omitempty"`
	BypassRLS       bool             `json:"bypassrls,omitempty"` // Row-Level Security
	ignorePassword  bool             `json:"-"`
	ConnectionLimit int64            `json:"connectionLimit,omitempty"` // default is -1
	ValidUntil      pgtype.Timestamp `json:"validUntil,omitempty"`
	InRoles         []string         `json:"inRoles,omitempty"`
	password        sql.NullString   `json:"-"`
	transactionID   int64            `json:"-"`
}

// passwordNeedsUpdating evaluates whether a DatabaseRole needs to be updated
func (d *DatabaseRole) passwordNeedsUpdating(
	storedPasswordState map[string]apiv1.PasswordState,
	latestSecretResourceVersion map[string]string,
) bool {
	return storedPasswordState[d.Name].SecretResourceVersion != latestSecretResourceVersion[d.Name] ||
		storedPasswordState[d.Name].TransactionID != d.transactionID
}

func (d *DatabaseRole) hasSameCommentAs(inSpec apiv1.RoleConfiguration) bool {
	return d.Comment == inSpec.Comment
}

func (d *DatabaseRole) isInSameRolesAs(inSpec apiv1.RoleConfiguration) bool {
	if len(d.InRoles) == 0 && len(inSpec.InRoles) == 0 {
		return true
	}

	if len(d.InRoles) != len(inSpec.InRoles) {
		return false
	}

	sort.Strings(d.InRoles)
	sort.Strings(inSpec.InRoles)
	return reflect.DeepEqual(d.InRoles, inSpec.InRoles)
}

func (d *DatabaseRole) hasSameValidUntilAs(inSpec apiv1.RoleConfiguration) bool {
	if inSpec.ValidUntil == nil {
		return !d.ValidUntil.Valid || d.ValidUntil.InfinityModifier == pgtype.Infinity
	}
	return d.ValidUntil.Valid && d.ValidUntil.Time.Equal(inSpec.ValidUntil.Time)
}

// isEquivalentTo checks a subset of the attributes of roles in DB and Spec
// leaving passwords and role membership (InRoles) to be done separately
func (d *DatabaseRole) isEquivalentTo(inSpec apiv1.RoleConfiguration) bool {
	type reducedEntries struct {
		Name            string
		Superuser       bool
		CreateDB        bool
		CreateRole      bool
		Inherit         bool
		Login           bool
		Replication     bool
		BypassRLS       bool
		ConnectionLimit int64
	}
	role := reducedEntries{
		Name:            d.Name,
		Superuser:       d.Superuser,
		CreateDB:        d.CreateDB,
		CreateRole:      d.CreateRole,
		Inherit:         d.Inherit,
		Login:           d.Login,
		Replication:     d.Replication,
		BypassRLS:       d.BypassRLS,
		ConnectionLimit: d.ConnectionLimit,
	}
	spec := reducedEntries{
		Name:            inSpec.Name,
		Superuser:       inSpec.Superuser,
		CreateDB:        inSpec.CreateDB,
		CreateRole:      inSpec.CreateRole,
		Inherit:         inSpec.GetRoleInherit(),
		Login:           inSpec.Login,
		Replication:     inSpec.Replication,
		BypassRLS:       inSpec.BypassRLS,
		ConnectionLimit: inSpec.ConnectionLimit,
	}

	return reflect.DeepEqual(role, spec) && d.hasSameValidUntilAs(inSpec)
}

// RoleManager abstracts the functionality of reconciling with PostgreSQL roles
type RoleManager interface {
	// List the roles in the database
	List(ctx context.Context) ([]DatabaseRole, error)
	// Update the role in the database
	Update(ctx context.Context, role DatabaseRole) error
	// Create the role in the database
	Create(ctx context.Context, role DatabaseRole) error
	// Delete the role in the database
	Delete(ctx context.Context, role DatabaseRole) error
	// GetLastTransactionID returns the last TransactionID as the `xmin`
	// from the database
	// See https://www.postgresql.org/docs/current/datatype-oid.html for reference
	GetLastTransactionID(ctx context.Context, role DatabaseRole) (int64, error)
	// UpdateComment Update the comment of role in the database
	UpdateComment(ctx context.Context, role DatabaseRole) error
	// UpdateMembership Update the In Role membership of role in the database
	UpdateMembership(ctx context.Context, role DatabaseRole, rolesToGrant []string, rolesToRevoke []string) error
	// GetParentRoles returns the roles the given role is a member of
	GetParentRoles(ctx context.Context, role DatabaseRole) ([]string, error)
}
