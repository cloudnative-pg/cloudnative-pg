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
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// GetMostRecentReleaseTag retrieves the most recent release tag from a list of files into a folder
func GetMostRecentReleaseTag(releasesPath string) (string, error) {
	fileInfo, err := ioutil.ReadDir(releasesPath)
	if err != nil {
		return "", err
	}

	versions := []*semver.Version{}

	// build the array that contains the versions
	// found in the releasePath directory
	for _, file := range fileInfo {
		tag := extractTag(file.Name())
		versions = append(versions, semver.MustParse(tag))
	}

	// Sorting version as descending order ([v1.10.0, v1.9.0...])
	sort.Sort(sort.Reverse(semver.Collection(versions)))

	if !isDevTagVersion() {
		return versions[1].String(), nil
	}

	return versions[0].String(), nil
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
