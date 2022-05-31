package logicalsnapshot

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"k8s.io/utils/strings/slices"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

type roleManager struct {
	origin      *pool.ConnectionPool
	destination *pool.ConnectionPool
	cluster     *apiv1.Cluster
}

// Role a result from pg_authid table
type Role struct {
	Oid            string  `json:"oid,omitempty"`
	Rolname        string  `json:"rolname,omitempty"`
	Rolsuper       bool    `json:"rolsuper,omitempty"`
	Rolinherit     bool    `json:"rolinherit,omitempty"`
	Rolcreaterole  bool    `json:"rolcreaterole,omitempty"`
	Rolcreatedb    bool    `json:"rolcreatedb,omitempty"`
	Rolcanlogin    bool    `json:"rolcanlogin,omitempty"`
	Rolreplication bool    `json:"rolreplication,omitempty"`
	Rolbypassrls   bool    `json:"rolbypassrls,omitempty"`
	Rolconnlimit   int     `json:"rolconnlimit,omitempty"`
	Rolpassword    *string `json:"rolpassword,omitempty"`
	Rolvaliduntil  *string `json:"rolvaliduntil,omitempty"`
	RolComment     *string `json:"rolcomment,omitempty"`
	IsCurrentUser  bool    `json:"is_current_user,omitempty"`
}

func cloneRoles(
	ctx context.Context,
	cluster *apiv1.Cluster,
	destination *pool.ConnectionPool,
	origin *pool.ConnectionPool,
) error {
	rs := roleManager{origin: origin, destination: destination, cluster: cluster}
	roles, err := rs.getRoles(ctx, true)
	if err != nil {
		return err
	}

	if err := rs.importRoles(ctx, roles); err != nil {
		return err
	}

	return nil
}

func (rs *roleManager) importRoles(ctx context.Context, roles []Role) error {
	contextLogger := log.FromContext(ctx)

	db, err := rs.destination.Connection(postgresDatabase)
	if err != nil {
		return err
	}
	for _, role := range roles {
		query := rs.createSQLStatement(role)
		contextLogger.Info("executing import role query", "query", query)
		_, err := db.Exec(query)
		if err != nil {
			contextLogger.Error(err, "error while importing the role")

			continue
		}
	}
	return nil
}

func (rs *roleManager) createSQLStatement(role Role) string {
	query := fmt.Sprintf("CREATE ROLE %s WITH ", pq.QuoteIdentifier(role.Rolname))

	if role.Rolcreatedb {
		query += "CREATEDB "
	}

	if role.Rolsuper {
		query += "SUPERUSER "
	}

	if role.Rolcanlogin {
		query += "LOGIN "
	} else {
		query += "NOLOGIN "
	}

	if role.Rolinherit {
		query += "INHERIT "
	}

	if role.Rolcreaterole {
		query += "CREATEROLE "
	}

	if role.Rolbypassrls {
		query += "BYPASSRLS "
	}

	if role.Rolvaliduntil != nil {
		query += fmt.Sprintf("VALID UNTIL %s ", pq.QuoteLiteral(*role.Rolvaliduntil))
	}

	if role.Rolpassword != nil {
		query += fmt.Sprintf("PASSWORD %s ", pq.QuoteLiteral(*role.Rolpassword))
	}
	query += fmt.Sprintf("CONNECTION LIMIT %d", role.Rolconnlimit)

	return query
}

func (rs *roleManager) getRoles(ctx context.Context, downgradeSuperUser bool) ([]Role, error) {
	contextLogger := log.FromContext(ctx)
	originDatabase, err := rs.origin.Connection(postgresDatabase)
	if err != nil {
		return nil, err
	}
	vers, err := postgres.GetPgVersion(originDatabase)
	if err != nil {
		return nil, err
	}
	contextLogger.Info("postgres version extracted", "version", vers.String())

	var rows *sql.Rows
	var query string

	//nolint:gocritic
	if vers.Major > 9 || (vers.Major == 9 && vers.Minor >= 6) {
		query = "SELECT oid, rolname, rolsuper, rolinherit, " +
			"rolcreaterole, rolcreatedb, " +
			"rolcanlogin, rolconnlimit, rolpassword, " +
			"rolvaliduntil, rolreplication, rolbypassrls, " +
			"pg_catalog.shobj_description(oid, 'pg_authid') as rolcomment, " +
			"rolname = current_user AS is_current_user " +
			"FROM pg_authid " +
			"WHERE rolname !~ '^pg_' " +
			"ORDER BY 2"
	} else if vers.Major == 9 && vers.Minor == 5 {
		query = "SELECT oid, rolname, rolsuper, rolinherit, " +
			"rolcreaterole, rolcreatedb, " +
			"rolcanlogin, rolconnlimit, rolpassword, " +
			"rolvaliduntil, rolreplication, rolbypassrls, " +
			"pg_catalog.shobj_description(oid, 'pg_authid') as rolcomment, " +
			"rolname = current_user AS is_current_user " +
			"FROM pg_authid " +
			"ORDER BY 2"
	} else {
		query = "SELECT oid, rolname, rolsuper, rolinherit, " +
			"rolcreaterole, rolcreatedb, " +
			"rolcanlogin, rolconnlimit, rolpassword, " +
			"rolvaliduntil, rolreplication, " +
			"false as rolbypassrls, " +
			"pg_catalog.shobj_description(oid, 'pg_authid') as rolcomment, " +
			"rolname = current_user AS is_current_user " +
			"FROM pg_authid " +
			"ORDER BY 2"
	}

	contextLogger.Debug("executing role snapshot query", "query", query)
	rows, err = originDatabase.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			contextLogger.Error(closeErr, "while closing rows: %w")
		}
	}()

	rolesToImport := rs.cluster.Spec.Bootstrap.InitDB.Import.Roles
	rolesToSkip := []string{
		"postgres",
		"streaming_replica",
		"cnp_pooler_pgbouncer",
		rs.cluster.Spec.Bootstrap.InitDB.Owner,
	}

	var roles []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(
			&r.Oid,
			&r.Rolname,
			&r.Rolsuper,
			&r.Rolinherit,
			&r.Rolcreaterole,
			&r.Rolcreatedb,
			&r.Rolcanlogin,
			&r.Rolconnlimit,
			&r.Rolpassword,
			&r.Rolvaliduntil,
			&r.Rolreplication,
			&r.Rolbypassrls,
			&r.RolComment,
			&r.IsCurrentUser,
		); err != nil {
			return nil, err
		}

		normalizedRoleName := strings.ToLower(r.Rolname)
		if slices.Contains(rolesToSkip, normalizedRoleName) {
			contextLogger.Info(
				"found a role that needs to be skipped",
				"rolesToSkip", rolesToSkip,
				"role", r,
			)
			continue
		}

		if !slices.Contains(rolesToImport, normalizedRoleName) && !slices.Contains(rolesToImport, "*") {
			contextLogger.Info(
				"found a role that doesn't need to be imported",
				"rolesToImport", rolesToImport,
				"role", r,
			)
			continue
		}

		if downgradeSuperUser && r.Rolsuper {
			contextLogger.Debug(
				"found a superUser, downgrading permissions",
				"role", r,
			)
			r.Rolsuper = false
		}

		roles = append(roles, r)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return roles, nil
}
