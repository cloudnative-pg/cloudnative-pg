/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import (
	"database/sql"
	"fmt"
	"os/exec"
	"path"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/2ndquadrant/cloud-native-postgresql/pkg/fileutils"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/management/log"
)

// InitInfo contains all the info needed to bootstrap a new PostgreSQL:O
// instance
type InitInfo struct {
	// The data directory where to generate the new cluster
	PgData string

	// The name of the file containing the superuser password
	PasswordFile string

	// The name of the database to be generated for the applications
	ApplicationDatabase string

	// The name of the role to be generated for the applications
	ApplicationUser string

	// The password of the role to be generated for the applications
	ApplicationPasswordFile string

	// The HBA rules to add to the cluster
	HBARulesFile string

	// The configuration to append to the one PostgreSQL already produces
	PostgreSQLConfigFile string
}

// VerifyConfiguration verify if the passed configuration is OK and returns an error otherwise
func (info InitInfo) VerifyConfiguration() error {
	passwordFileExists, err := fileutils.FileExists(info.PasswordFile)
	if err != nil {
		return err
	}
	if !passwordFileExists {
		return fmt.Errorf("superuser password file doesn't exist (%v)", info.PasswordFile)
	}

	applicationPasswordFileExists, err := fileutils.FileExists(info.ApplicationPasswordFile)
	if err != nil {
		return err
	}
	if !applicationPasswordFileExists {
		return fmt.Errorf("application user's password file doesn't exist (%v)", info.PasswordFile)
	}

	pgdataExists, err := fileutils.FileExists(info.PgData)
	if err != nil {
		return err
	}
	if pgdataExists {
		return fmt.Errorf("PGData directories already exist")
	}

	if len(info.HBARulesFile) != 0 {
		hbaRulesFileExists, err := fileutils.FileExists(info.HBARulesFile)
		if err != nil {
			return err
		}
		if !hbaRulesFileExists {
			return fmt.Errorf("hba rules file doesn't exist (%v)", info.HBARulesFile)
		}
	}

	if len(info.PostgreSQLConfigFile) != 0 {
		postgresConfigFileExists, err := fileutils.FileExists(info.PostgreSQLConfigFile)
		if err != nil {
			return err
		}
		if !postgresConfigFileExists {
			return fmt.Errorf("postgresql config file doesn't exist (%v)", info.HBARulesFile)
		}
	}

	if len(info.ApplicationUser) == 0 {
		return fmt.Errorf("the name of the application user is empty")
	}

	if len(info.ApplicationDatabase) == 0 {
		return fmt.Errorf("the name of the application database is empty")
	}

	return nil
}

// CreateDataDirectory create a new data directory given the configuration
func (info InitInfo) CreateDataDirectory() error {
	log.Log.Info("Creating new data directory",
		"pgdata", info.PgData)

	// Invoke initdb to generate a data directory
	options := []string{
		"--username",
		"postgres",
		"--pwfile",
		info.PasswordFile,
		"-D",
		info.PgData,
	}

	cmd := exec.Command("initdb", options...)
	stdOutErr, err := cmd.CombinedOutput()
	if err != nil {
		log.Log.Info("initdb output", "output", stdOutErr)
		return errors.Wrap(err, "Error while creating the PostgreSQL instance")
	}

	// Add HBA info and PostgreSQL configuration
	if len(info.HBARulesFile) != 0 {
		err = fileutils.AppendFile(
			path.Join(info.PgData, "pg_hba.conf"),
			info.HBARulesFile)
		if err != nil {
			return errors.Wrap(err, "appending to pg_hba.conf file resulted in an error")
		}
	}

	if len(info.PostgreSQLConfigFile) != 0 {
		err = fileutils.AppendFile(
			path.Join(info.PgData, "postgresql.conf"),
			info.PostgreSQLConfigFile)
		if err != nil {
			return errors.Wrap(err, "appending to postgresql.conf file resulted in an error")
		}
	}

	return nil
}

// GetInstance gets the PostgreSQL instance which correspond to these init information
func (info InitInfo) GetInstance() Instance {
	postgresInstance := Instance{
		PgData:              info.PgData,
		StartupOptions:      []string{"listen_addresses='127.0.0.1'"},
		Port:                5432,
		ApplicationDatabase: info.ApplicationDatabase,
	}
	return postgresInstance
}

// ConfigureApplicationEnvironment creates the environment for an
// application to run against this PostgreSQL instance given a connection pool
func (info InitInfo) ConfigureApplicationEnvironment(db *sql.DB) error {
	_, err := db.Exec(fmt.Sprintf(
		"CREATE USER %v",
		pq.QuoteIdentifier(info.ApplicationUser)))
	if err != nil {
		return err
	}

	_, err = db.Exec(fmt.Sprintf(
		"ALTER USER %v PASSWORD %v",
		pq.QuoteIdentifier(info.ApplicationUser),
		pq.QuoteLiteral(info.ApplicationPasswordFile)))
	if err != nil {
		return err
	}

	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE %v OWNER %v",
		pq.QuoteIdentifier(info.ApplicationDatabase),
		pq.QuoteIdentifier(info.ApplicationUser)))
	if err != nil {
		return err
	}

	return nil
}

// Bootstrap create and configure this new PostgreSQL instance
func (info InitInfo) Bootstrap() error {
	err := info.CreateDataDirectory()
	if err != nil {
		return err
	}

	instance := info.GetInstance()
	return instance.WithActiveInstance(func() error {
		db, err := instance.GetSuperuserDB()
		if err != nil {
			return nil
		}

		return info.ConfigureApplicationEnvironment(db)
	})
}
