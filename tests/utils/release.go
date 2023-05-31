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
	versions, err := GetAvailableReleases(releasesPath)
	if err != nil {
		return "", err
	}

	if len(versions) == 0 {
		return "", errors.New("could not find releases")
	}

	// if we're running on a release branch, we should get the previous version (if it
	// has) to test upgrades from it
	if len(versions) > 1 && isReleasePullRequestBranch() {
		return versions[1].String(), nil
	}

	// otherwise, we take for granted it's on a dev branch (or just one release available),
	// so just return the latest release tag
	return versions[0].String(), nil
}

// GetAvailableReleases retrieves all the available releases from
// the list of YAML files in the top-level `releases/` directory.
func GetAvailableReleases(releasesPath string) ([]*semver.Version, error) {
	fileInfo, err := os.ReadDir(releasesPath)
	if err != nil {
		return nil, err
	}

	for i, file := range fileInfo {
		if !strings.HasSuffix(file.Name(), ".yaml") {
			fileInfo = append(fileInfo[:i], fileInfo[i+1:]...)
		}
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

	return versions, nil
}

func isReleasePullRequestBranch() bool {
	branchName := os.Getenv("BRANCH_NAME")
	if branchName == "" {
		branchNameBytes, err := exec.Command("git", "symbolic-ref", "--short", "-q", "HEAD").Output()
		if err != nil {
			return false
		}
		branchName = string(branchNameBytes)
	}
	return strings.HasPrefix(branchName, "release/v")
}

func extractTag(releaseFile string) string {
	releaseFile = strings.TrimPrefix(releaseFile, "cnpg-")
	tag := strings.TrimSuffix(releaseFile, ".yaml")

	return tag
}
