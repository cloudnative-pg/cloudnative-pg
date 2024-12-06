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
	"database/sql"
	"reflect"

	"github.com/cloudnative-pg/machinery/pkg/stringset"
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

// isInRolesNotInDB checks if the InRoles in the spec are a subset of the InRoles in the DB
func (d *DatabaseRole) isInRolesInDB(inSpec apiv1.RoleConfiguration) bool {
	if len(d.InRoles) == 0 && len(inSpec.InRoles) == 0 {
		return true
	}

	if len(d.InRoles) < len(inSpec.InRoles) {
		return false
	}

	for _, role := range inSpec.InRoles {
		if !stringset.From(d.InRoles).Has(role) {
			return false
		}
	}
	return true
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
