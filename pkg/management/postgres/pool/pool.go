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

// Package pool contain an implementation of a connection pool to multiple
// database pointing to the same instance
package pool

import (
	"database/sql"
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	// this is needed to correctly open the sql connection with the pgx driver
	_ "github.com/jackc/pgx/v4/stdlib"
)

// ConnectionPool is a repository of DB connections, pointing to the same instance
// given a base DSN without the "dbname" parameter
type ConnectionPool struct {
	// This is the base connection string (without the "dbname" parameter)
	baseConnectionString string

	// A map of connection for every used database
	connectionMap map[string]*sql.DB
}

// NewConnectionPool creates a new connectionMap of connections given
// the base connection string
func NewConnectionPool(baseConnectionString string) *ConnectionPool {
	return &ConnectionPool{
		baseConnectionString: baseConnectionString,
		connectionMap:        make(map[string]*sql.DB),
	}
}

// Connection gets the connection for the given database
func (pool *ConnectionPool) Connection(dbname string) (*sql.DB, error) {
	if result, ok := pool.connectionMap[dbname]; ok {
		return result, nil
	}

	connection, err := pool.newConnection(dbname)
	if err != nil {
		return nil, err
	}

	pool.connectionMap[dbname] = connection
	return connection, nil
}

// ShutdownConnections closes every database connection
func (pool *ConnectionPool) ShutdownConnections() {
	for _, db := range pool.connectionMap {
		_ = db.Close()
	}

	pool.connectionMap = make(map[string]*sql.DB)
}

// newConnection creates a database connection connectionMap, connecting via
// Unix domain socket to a database with a certain name
func (pool *ConnectionPool) newConnection(dbname string) (*sql.DB, error) {
	dsn := pool.GetDsn(dbname)
	db, err := utils.NewSimpleDBConnection(dsn)
	if err != nil {
		return nil, fmt.Errorf("cannot create connection connectionMap: %w", err)
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(0)

	return db, nil
}

// GetDsn returns the connection string for a given database
func (pool *ConnectionPool) GetDsn(dbname string) string {
	return fmt.Sprintf("%s dbname=%s", pool.baseConnectionString, dbname)
}
