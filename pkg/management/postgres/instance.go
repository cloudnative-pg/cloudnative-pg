/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/fileutils"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/management/log"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/postgres"
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

	// Connection pool pointing to the superuser database
	superUserDB *sql.DB

	// Connection pool pointing to the application database
	applicationDB *sql.DB

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
		"-o", "-c port=5432",
	}

	// Add postmaster command line options
	for _, opt := range instance.StartupOptions {
		options = append(options, "-o", "-c "+opt)
	}

	log.Log.Info("Starting up instance",
		"pgdata", instance.PgData)

	cmd := exec.Command("pg_ctl", options...) // #nosec
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
	if instance.applicationDB != nil {
		_ = instance.applicationDB.Close()
		instance.applicationDB = nil
	}

	if instance.superUserDB != nil {
		_ = instance.superUserDB.Close()
		instance.superUserDB = nil
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

	cmd := exec.Command("pg_ctl", options...) // #nosec
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

	cmd := exec.Command("postgres", options...) // #nosec
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

// GetSuperUserDB gets the connection pool pointing to this instance, possibly creating
// it if needed
func (instance *Instance) GetSuperUserDB() (*sql.DB, error) {
	if instance.superUserDB != nil {
		return instance.superUserDB, nil
	}

	db, err := sql.Open(
		"postgres",
		"postgres://postgres@localhost/postgres?sslmode=disable")
	if err != nil {
		return nil, errors.Wrap(err, "Can't create connection pool")
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(0)

	instance.superUserDB = db
	return instance.superUserDB, nil
}

// GetApplicationDB gets the connection pool pointing to this instance, possibly creating
// it if needed
func (instance *Instance) GetApplicationDB() (*sql.DB, error) {
	if instance.applicationDB != nil {
		return instance.applicationDB, nil
	}

	db, err := sql.Open(
		"postgres",
		"postgres://postgres@localhost/"+instance.ApplicationDatabase+"?sslmode=disable")
	if err != nil {
		return nil, errors.Wrap(err, "Can't create connection pool")
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(0)

	instance.applicationDB = db
	return instance.applicationDB, nil
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
	if result {
		return false, nil
	}

	result, err = fileutils.FileExists(filepath.Join(instance.PgData, "recovery.conf"))
	if err != nil {
		return false, err
	}
	if result {
		return false, nil
	}

	return true, nil
}

// Demote demote an existing PostgreSQL instance
func (instance *Instance) Demote() error {
	major, err := postgres.GetMajorVersion(instance.PgData)
	if err != nil {
		return errors.Wrap(err, "Cannot detect major version")
	}

	if major < 12 {
		return instance.createRecoveryConf()
	}

	return instance.createStandbySignal()
}

// createRecoveryConf create a recovery.conf file for PostgreSQL 11 and earlier
func (instance *Instance) createRecoveryConf() error {
	primaryConnInfo := fmt.Sprintf(
		"host=%v-rw user=postgres port=5432 dbname=%v",
		instance.ClusterName,
		"postgres")

	f, err := os.Create(filepath.Join(instance.PgData, "recovery.conf"))
	if err != nil {
		return err
	}

	defer func() {
		err = f.Close()
	}()
	_, err = f.WriteString("standby_mode = 'on'\n" +
		"primary_conninfo = " + pq.QuoteLiteral(primaryConnInfo) + "\n" +
		"recovery_target_timeline = 'latest'\n" +
		"restore_command = '/controller/manager wal-restore %f %p'\n")
	if err != nil {
		return err
	}

	return nil
}

// createStandbySignal creates a standby.signal file for PostgreSQL 12 and beyond
func (instance *Instance) createStandbySignal() error {
	emptyFile, err := os.Create(filepath.Join(instance.PgData, "standby.signal"))
	if emptyFile != nil {
		_ = emptyFile.Close()
	}

	return err
}
