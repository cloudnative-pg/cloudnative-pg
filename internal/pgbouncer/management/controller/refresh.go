/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package controller

import (
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/pgbouncer/config"
)

// refreshConfigurationFiles writes the configuration files, returning a
// flag indicating if something is changed or not and an error status
func refreshConfigurationFiles(files config.ConfigurationFiles) (bool, error) {
	var changed bool

	for fileName, content := range files {
		changedFile, err := fileutils.WriteFileAtomic(fileName, content, 0o600)
		if err != nil {
			return false, fmt.Errorf("while recreating configs:%w", err)
		}
		if changedFile {
			log.Info("updated configuration file", "name", fileName)
		}
		changed = changed || changedFile
	}

	return changed, nil
}
