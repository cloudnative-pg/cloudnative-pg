package logicalsnapshot

import (
	"context"
	"fmt"

	"github.com/lib/pq"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

type roleInheritanceManager struct {
	origin      *pool.ConnectionPool
	destination *pool.ConnectionPool
}

// RoleInheritance contains the data needed to execute grants, based on pg_authid
type RoleInheritance struct {
	RoleID      string  `json:"roleid,omitempty"`
	Member      string  `json:"member,omitempty"`
	AdminOption bool    `json:"admin_option,omitempty"`
	Grantor     *string `json:"grantor,omitempty"`
}

func cloneRoleInheritance(ctx context.Context, destination *pool.ConnectionPool, origin *pool.ConnectionPool) error {
	rs := roleInheritanceManager{
		origin:      origin,
		destination: destination,
	}

	ri, err := rs.getRoleInheritance(ctx)
	if err != nil {
		return err
	}

	return rs.importRoleInheritance(ctx, ri)
}

func (rs *roleInheritanceManager) importRoleInheritance(ctx context.Context, ris []RoleInheritance) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("importing role inheritances")

	db, err := rs.destination.Connection(postgresDatabase)
	if err != nil {
		return err
	}

	for _, inheritance := range ris {
		query := fmt.Sprintf("GRANT %s TO %s ",
			pq.QuoteIdentifier(inheritance.RoleID),
			pq.QuoteIdentifier(inheritance.Member))
		if inheritance.AdminOption {
			query += "WITH ADMIN OPTION "
		}
		if inheritance.Grantor != nil {
			query += fmt.Sprintf("GRANTED BY %s",
				pq.QuoteIdentifier(*inheritance.Grantor))
		}

		contextLogger.Info("executing role inheritance query", "query", query)

		_, err := db.Exec(query)
		if err != nil {
			contextLogger.Error(err, "while importing role inheritance")
		}
	}

	return nil
}

func (rs *roleInheritanceManager) getRoleInheritance(ctx context.Context) ([]RoleInheritance, error) {
	contextLogger := log.FromContext(ctx)
	originDB, err := rs.origin.Connection(postgresDatabase)
	if err != nil {
		return nil, err
	}
	query := "SELECT ur.rolname AS roleid, " +
		"um.rolname AS member, " +
		"a.admin_option, " +
		"ug.rolname AS grantor " +
		"FROM pg_auth_members a " +
		"LEFT JOIN pg_authid ur on ur.oid = a.roleid " +
		"LEFT JOIN pg_authid um on um.oid = a.member " +
		"LEFT JOIN pg_authid ug on ug.oid = a.grantor " +
		"WHERE NOT (ur.rolname ~ '^pg_' AND um.rolname ~ '^pg_')"

	rows, err := originDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			contextLogger.Error(closeErr, "while closing rows: %w")
		}
	}()
	var ris []RoleInheritance

	for rows.Next() {
		var ri RoleInheritance
		if err := rows.Scan(
			&ri.RoleID,
			&ri.Member,
			&ri.AdminOption,
			&ri.Grantor,
		); err != nil {
			return nil, err
		}

		ris = append(ris, ri)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ris, nil
}
