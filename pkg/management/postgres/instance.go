/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/pkg/errors"

	"github.com/2ndquadrant/cloud-native-postgresql/pkg/fileutils"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/management/log"
)

// Instance represent a PostgreSQL instance to be executed
// in the current environment
type Instance struct {
	// The data directory
	PgData string

	// Application database name
	ApplicationDatabase string

	// Command line options to pass to the postmaster, see the
	// '-c' option of pg_ctl for an useful example
	StartupOptions []string

	// Port where the instance will listen
	Port int

	// Connection pool pointing to the superuser database
	superUserDb *sql.DB

	// Connection pool pointing to the application database
	applicationDb *sql.DB

	// The namespace of the k8s object representing this cluster
	Namespace string

	// The name of the Pod where the controller is executing
	PodName string

	// The name of the cluster of which this Pod is belonging
	ClusterName string
}

// Startup starts up a PostgreSQL instance and wait for the instance to be
// started
func (instance Instance) Startup() error {
	options := []string{
		"start",
		"-w",
		"-D", instance.PgData,
		"-o", "-c port=" + strconv.Itoa(instance.Port),
	}

	// Add postmaster command line options
	for _, opt := range instance.StartupOptions {
		options = append(options, "-o", "-c "+opt)
	}

	log.Log.Info("Starting up instance",
		"pgdata", instance.PgData)

	cmd := exec.Command("pg_ctl", options...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return errors.Wrap(err, "Error starting PostgreSQL instance")
	}

	return nil
}

// ShutdownConnections tears down database connections
func (instance *Instance) ShutdownConnections() {
	if instance.applicationDb != nil {
		_ = instance.applicationDb.Close()
		instance.applicationDb = nil
	}

	if instance.superUserDb != nil {
		_ = instance.superUserDb.Close()
		instance.superUserDb = nil
	}
}

// Shutdown shuts down a PostgreSQL instance which was previously started
// with Startup
func (instance *Instance) Shutdown() error {
	instance.ShutdownConnections()

	options := []string{
		"-D",
		instance.PgData,
		"-m",
		"fast",
		"-w",
		"stop",
	}

	log.Log.Info("Shutting down instance",
		"pgdata", instance.PgData)

	cmd := exec.Command("pg_ctl", options...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return errors.Wrap(err, "Error stopping PostgreSQL instance")
	}

	return nil
}

// Run this instance returning an exec.Cmd structure
// to control the instance execution
func (instance Instance) Run() (*exec.Cmd, error) {
	options := []string{
		"-D", instance.PgData,
	}

	cmd := exec.Command("postgres", options...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

// WithActiveInstance execute the internal function while this
// PostgreSQL instance is running
func (instance Instance) WithActiveInstance(inner func() error) error {
	err := instance.Startup()
	if err != nil {
		return errors.Wrap(err, "Error while activating instance")
	}
	defer func() {
		if err := instance.Shutdown(); err != nil {
			log.Log.Error(err, "Error while deactivating instance")
		}
	}()

	return inner()
}

// GetSuperuserDB gets the connection pool pointing to this instance, possibly creating
// it if needed
func (instance *Instance) GetSuperuserDB() (*sql.DB, error) {
	if instance.superUserDb != nil {
		return instance.superUserDb, nil
	}

	db, err := sql.Open(
		"postgres",
		"postgres://postgres@localhost/postgres?sslmode=disable")
	if err != nil {
		return nil, errors.Wrap(err, "Can't create connection pool")
	}

	instance.superUserDb = db
	return instance.superUserDb, nil
}

// GetApplicationDB gets the connection pool pointing to this instance, possibly creating
// it if needed
func (instance *Instance) GetApplicationDB() (*sql.DB, error) {
	if instance.applicationDb != nil {
		return instance.applicationDb, nil
	}

	db, err := sql.Open(
		"postgres",
		"postgres://postgres@localhost/"+instance.ApplicationDatabase+"?sslmode=disable")
	if err != nil {
		return nil, errors.Wrap(err, "Can't create connection pool")
	}

	instance.applicationDb = db
	return instance.applicationDb, nil
}

// IsPrimary check if the data directory belongs to a primary server or to a
// secondary one by looking for a "standby.signal" file inside the data
// directory. IMPORTANT: this method also works when the instance is not
// started up
func (instance *Instance) IsPrimary() (bool, error) {
	result, err := fileutils.FileExists(filepath.Join(instance.PgData, "standby.signal"))
	if err != nil {
		return false, err
	}

	return !result, nil
}
