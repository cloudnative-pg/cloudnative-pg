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

package logicalimport

import (
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

type (
	executable = string
)

const (
	pgDump           executable = "pg_dump"
	pgRestore        executable = "pg_restore"
	postgresDatabase            = "postgres"
	dumpDirectory               = specs.PgDataPath + "/dumps"
)

func createDumpsDirectory() error {
	return fileutils.EnsureDirectoryExist(dumpDirectory)
}

func generateFileNameForDatabase(database string) string {
	return fmt.Sprintf("%s/%s.dump", dumpDirectory, database)
}

func cleanDumpDirectory() error {
	return fileutils.RemoveDirectoryContent(dumpDirectory)
}
