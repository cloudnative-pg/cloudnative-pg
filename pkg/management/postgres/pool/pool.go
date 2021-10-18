/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package pool contain an implementation of a connection pool to multiple
// database pointing to the same instance
package pool

import (
	"database/sql"
	"fmt"
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
	dsn := fmt.Sprintf(
		"%s dbname=%s", pool.baseConnectionString, dbname)
	db, err := sql.Open(
		"postgres",
		dsn)
	if err != nil {
		return nil, fmt.Errorf("cannot create connection connectionMap: %w", err)
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(0)

	return db, nil
}
