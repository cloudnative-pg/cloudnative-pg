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
	"time"

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
}

func (ws *remoteWebserverEndpoints) pgDatabase(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodPut:
		ws.putPgDatabase(w, req)

	case http.MethodDelete:
		ws.deletePgDatabase(w, req)

	case http.MethodGet:
		ws.getPgDatabase(w, req)

	case http.MethodPatch:
		ws.patchPgDatabase(w, req)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ws *remoteWebserverEndpoints) getPgDatabase(w http.ResponseWriter, req *http.Request) {
	dbname := req.PathValue("dbname")

	db, err := ws.instance.GetSuperUserDB()
	if err != nil {
		log.Debug(
			"Error while getting DB connection",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	row := db.QueryRowContext(
		req.Context(),
		`
		SELECT
			coalesce(pg_authid.rolname, '') as rolname,
			coalesce(pg_encoding_to_char(encoding), '') as encoding,
			datistemplate,
			datallowconn,
			datconnlimit,
			coalesce(pg_tablespace.spcname, '') as spcname
		FROM pg_database
		LEFT JOIN pg_authid ON pg_authid.oid = pg_database.datdba
		LEFT JOIN pg_tablespace ON pg_tablespace.oid = pg_database.dattablespace
		WHERE datname = $1
		`,
		dbname)
	if row.Err() != nil {
		log.Debug(
			"Error while getting database status",
			"dbname", dbname,
			"err", row.Err().Error())
		http.Error(w, row.Err().Error(), http.StatusInternalServerError)
		return
	}

	var result pgDatabaseRequestFormat
	if err := row.Scan(
		&result.Owner,
		&result.Encoding,
		&result.IsTemplate,
		&result.AllowConnections,
		&result.ConnectionLimit,
		&result.Tablespace,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		log.Debug(
			"Error while getting database status (scanning error)",
			"dbname", dbname,
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Debug(
			"Error while marshalling database status",
			"dbname", dbname,
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ws *remoteWebserverEndpoints) deletePgDatabase(w http.ResponseWriter, req *http.Request) {
	dbname := req.PathValue("dbname")

	db, err := ws.instance.GetSuperUserDB()
	if err != nil {
		log.Debug(
			"Error while getting DB connection",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Should we prevent our users from deleting an empty database?
	// It sounds like a good idea, but the implementation is
	// surprisingly difficult because it is surprising difficult
	// to understand what an empty database really is.
	//
	// An empty database is not really empty, as new databases
	// are created from a template that is not empty (system catalog).
	// To add even more complexity, the template database can change
	// over time.
	//
	// Perhaps a good approach would be disallowing to delete
	// a database.

	// The DROP DATABASE statement will be stuck if there are
	// active connections on the target database. Let's give it
	// a deadline.
	ctx, cancelFunc := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancelFunc()

	if _, err := db.ExecContext(
		ctx,
		fmt.Sprintf("DROP DATABASE %s", pgx.Identifier{dbname}.Sanitize()),
	); err != nil {
		log.Debug(
			"Error while deleting database",
			"dbname", dbname,
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (ws *remoteWebserverEndpoints) patchPgDatabase(w http.ResponseWriter, req *http.Request) {
	dbname := req.PathValue("dbname")

	var database pgDatabaseRequestFormat
	if err := json.NewDecoder(req.Body).Decode(&database); err != nil {
		log.Debug("Error while decoding patch database request",
			"dbname", dbname,
			"error", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	db, err := ws.instance.GetSuperUserDB()
	if err != nil {
		log.Debug(
			"Error while getting DB connection",
			"dbname", dbname,
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(database.Owner) > 0 {
		changeOwnerSQL := fmt.Sprintf(
			"ALTER DATABASE %s OWNER TO %s",
			pgx.Identifier{dbname}.Sanitize(),
			pgx.Identifier{database.Owner}.Sanitize())

		if _, err := db.ExecContext(req.Context(), changeOwnerSQL); err != nil {
			log.Debug(
				"Error while changing database ownership",
				"dbname", dbname,
				"err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if database.IsTemplate != nil {
		changeIsTemplateSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH IS_TEMPLATE %v",
			pgx.Identifier{dbname}.Sanitize(),
			*database.IsTemplate)

		if _, err := db.ExecContext(req.Context(), changeIsTemplateSQL); err != nil {
			log.Debug(
				"Error while changing database IS_TEMPLATE option",
				"dbname", dbname,
				"err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if database.AllowConnections != nil {
		changeAllowConnectionsSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH ALLOW_CONNECTIONS %v",
			pgx.Identifier{dbname}.Sanitize(),
			*database.AllowConnections)

		if _, err := db.ExecContext(req.Context(), changeAllowConnectionsSQL); err != nil {
			log.Debug(
				"Error while changing database ALLOW_CONNECTIONS option",
				"dbname", dbname,
				"err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if database.ConnectionLimit != nil {
		changeConnectionsLimitSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH CONNECTION LIMIT %v",
			pgx.Identifier{dbname}.Sanitize(),
			*database.ConnectionLimit)

		if _, err := db.ExecContext(req.Context(), changeConnectionsLimitSQL); err != nil {
			log.Debug(
				"Error while changing database CONNECTION LIMIT option",
				"dbname", dbname,
				"err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if len(database.Tablespace) > 0 {
		changeTablespaceSQL := fmt.Sprintf(
			"ALTER DATABASE %s SET TABLESPACE %s",
			pgx.Identifier{dbname}.Sanitize(),
			pgx.Identifier{database.Tablespace}.Sanitize())

		if _, err := db.ExecContext(req.Context(), changeTablespaceSQL); err != nil {
			log.Debug(
				"Error while changing database default tablespace",
				"dbname", dbname,
				"err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (ws *remoteWebserverEndpoints) putPgDatabase(w http.ResponseWriter, req *http.Request) {
	dbname := req.PathValue("dbname")

	var database pgDatabaseRequestFormat
	if err := json.NewDecoder(req.Body).Decode(&database); err != nil {
		log.Debug("Error while decoding patch database request",
			"dbname", dbname,
			"error", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	db, err := ws.instance.GetSuperUserDB()
	if err != nil {
		log.Debug(
			"Error while getting DB connection",
			"dbname", dbname,
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sqlCreateDatabase := fmt.Sprintf("CREATE DATABASE %s ", dbname)
	if database.IsTemplate != nil {
		sqlCreateDatabase += fmt.Sprintf(" IS_TEMPLATE %v", *database.IsTemplate)
	}
	if len(database.Owner) > 0 {
		sqlCreateDatabase += fmt.Sprintf(" OWNER %s", pgx.Identifier{database.Owner}.Sanitize())
	}
	if len(database.Tablespace) > 0 {
		sqlCreateDatabase += fmt.Sprintf(" TABLESPACE %s", pgx.Identifier{database.Tablespace}.Sanitize())
	}
	if database.AllowConnections != nil {
		sqlCreateDatabase += fmt.Sprintf(" ALLOW_CONNECTIONS %v", *database.AllowConnections)
	}
	if database.ConnectionLimit != nil {
		sqlCreateDatabase += fmt.Sprintf(" CONNECTION LIMIT %v", *database.ConnectionLimit)
	}

	if _, err := db.ExecContext(req.Context(), sqlCreateDatabase); err != nil {
		log.Debug(
			"Error while creating database",
			"dbname", dbname,
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
