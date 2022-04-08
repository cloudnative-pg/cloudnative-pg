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

package controller

import (
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
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
