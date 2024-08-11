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

package webserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// pgDatabaseRequestFormat represents a PostgreSQL Database
type pgDatabaseRequestFormat struct {
	// The owner
	Owner string `json:"owner"`

	// The encoding (cannot be changed)
	Encoding string `json:"encoding"`

	// True when the database is a template
	IsTemplate *bool `json:"isTemplate"`

	// True when connections to this database are allowed
	AllowConnections *bool `json:"allowConnections"`

	// Connection limit, -1 means no limit and -2 means the
	// database is not valid
	ConnectionLimit *int `json:"connectionLimit"`

	// The default tablespace of this database
	Tablespace string `json:"tablespace"`

	// the name of the database, this is gathered internally from the request param
	name string
}

func getPgDatabaseRequest(req *http.Request) (pgDatabaseRequestFormat, error) {
	contextLogger := log.FromContext(req.Context())
	dbname := req.PathValue("dbname")

	var database pgDatabaseRequestFormat
	if err := json.NewDecoder(req.Body).Decode(&database); err != nil {
		contextLogger.Debug("Error while decoding the database request",
			"dbname", dbname,
			"error", err.Error(),
			"url", req.URL,
			"method", req.Method,
		)
		return pgDatabaseRequestFormat{}, err
	}
	database.name = dbname
	return database, nil
}

func (ws *remoteWebserverEndpoints) pgDatabase(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodPost:
		ws.postPgDatabase(w, req)

	default:
		http.Error(w, fmt.Sprintf("method '%s' not allowed", req.Method), http.StatusMethodNotAllowed)
	}
}

func (ws *remoteWebserverEndpoints) postPgDatabase(w http.ResponseWriter, req *http.Request) {
	contextLogger := log.FromContext(req.Context())
	dbname := req.PathValue("dbname")

	pgRequest, err := getPgDatabaseRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	db, err := ws.instance.GetSuperUserDB()
	if err != nil {
		contextLogger.Debug(
			"Error while getting DB connection",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	row := db.QueryRowContext(
		req.Context(),
		`
		SELECT count(*)
		FROM pg_database
	        WHERE datname = $1
		`,
		dbname)
	if row.Err() != nil {
		contextLogger.Debug(
			"Error while getting database status",
			"dbname", dbname,
			"err", row.Err().Error())
		http.Error(w, row.Err().Error(), http.StatusInternalServerError)
		return
	}

	var count int
	if err := row.Scan(&count); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		contextLogger.Debug(
			"Error while getting database status (scanning error)",
			"dbname", dbname,
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if count > 0 {
		if err := patchDatabase(req.Context(), db, &pgRequest); err != nil {
			contextLogger.Debug(
				"Error while patching database",
				"dbname", dbname,
				"err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		return
	}

	if err := createDatabase(req.Context(), db, &pgRequest); err != nil {
		contextLogger.Debug(
			"Error while creating database",
			"dbname", dbname,
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func createDatabase(
	ctx context.Context,
	db *sql.DB,
	request *pgDatabaseRequestFormat,
) error {
	sqlCreateDatabase := fmt.Sprintf("CREATE DATABASE %s ", request.name)
	if request.IsTemplate != nil {
		sqlCreateDatabase += fmt.Sprintf(" IS_TEMPLATE %v", *request.IsTemplate)
	}
	if len(request.Owner) > 0 {
		sqlCreateDatabase += fmt.Sprintf(" OWNER %s", pgx.Identifier{request.Owner}.Sanitize())
	}
	if len(request.Tablespace) > 0 {
		sqlCreateDatabase += fmt.Sprintf(" TABLESPACE %s", pgx.Identifier{request.Tablespace}.Sanitize())
	}
	if request.AllowConnections != nil {
		sqlCreateDatabase += fmt.Sprintf(" ALLOW_CONNECTIONS %v", *request.AllowConnections)
	}
	if request.ConnectionLimit != nil {
		sqlCreateDatabase += fmt.Sprintf(" CONNECTION LIMIT %v", *request.ConnectionLimit)
	}

	if _, err := db.ExecContext(ctx, sqlCreateDatabase); err != nil {
		return err
	}

	return nil
}

func patchDatabase(
	ctx context.Context,
	db *sql.DB,
	request *pgDatabaseRequestFormat,
) error {
	if len(request.Owner) > 0 {
		changeOwnerSQL := fmt.Sprintf(
			"ALTER DATABASE %s OWNER TO %s",
			pgx.Identifier{request.name}.Sanitize(),
			pgx.Identifier{request.Owner}.Sanitize())

		if _, err := db.ExecContext(ctx, changeOwnerSQL); err != nil {
			return fmt.Errorf("alter database owner to: %w", err)
		}
	}

	if request.IsTemplate != nil {
		changeIsTemplateSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH IS_TEMPLATE %v",
			pgx.Identifier{request.name}.Sanitize(),
			*request.IsTemplate)

		if _, err := db.ExecContext(ctx, changeIsTemplateSQL); err != nil {
			return fmt.Errorf("alter database with is_template: %w", err)
		}
	}

	if request.AllowConnections != nil {
		changeAllowConnectionsSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH ALLOW_CONNECTIONS %v",
			pgx.Identifier{request.name}.Sanitize(),
			*request.AllowConnections)

		if _, err := db.ExecContext(ctx, changeAllowConnectionsSQL); err != nil {
			return fmt.Errorf("alter database with allow_connections: %w", err)
		}
	}

	if request.ConnectionLimit != nil {
		changeConnectionsLimitSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH CONNECTION LIMIT %v",
			pgx.Identifier{request.name}.Sanitize(),
			*request.ConnectionLimit)

		if _, err := db.ExecContext(ctx, changeConnectionsLimitSQL); err != nil {
			return fmt.Errorf("alter database with connection limit: %w", err)
		}
	}

	if len(request.Tablespace) > 0 {
		changeTablespaceSQL := fmt.Sprintf(
			"ALTER DATABASE %s SET TABLESPACE %s",
			pgx.Identifier{request.name}.Sanitize(),
			pgx.Identifier{request.Tablespace}.Sanitize())

		if _, err := db.ExecContext(ctx, changeTablespaceSQL); err != nil {
			return fmt.Errorf("alter database set tablespace: %w", err)
		}
	}

	return nil
}
