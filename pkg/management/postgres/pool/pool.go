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

// Package pool contain an implementation of a connection pool to multiple
// database pointing to the same instance
package pool

import (
	"database/sql"
	"fmt"
	"sync"

	// this is needed to correctly open the sql connection with the pgx driver
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Pooler represents an interface for a connection pooler.
// It exposes functionalities for retrieving a connection, obtaining the Data Source Name (DSN),
// and shutting down all active connections.
type Pooler interface {
	// Connection gets the connection for the given database
	Connection(dbname string) (*sql.DB, error)
	// GetDsn returns the connection string for a given database
	GetDsn(dbname string) string
	// ShutdownConnections closes every database connection
	ShutdownConnections()
}

// ConnectionPool is a repository of DB connections, pointing to the same instance
// given a base DSN without the "dbname" parameter
type ConnectionPool struct {
	// This is the base connection string (without the "dbname" parameter)
	baseConnectionString string

	// The configuration to be used
	connectionProfile ConnectionProfile

	// A map of connection for every used database
	connectionMap      map[string]*sql.DB
	connectionMapMutex sync.Mutex
}

// NewPostgresqlConnectionPool creates a new connectionMap of connections given
// the base connection string, targeting a PostgreSQL server
func NewPostgresqlConnectionPool(baseConnectionString string) *ConnectionPool {
	return newConnectionPool(baseConnectionString, ConnectionProfilePostgresql)
}

// NewPgbouncerConnectionPool creates a new connectionMap of connections given
// the base connection string
func NewPgbouncerConnectionPool(baseConnectionString string) *ConnectionPool {
	return newConnectionPool(baseConnectionString, ConnectionProfilePgbouncer)
}

// newConnectionPool creates a new connectionMap of connections given
// the base connection string
func newConnectionPool(baseConnectionString string, connectionProfile ConnectionProfile) *ConnectionPool {
	return &ConnectionPool{
		baseConnectionString: baseConnectionString,
		connectionMap:        make(map[string]*sql.DB),
		connectionProfile:    connectionProfile,
	}
}

// Connection gets the connection for the given database
func (pool *ConnectionPool) Connection(dbname string) (*sql.DB, error) {
	pool.connectionMapMutex.Lock()
	defer pool.connectionMapMutex.Unlock()
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
	pool.connectionMapMutex.Lock()
	defer pool.connectionMapMutex.Unlock()

	for _, db := range pool.connectionMap {
		_ = db.Close()
	}

	pool.connectionMap = make(map[string]*sql.DB)
}

// newConnection creates a database connection, connecting via
// Unix domain socket to a database with a certain name
func (pool *ConnectionPool) newConnection(dbname string) (*sql.DB, error) {
	dsn := pool.GetDsn(dbname)
	db, err := NewDBConnection(dsn, pool.connectionProfile)
	if err != nil {
		return nil, fmt.Errorf("cannot create connection connectionMap: %w", err)
	}

	// This is the list of long-running processes of the instance manager
	// that need a PostgreSQL connection:
	//
	// * Declarative Role Management
	// * Probes
	// * Replication slots reconciler
	// * Online VolumeSnapshot backup connection
	//
	// The latter will use an exclusive connection, that is required
	// for the PostgreSQL Physical backup APIs

	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(0)

	return db, nil
}

// GetDsn returns the connection string for a given database
func (pool *ConnectionPool) GetDsn(dbname string) string {
	return fmt.Sprintf("%s dbname=%s", pool.baseConnectionString, dbname)
}
