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
