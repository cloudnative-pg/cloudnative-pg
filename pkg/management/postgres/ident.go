/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"
	"os/user"
	"path/filepath"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/constants"
)

// WritePostgresUserMaps creates a pg_ident.conf file containing only one map called "local" that
// maps the current user to "postgres" user.
func WritePostgresUserMaps(pgData string) error {
	var username string

	currentUser, err := user.Current()
	if err != nil {
		log.Info("Unable to identify the current user. Falling back to insecure mapping.")
		username = "/"
	} else {
		username = currentUser.Username
	}

	_, err = fileutils.WriteStringToFile(filepath.Join(pgData, constants.PostgresqlIdentFile),
		fmt.Sprintf("local %s postgres\n", username))
	if err != nil {
		return err
	}

	return nil
}
