/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"database/sql"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/blang/semver"
)

// GetPgVersion returns the version of postgres in a semantic format or an error
func GetPgVersion(db *sql.DB) (*semver.Version, error) {
	var versionString string
	row := db.QueryRow("SHOW server_version_num")
	err := row.Scan(&versionString)
	if err != nil {
		return nil, err
	}
	return parseVersionNum(versionString)
}

func parseVersionNum(versionNum string) (*semver.Version, error) {
	versionInt, err := strconv.ParseUint(versionNum, 10, 64)
	if err != nil {
		return nil, err
	}

	return &semver.Version{
		Major: versionInt / 10000,
		Minor: (versionInt / 100) % 100,
		Patch: versionInt % 100,
	}, nil
}

// GetMajorVersionFromPgData read the PG_VERSION file in the data directory
// returning the major version of the database
func GetMajorVersionFromPgData(pgData string) (int, error) {
	content, err := os.ReadFile(path.Join(pgData, "PG_VERSION")) // #nosec
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(strings.TrimSpace(string(content)))
}
