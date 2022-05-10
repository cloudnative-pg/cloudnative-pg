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

// Package postgres contains the function about starting up,
// shutting down and managing a PostgreSQL instance. This functions
// are primarily used by PGK
package postgres

import (
	"database/sql"
	"fmt"
	"os/exec"
	"path"

	"github.com/lib/pq"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/execlog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// InitInfo contains all the info needed to bootstrap a new PostgreSQL instance
type InitInfo struct {
	// The data directory where to generate the new cluster
	PgData string

	// The name of the database to be generated for the applications
	ApplicationDatabase string

	// The name of the role to be generated for the applications
	ApplicationUser string

	// The parent node, used to fill primary_conninfo
	ParentNode string

	// The current node, used to fill application_name
	PodName string

	// The cluster name to assign to
	ClusterName string

	// The namespace where the cluster will be installed
	Namespace string

	// The list options that should be passed to initdb to
	// create the cluster
	InitDBOptions []string

	// The list of queries to be executed just after having
	// configured a new instance
	PostInitSQL []string

	// The list of queries to be executed just after having
	// the application database created
	PostInitApplicationSQL []string

	// The list of queries to be executed inside the template1
	// database just after having configured a new instance
	PostInitTemplateSQL []string

	// Whether it is a temporary instance that will never contain real data.
	Temporary bool
}

// VerifyPGData verifies if the passed configuration is OK, otherwise it returns an error
func (info InitInfo) VerifyPGData() error {
	pgdataExists, err := fileutils.FileExists(info.PgData)
	if err != nil {
		log.Error(err, "Error while checking for an existing PGData")
		return err
	}
	if pgdataExists {
		log.Info("PGData already exists, can't overwrite")
		return fmt.Errorf("PGData directories already exist")
	}

	return nil
}

// CreateDataDirectory creates a new data directory given the configuration
func (info InitInfo) CreateDataDirectory() error {
	// Invoke initdb to generate a data directory
	options := []string{
		"--username",
		"postgres",
		"-D",
		info.PgData,
	}

	// If temporary instance disable fsync on creation
	if info.Temporary {
		options = append(options, "--no-sync")
	}

	// Add custom initdb options from the user
	options = append(options, info.InitDBOptions...)

	log.Info("Creating new data directory",
		"pgdata", info.PgData,
		"initDbOptions", options)

	initdbCmd := exec.Command(constants.InitdbName, options...) // #nosec
	err := execlog.RunBuffering(initdbCmd, constants.InitdbName)
	if err != nil {
		return fmt.Errorf("error while creating the PostgreSQL instance: %w", err)
	}

	// Always read the custom configuration file created by the operator
	postgresConfTrailer := fmt.Sprintf("# load CloudNativePG custom configuration\n"+
		"include '%v'\n",
		constants.PostgresqlCustomConfigurationFile)
	err = fileutils.AppendStringToFile(
		path.Join(info.PgData, "postgresql.conf"),
		postgresConfTrailer,
	)
	if err != nil {
		return fmt.Errorf("appending to postgresql.conf file resulted in an error: %w", err)
	}

	// Create a stub for the configuration file
	// to be filled during the real startup of this instance
	err = fileutils.CreateEmptyFile(
		path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile))
	if err != nil {
		return fmt.Errorf("appending to the operator managed settings file resulted in an error: %w", err)
	}

	return nil
}

// GetInstance gets the PostgreSQL instance which correspond to these init information
func (info InitInfo) GetInstance() *Instance {
	postgresInstance := NewInstance()
	postgresInstance.PgData = info.PgData
	postgresInstance.StartupOptions = []string{"listen_addresses='127.0.0.1'"}
	return postgresInstance
}

// ConfigureNewInstance creates the expected users and databases in a new
// PostgreSQL instance. If any error occurs, we return it
func (info InitInfo) ConfigureNewInstance(instance *Instance) error {
	log.Info("Configuring new PostgreSQL instance")

	dbSuperUser, err := instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting superuser database: %w", err)
	}

	_, err = dbSuperUser.Exec(fmt.Sprintf(
		"CREATE USER %v",
		pq.QuoteIdentifier(info.ApplicationUser)))
	if err != nil {
		return err
	}

	// Execute the custom set of init queries
	log.Info("Executing post-init SQL instructions")
	if err = info.executeQueries(dbSuperUser, info.PostInitSQL); err != nil {
		return err
	}

	dbTemplate, err := instance.GetTemplateDB()
	if err != nil {
		return fmt.Errorf("while getting template database: %w", err)
	}
	// Execute the custom set of init queries of the template
	log.Info("Executing post-init template SQL instructions")
	if err = info.executeQueries(dbTemplate, info.PostInitTemplateSQL); err != nil {
		return fmt.Errorf("could not execute init Template queries: %w", err)
	}

	if info.ApplicationDatabase != "" {
		_, err = dbSuperUser.Exec(fmt.Sprintf("CREATE DATABASE %v OWNER %v",
			pq.QuoteIdentifier(info.ApplicationDatabase),
			pq.QuoteIdentifier(info.ApplicationUser)))
		if err != nil {
			return fmt.Errorf("could not create ApplicationDatabase: %w", err)
		}
		appDB, err := instance.ConnectionPool().Connection(info.ApplicationDatabase)
		if err != nil {
			return fmt.Errorf("could not get connection to ApplicationDatabase: %w", err)
		}
		// Execute the custom set of init queries of the application database
		log.Info("Executing Application instructions")
		if err = info.executeQueries(appDB, info.PostInitApplicationSQL); err != nil {
			return fmt.Errorf("could not execute init Application queries: %w", err)
		}
	}

	return nil
}

// executeQueries run the set of queries in the provided database connection
func (info InitInfo) executeQueries(sqlUser *sql.DB, queries []string) error {
	if len(queries) == 0 {
		log.Debug("No queries to execute")
		return nil
	}

	for _, sqlQuery := range queries {
		log.Debug("Executing query", "sqlQuery", sqlQuery)
		_, err := sqlUser.Exec(sqlQuery)
		if err != nil {
			return err
		}
	}

	return nil
}

// Bootstrap creates and configures this new PostgreSQL instance
func (info InitInfo) Bootstrap() error {
	err := info.CreateDataDirectory()
	if err != nil {
		return err
	}

	instance := info.GetInstance()

	majorVersion, err := postgres.GetMajorVersion(instance.PgData)
	if err != nil {
		return fmt.Errorf("while reading major version: %w", err)
	}

	if majorVersion >= 12 {
		primaryConnInfo := buildPrimaryConnInfo(info.ClusterName, info.PodName)
		_, err = configurePostgresAutoConfFile(info.PgData, primaryConnInfo)
		if err != nil {
			return fmt.Errorf("while configuring replica: %w", err)
		}
	}

	return instance.WithActiveInstance(func() error {
		err = info.ConfigureNewInstance(instance)
		if err != nil {
			return fmt.Errorf("while configuring new instance: %w", err)
		}

		return nil
	})
}
