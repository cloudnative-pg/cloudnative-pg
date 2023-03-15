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
)

// DatabaseRole represents the role information read from / written to the Database
// The password management in the apiv1.RoleConfiguration assumes the use of Secrets,
// so cannot cleanly mapped to Postgres
type DatabaseRole struct {
	Name            string         `json:"name"`
	Comment         string         `json:"comment,omitempty"`
	Superuser       bool           `json:"superuser,omitempty"`
	CreateDB        bool           `json:"createdb,omitempty"`
	CreateRole      bool           `json:"createrole,omitempty"`
	Inherit         bool           `json:"inherit,omitempty"` // defaults to true
	Login           bool           `json:"login,omitempty"`
	Replication     bool           `json:"replication,omitempty"`
	BypassRLS       bool           `json:"bypassrls,omitempty"`       // Row-Level Security
	ConnectionLimit int64          `json:"connectionLimit,omitempty"` // default is -1
	ValidUntil      string         `json:"validUntil,omitempty"`
	InRoles         []string       `json:"inRoles,omitempty"`
	password        sql.NullString `json:"-"`
	transactionID   int64          `json:"-"`
}

// ReservedRoles is the set of roles managed by the operator that should
// never be put in the managed roles stanza
var ReservedRoles = map[string]bool{
	"cnpg_pooler_pgbouncer": true,
	"streaming_replica":     true,
	"postgres":              true,
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
}
