/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// PgPassEntry represent an entry inside the .pgpass file
type PgPassEntry struct {
	// The DB host name (must match as string)
	HostName string

	// The port
	Port int

	// The database name
	DBName string

	// The user name
	Username string

	// The password
	Password string
}

// CreatePgPassLine create the line .pgpass line for this entry
func (entry PgPassEntry) CreatePgPassLine() string {
	return fmt.Sprintf(
		"%v:%v:%v:%v:%v\n",
		entry.HostName, // host
		entry.Port,     // port
		entry.DBName,   // database name
		entry.Username, // user name
		entry.Password) // password
}

// CreatePgPassContent create the contents of a .pgpass file
func CreatePgPassContent(content []PgPassEntry) string {
	result := ""
	for _, entry := range content {
		result += entry.CreatePgPassLine()
	}
	return result
}

// CreatePgPass create and install a `.pgpass` file to allow
// connecting to all PostgreSQL server with password in the
// password file
func CreatePgPass(pwfile string) error {
	if pwfile == "" {
		return nil
	}

	log.Log.Info("Creating pgpass file", "pwfile", pwfile)
	password, err := fileutils.ReadFile(pwfile)
	if err != nil {
		return err
	}

	entries := []PgPassEntry{
		{
			HostName: "*",
			Port:     5432,
			DBName:   "postgres",
			Username: "postgres",
			Password: password,
		},
		{
			HostName: "*",
			Port:     5432,
			DBName:   "replication",
			Username: "postgres",
			Password: password,
		},
	}

	if err = InstallPgPass(CreatePgPassContent(entries)); err != nil {
		return err
	}

	return nil
}

// InstallPgPass install a pgpass file with the given content in the
// user home directory
func InstallPgPass(pgpassContent string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	targetPgPass := path.Join(homeDir, ".pgpass")
	if err = ioutil.WriteFile(targetPgPass, []byte(pgpassContent), 0600); err != nil {
		return err
	}

	return nil
}
