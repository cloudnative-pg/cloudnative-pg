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
	"reflect"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

type DatabaseRoleGrant struct {
	Name    string `json:"name"`
	Admin   *bool  `json:"admin,omitempty"`
	Inherit *bool  `json:"inherit,omitempty"`
	Set     *bool  `json:"set,omitempty"`
}

func NewDatabaseRoleGrant(name string) DatabaseRoleGrant {
	return DatabaseRoleGrant{
		Name:    name,
		Admin:   nil,
		Inherit: nil,
		Set:     nil,
	}
}

func NewDatabaseRoleGrantFromRoleGrant(roleGrant apiv1.RoleGrant) DatabaseRoleGrant {
	return DatabaseRoleGrant{
		Name:    roleGrant.Name,
		Admin:   roleGrant.Admin,
		Inherit: roleGrant.Inherit,
		Set:     roleGrant.Set,
	}
}

// `Matches` returns true if the name of this and other match and their options
// set don't collide with each other, treating nil as not-conflicting.
//
// This is required because the database always returns the default values as
// set, but we are possibly comparing it to values from the spec, which might
// contain 'nil' where the default values are desired.
func (g *DatabaseRoleGrant) Matches(other DatabaseRoleGrant) bool {
	return g.Name == other.Name &&
		(g.Admin == nil || other.Admin == nil || *g.Admin == *other.Admin) &&
		(g.Inherit == nil || other.Inherit == nil || *g.Inherit == *other.Inherit) &&
		(g.Set == nil || other.Set == nil || *g.Set == *other.Set)
}

func (g *DatabaseRoleGrant) hasOverriddenDefaults() bool {
	return g.Admin != nil || g.Set != nil || g.Inherit != nil
}

func (g *DatabaseRoleGrant) ToWithOptionQuery() string {
	if !g.hasOverriddenDefaults() {
		return ""
	}
	queryParts := []string{}
	if g.Admin != nil {
		queryParts = append(queryParts, fmt.Sprintf(`ADMIN %v`,
			*g.Admin,
		))
	}

	if g.Inherit != nil {
		queryParts = append(queryParts, fmt.Sprintf(`INHERIT %v`,
			*g.Inherit,
		))
	}

	if g.Set != nil {
		queryParts = append(queryParts, fmt.Sprintf(`SET %v`,
			*g.Set,
		))
	}

	return fmt.Sprintf(`WITH %s`, strings.Join(queryParts, ", "))
}

// DatabaseRole represents the role information read from / written to the Database
// The password management in the apiv1.RoleConfiguration assumes the use of Secrets,
// so cannot cleanly be mapped to Postgres
type DatabaseRole struct {
	Name            string              `json:"name"`
	Comment         string              `json:"comment,omitempty"`
	Superuser       bool                `json:"superuser,omitempty"`
	CreateDB        bool                `json:"createdb,omitempty"`
	CreateRole      bool                `json:"createrole,omitempty"`
	Inherit         bool                `json:"inherit,omitempty"` // defaults to true
	Login           bool                `json:"login,omitempty"`
	Replication     bool                `json:"replication,omitempty"`
	BypassRLS       bool                `json:"bypassrls,omitempty"` // Row-Level Security
	ignorePassword  bool                `json:"-"`
	ConnectionLimit int64               `json:"connectionLimit,omitempty"` // default is -1
	ValidUntil      pgtype.Timestamp    `json:"validUntil"`
	InRoles         []string            `json:"inRoles,omitempty"`
	RoleGrants      []DatabaseRoleGrant `json:"roleGrants,omitempty"`
	password        sql.NullString      `json:"-"`
	// passwordPassthrough, when true, instructs the instance manager to send the
	// password literal verbatim rather than SCRAM-SHA-256 encoding it
	// client-side. It is populated from the cnpg.io/passwordPassthrough
	// annotation on the Secret backing the role. Write-only: List() results
	// always carry false, since PostgreSQL has no concept of the annotation.
	passwordPassthrough bool  `json:"-"`
	transactionID       int64 `json:"-"`
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

func (d *DatabaseRole) hasMatchingRoleGrants(inSpec apiv1.RoleConfiguration) bool {
	if len(d.InRoles)+len(d.RoleGrants) == 0 &&
		len(inSpec.InRoles)+len(inSpec.RoleGrants) == 0 {
		return true
	}

	if len(d.InRoles)+len(d.RoleGrants) !=
		len(inSpec.InRoles)+len(inSpec.RoleGrants) {
		return false
	}

	collectedInSpecRoleGrants := make(map[string]DatabaseRoleGrant)

	for _, r := range inSpec.InRoles {
		collectedInSpecRoleGrants[r] = NewDatabaseRoleGrant(r)
	}

	for _, r := range inSpec.RoleGrants {
		collectedInSpecRoleGrants[r.Name] = NewDatabaseRoleGrantFromRoleGrant(r)
	}

	for _, r := range d.InRoles {
		// For InRoles we are not interested in comparing Admin, Inherit and Set
		// options; it's only relevant that the role exists anywhere in the spec
		if _, ok := collectedInSpecRoleGrants[r]; !ok {
			return false
		}
	}

	for _, r := range d.RoleGrants {
		roleGrant, ok := collectedInSpecRoleGrants[r.Name]
		if !ok {
			return false
		}
		if !roleGrant.Matches(r) {
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
// leaving passwords and role membership (InRoles,RoleGrants) to be done
// separately
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

// ApplyPassword updates a database role with the password located in the Secret,
// and it returns the resource version of the Secret
func (d *DatabaseRole) ApplyPassword(
	ctx context.Context,
	cl client.Client,
	config *apiv1.RoleConfiguration,
	namespace string,
) (string, error) {
	switch {
	case config.GetRoleSecretName() == "" && !config.DisablePassword:
		d.ignorePassword = true
		return "", nil
	case config.GetRoleSecretName() == "" && config.DisablePassword:
		d.password = sql.NullString{}
		return "", nil
	case config.GetRoleSecretName() != "" && config.DisablePassword:
		// For DatabaseRole CRDs this is prevented by CEL validation.
		// For inline managed roles this is a runtime error.
		return "",
			fmt.Errorf("cannot reconcile: password both provided and disabled: %s",
				config.GetRoleSecretName())
	default:
		passwordSecret, err := getPassword(ctx, cl, config, namespace)
		if err != nil {
			return "", err
		}

		d.password = sql.NullString{Valid: true, String: passwordSecret.password}
		d.passwordPassthrough = passwordSecret.passthrough
		return passwordSecret.version, nil
	}
}

// GetDatabaseRoleGrants helps with translating both InRoles and RoleGrants
// to DatabaseRoleGrants.
func (d *DatabaseRole) GetDatabaseRoleGrants() []DatabaseRoleGrant {
	roleGrants := d.RoleGrants
	for _, inRoleName := range d.InRoles {
		roleGrants = append(roleGrants, NewDatabaseRoleGrant(inRoleName))
	}
	return roleGrants
}

func (d *DatabaseRole) PopulateRoleGrantsFromStringArray(roleGrantStrings []string) error {
	d.RoleGrants = []DatabaseRoleGrant{}
	for _, roleGrantString := range roleGrantStrings {
		roleGrantParts := strings.Split(roleGrantString, "|")
		if len(roleGrantParts) != 4 {
			return fmt.Errorf("MatchGrants can only match against results with 4 columns, got %d (%v)", len(roleGrantParts), roleGrantParts)
		}

		adminOption, err := strconv.ParseBool(roleGrantParts[1])
		if err != nil {
			return fmt.Errorf("Couldn't convert %v to boolean admin option", roleGrantParts[1])
		}

		inheritOption, err := strconv.ParseBool(roleGrantParts[2])
		if err != nil {
			return fmt.Errorf("Couldn't convert %v to boolean inherit option", roleGrantParts[2])
		}

		setOption, err := strconv.ParseBool(roleGrantParts[3])
		if err != nil {
			return fmt.Errorf("Couldn't convert %v to boolean set option", roleGrantParts[3])
		}

		d.RoleGrants = append(d.RoleGrants, DatabaseRoleGrant{
			Name:    roleGrantParts[0],
			Admin:   &adminOption,
			Inherit: &inheritOption,
			Set:     &setOption,
		})
	}

	return nil
}
