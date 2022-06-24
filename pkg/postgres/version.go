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
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const firstMajorWithoutMinor = 10

var semanticVersionRegex = regexp.MustCompile(`^(\d\.?)+`)

// GetPostgresVersionFromTag parse a PostgreSQL version string returning
// a major version ID. Example:
//
//     GetPostgresVersionFromTag("9.5.3") == 90503
//     GetPostgresVersionFromTag("10.2") == 100002
//     GetPostgresVersionFromTag("15beta1") == 150000
func GetPostgresVersionFromTag(version string) (int, error) {
	if !semanticVersionRegex.MatchString(version) {
		return 0,
			fmt.Errorf("version not starting with a semantic version regex (%v): %s", semanticVersionRegex, version)
	}

	if versionOnly := semanticVersionRegex.FindString(version); versionOnly != "" {
		version = versionOnly
	}

	splitVersion := strings.Split(version, ".")

	idx := 0
	majorVersion, err := strconv.Atoi(splitVersion[idx])
	if err != nil {
		return 0, fmt.Errorf("wrong PostgreSQL major in version %v", version)
	}
	parsedVersion := majorVersion * 10000
	idx++

	if majorVersion < firstMajorWithoutMinor {
		if len(splitVersion) <= idx {
			return 0, fmt.Errorf("missing PostgreSQL minor in version %v", version)
		}
		minorVersion, err := strconv.Atoi(splitVersion[idx])
		if err != nil || minorVersion >= 100 {
			return 0, fmt.Errorf("wrong PostgreSQL minor in version %v", version)
		}
		parsedVersion += minorVersion * 100
		idx++
	}

	if len(splitVersion) > idx {
		patchLevel, err := strconv.Atoi(splitVersion[idx])
		if err != nil || patchLevel >= 100 {
			return 0, fmt.Errorf("wrong PostgreSQL patch level in version %v", version)
		}
		parsedVersion += patchLevel
	}

	return parsedVersion, nil
}

// GetPostgresMajorVersionFromTag retrieves the major version from a version tag
func GetPostgresMajorVersionFromTag(version string) (int, error) {
	if !semanticVersionRegex.MatchString(version) {
		return 0,
			fmt.Errorf("version not starting with a semantic version regex (%v): %s", semanticVersionRegex, version)
	}

	if versionOnly := semanticVersionRegex.FindString(version); versionOnly != "" {
		version = versionOnly
	}

	splitVersion := strings.Split(version, ".")

	majorVersion, err := strconv.Atoi(splitVersion[0])
	if err != nil {
		return 0, fmt.Errorf("wrong format in PostgreSQL major version from %v: %w", splitVersion[0], err)
	}

	return majorVersion, err
}

// GetPostgresMajorVersion gets only the Major version from a PostgreSQL version string.
// Example:
//
//     GetPostgresMajorVersion("90503") == 90500
//     GetPostgresMajorVersion("100002") == 100000
func GetPostgresMajorVersion(parsedVersion int) int {
	return parsedVersion - parsedVersion%100
}

// IsUpgradePossible detect if it's possible to upgrade from fromVersion to
// toVersion
func IsUpgradePossible(fromVersion, toVersion int) bool {
	return GetPostgresMajorVersion(fromVersion) == GetPostgresMajorVersion(toVersion)
}

// CanUpgrade check if we can upgrade from une image version to another
func CanUpgrade(fromImage, toImage string) (bool, error) {
	fromTag := utils.GetImageTag(fromImage)
	toTag := utils.GetImageTag(toImage)

	if fromTag == "latest" || toTag == "latest" {
		// We don't really know which major version "latest" is,
		// so we can't safely upgrade
		return false, nil
	}

	fromVersion, err := GetPostgresVersionFromTag(fromTag)
	if err != nil {
		return false, err
	}

	toVersion, err := GetPostgresVersionFromTag(toTag)
	if err != nil {
		return false, err
	}

	return IsUpgradePossible(fromVersion, toVersion), nil
}
