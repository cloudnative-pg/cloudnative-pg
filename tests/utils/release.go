/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package utils contains helper functions/methods for e2e
package utils

import (
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// GetMostRecentReleaseTag retrieves the most recent release tag from a list of files into a folder
func GetMostRecentReleaseTag(releasesPath string) (string, error) {
	fileInfo, err := ioutil.ReadDir(releasesPath)
	if err != nil {
		return "", err
	}

	var mostRecentTag, previousMostRecentTag semver.Version

	isDevTag := isDevTagVersion()

	for _, file := range fileInfo {
		tag := extractTag(file.Name())
		version := semver.MustParse(tag)
		if version.GreaterThan(&mostRecentTag) {
			previousMostRecentTag = mostRecentTag
			mostRecentTag = *version
		}
	}

	if !isDevTag {
		return previousMostRecentTag.String(), nil
	}

	return mostRecentTag.String(), nil
}

func isDevTagVersion() bool {
	var currentTagVersion string
	if currentTagVersion = os.Getenv("VERSION"); currentTagVersion == "" {
		currentTagVersionBytes, err := exec.Command("git", "describe", "--tags", "--match", "v*").Output()
		if err != nil {
			return false
		}
		currentTagVersion = string(currentTagVersionBytes)
	}

	s := strings.Split(currentTagVersion, "-")
	return len(s) != 1
}

func extractTag(releaseFile string) string {
	releaseFile = strings.TrimPrefix(releaseFile, "postgresql-operator-")
	tag := strings.TrimSuffix(releaseFile, ".yaml")

	return tag
}
