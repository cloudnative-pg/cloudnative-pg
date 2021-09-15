/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"
	"io/ioutil"
	"path"
	"strconv"
	"strings"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

const firstMajorWithoutMinor = 10

// GetPostgresVersionFromTag parse a PostgreSQL version string returning
// a major version ID. Example:
//
//     GetPostgresVersionFromTag("9.5.3") == 90503
//     GetPostgresVersionFromTag("10.2") == 100002
func GetPostgresVersionFromTag(version string) (int, error) {
	if versionDelimiter := strings.IndexAny(version, "_-"); versionDelimiter >= 0 {
		version = version[:versionDelimiter]
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
	if versionDelimiter := strings.IndexAny(version, "_-"); versionDelimiter >= 0 {
		version = version[:versionDelimiter]
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

// GetMajorVersion read the PG_VERSION file in the data directory
// returning the major version of the database
func GetMajorVersion(pgData string) (int, error) {
	content, err := ioutil.ReadFile(path.Join(pgData, "PG_VERSION")) // #nosec
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(strings.TrimSpace(string(content)))
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
