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

package utils

import (
	"path/filepath"
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Release tag extraction", func() {
	It("properly works with expected filename", func() {
		tag, err := extractTag("cnpg-0.5.0.yaml")
		Expect(err).ToNot(HaveOccurred())
		Expect(tag).To(Equal("0.5.0"))
	})
})

var _ = Describe("Most recent tag", func() {
	It("properly works with release branch", func() {
		releasesDir, err := filepath.Abs("../../releases")
		Expect(err).ToNot(HaveOccurred())

		versionList, err := GetAvailableReleases(releasesDir)
		Expect(err).ToNot(HaveOccurred())
		if len(versionList) < 2 {
			Skip("because we need two or more releases")
		}

		GinkgoT().Setenv("BRANCH_NAME", "release/v"+versions.Version)

		tag, err := GetMostRecentReleaseTag(releasesDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(tag).To(Not(BeEmpty()))
		Expect(tag).ToNot(BeEquivalentTo(versions.Version))
	})

	It("properly works with dev branch", func() {
		releasesDir, err := filepath.Abs("../../releases")
		Expect(err).ToNot(HaveOccurred())

		GinkgoT().Setenv("BRANCH_NAME", "dev/"+versions.Version)

		tag, err := GetMostRecentReleaseTag(releasesDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(tag).To(Not(BeEmpty()))
		if strings.Contains(versions.Version, "-rc") {
			// an RC release should not count as the most recent release
			Expect(tag).NotTo(BeEquivalentTo(versions.Version))
		} else {
			Expect(tag).To(BeEquivalentTo(versions.Version))
		}
	})
})

var _ = Describe("GetAvailableReleases fails on wrong release directory", func() {
	It("properly errors out if the release directory doesn't exist", func() {
		tmpDir := GinkgoT().TempDir()
		nonexistent := filepath.Join(filepath.Dir(tmpDir), "nonexistent")

		_, err := GetAvailableReleases(nonexistent)
		Expect(err).To(HaveOccurred())
	})

	It("properly fails if there's no release files in the directory", func() {
		tmpDir := GinkgoT().TempDir()

		_, err := GetMostRecentReleaseTag(tmpDir)
		Expect(err).To(HaveOccurred())
	})
})
