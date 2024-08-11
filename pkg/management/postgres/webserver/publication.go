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
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/jackc/pgx/v5"
)

type pgPublicationRequestFormat struct {
	// The owner
	Owner string `json:"owner,omitempty"`

	// Parameters
	Parameters map[string]string `json:"parameters,omitempty"`

	// Publication target
	Target pgPublicationTarget `json:"target,omitempty"`
}

func (obj pgPublicationRequestFormat) ToCreateSQL(name string) string {
	result := fmt.Sprintf(
		"CREATE PUBLICATION %s %s",
		pgx.Identifier{name}.Sanitize(),
		obj.Target.ToPublicationTargetSQL(),
	)

	if len(obj.Parameters) > 0 {
		result = fmt.Sprintf("%s WITH (%s)", result, obj.Parameters)
	}

	return result
}

func (obj pgPublicationRequestFormat) ToAlterSQL(name string) []string {
	result := make([]string, 0, 2)
	result = append(result,
		fmt.Sprintf(
			"ALTER PUBLICATION %s SET %s",
			pgx.Identifier{name}.Sanitize(),
			obj.Target.ToPublicationTargetSQL(),
		),
	)

	if len(obj.Parameters) > 0 {
		result = append(result,
			fmt.Sprintf(
				"ALTER PUBLICATION %s SET (%s)",
				result,
				obj.Parameters,
			),
		)
	}

	return result
}

type pgPublicationTarget struct {
	// All tables should be publicated
	AllTables *pgPublicationTargetAllTables `json:"allTables,omitempty"`

	// Just the following schema objects
	Objects []pgPublicationTargetObject `json:"objects,omitempty"`
}

type pgPublicationTargetAllTables struct {
}

func (obj pgPublicationTarget) ToPublicationTargetSQL() string {
	if obj.AllTables != nil {
		return "FOR ALL TABLES"
	}

	result := ""
	for _, object := range obj.Objects {
		if len(result) > 0 {
			result += ", "
		}
		result += object.ToPublicationObjectSQL()
	}

	if len(result) > 0 {
		result = fmt.Sprintf("FOR %s", result)
	}
	return result
}

// pgPublicationObject is an object to publicate
type pgPublicationTargetObject struct {
	// The schema to publicate
	Schema string `json:"schema,omitempty"`

	// A list of table expressions
	TableExpression []string `json:"tableExpression,omitempty"`
}

func (obj pgPublicationTargetObject) ToPublicationObjectSQL() string {
	if len(obj.Schema) > 0 {
		return fmt.Sprintf("TABLES IN SCHEMA %s", pgx.Identifier{obj.Schema}.Sanitize())
	}

	return fmt.Sprintf("TABLE %s", strings.Join(obj.TableExpression, ", "))
}

func (ws *remoteWebserverEndpoints) pgPublication(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodPost:
		ws.postPgPublication(w, req)

	default:
		http.Error(w, fmt.Sprintf("method '%s' not allowed", req.Method), http.StatusMethodNotAllowed)
	}
}

func getPgPublicationRequest(req *http.Request) (pgPublicationRequestFormat, error) {
	contextLogger := log.FromContext(req.Context())
	dbname := req.PathValue("dbname")
	pubname := req.PathValue("pubname")

	var publication pgPublicationRequestFormat
	if err := json.NewDecoder(req.Body).Decode(&publication); err != nil {
		contextLogger.Debug("Error while decoding the publication request",
			"dbname", dbname,
			"pubname", pubname,
			"error", err.Error(),
			"url", req.URL,
			"method", req.Method,
		)
		return pgPublicationRequestFormat{}, err
	}
	return publication, nil
}

func (ws *remoteWebserverEndpoints) postPgPublication(w http.ResponseWriter, req *http.Request) {
	dbname := req.PathValue("dbname")
	pubname := req.PathValue("pubname")
	contextLogger := log.FromContext(req.Context()).WithValues(
		"dbname", dbname,
		"pubname", pubname,
	)

	pgPublicationRequest, err := getPgPublicationRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	db, err := ws.instance.ConnectionPool().Connection(dbname)
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
		FROM pg_publication
	        WHERE pubname = $1
		`,
		pubname)
	if row.Err() != nil {
		contextLogger.Debug(
			"Error while getting publication status",
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
			"Error while getting publication status (scanning error)",
			"dbname", dbname,
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if count > 0 {
		if err := patchPublication(req.Context(), db, pubname, &pgPublicationRequest); err != nil {
			contextLogger.Debug(
				"Error while patching publication",
				"dbname", dbname,
				"err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		return
	}

	if err := createPublication(req.Context(), db, pubname, &pgPublicationRequest); err != nil {
		contextLogger.Debug(
			"Error while creating publication",
			"dbname", dbname,
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func patchPublication(
	ctx context.Context,
	db *sql.DB,
	name string,
	request *pgPublicationRequestFormat,
) error {
	sqls := request.ToAlterSQL(name)
	for _, sql := range sqls {	
	if _, err := db.ExecContext(ctx, sql); err != nil {
			return err
		}
	}

	return nil
}

func createPublication(
	ctx context.Context,
	db *sql.DB,
	name string,
	request *pgPublicationRequestFormat,
) error {
	sql := request.ToCreateSQL(name)
	if _, err := db.ExecContext(ctx, sql); err != nil {
		return err
	}

	return nil
}
