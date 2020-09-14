/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import (
	"os"
	"path"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/fileutils"
)

// InstallPgPass install a given pgpass file in the user home directory
func InstallPgPass(pgpass string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	targetPgPass := path.Join(homeDir, ".pgpass")
	if err = fileutils.CopyFile(pgpass, targetPgPass); err != nil {
		return err
	}

	return os.Chmod(targetPgPass, 0600)
}
