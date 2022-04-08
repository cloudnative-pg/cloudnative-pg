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

// Package utils contains helper functions/methods for e2e
package utils

import (
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// GetMostRecentReleaseTag retrieves the most recent release tag from
// the list of YAML files in the top-level `releases/` directory.
// Used for testing upgrades, so: if we're in a release branch, the MostRecent
// should be the next-to-last release
func GetMostRecentReleaseTag(releasesPath string) (string, error) {
	fileInfo, err := ioutil.ReadDir(releasesPath)
	if err != nil {
		return "", err
	}

	versions := make([]*semver.Version, len(fileInfo))

	// build the array that contains the versions
	// found in the releasePath directory
	for i, file := range fileInfo {
		tag := extractTag(file.Name())
		versions[i] = semver.MustParse(tag)
	}

	// Sorting version as descending order ([v1.10.0, v1.9.0...])
	sort.Sort(sort.Reverse(semver.Collection(versions)))
	if len(versions) == 0 {
		return "", errors.New("could not find releases")
	}

	if isDevTagVersion() {
		return versions[0].String(), nil
	}

	// if we're running on a release branch, we should get the previous version
	// to test upgrades from it
	return versions[1].String(), nil
}

func isDevTagVersion() bool {
	currentTagVersion := os.Getenv("CNP_VERSION")
	if currentTagVersion == "" {
		currentTagVersionBytes, err := exec.Command("git", "describe", "--tags", "--match", "v*").Output()
		if err != nil {
			return false
		}
		currentTagVersion = string(currentTagVersionBytes)
	}

	fragments := strings.Split(currentTagVersion, "-")
	return len(fragments) > 1
}

func extractTag(releaseFile string) string {
	releaseFile = strings.TrimPrefix(releaseFile, "postgresql-operator-")
	tag := strings.TrimSuffix(releaseFile, ".yaml")

	return tag
}
