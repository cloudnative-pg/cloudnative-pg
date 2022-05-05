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
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Release tag extraction", func() {
	It("properly works with expected filename", func() {
		tag := extractTag("cnpg-0.5.0.yaml")
		Expect(tag).To(Equal("0.5.0"))
	})
})

var _ = Describe("Most recent tag", func() {
	It("properly works with release tag", func() {
		wd, err := os.Getwd()
		Expect(err).To(BeNil())
		releasesDir := filepath.Join(filepath.Dir(filepath.Dir(wd)), "releases")
		versionList, err := GetAvailableReleases(releasesDir)
		Expect(err).To(BeNil())
		if len(versionList) < 2 {
			Skip("because we need two or more releases")
		}
		err = os.Setenv("CNPG_VERSION", versions.Version)
		Expect(err).To(BeNil())
		tag, err := GetMostRecentReleaseTag(releasesDir)
		Expect(tag).To(Not(BeEmpty()))
		Expect(tag).ToNot(BeEquivalentTo(versions.Version))
		Expect(err).To(BeNil())
	})

	It("properly works with dev tag", func() {
		wd, err := os.Getwd()
		Expect(err).To(BeNil())
		releasesDir := filepath.Join(filepath.Dir(filepath.Dir(wd)), "releases")
		err = os.Setenv("CNPG_VERSION", versions.Version+"-test")
		Expect(err).To(BeNil())
		tag, err := GetMostRecentReleaseTag(releasesDir)
		Expect(tag).To(Not(BeEmpty()))
		Expect(tag).To(BeEquivalentTo(versions.Version))
		Expect(err).To(BeNil())
	})
})

var _ = Describe("Dev tag version check", func() {
	It("returns true when CNPG_VERSION contains a dev tag", func() {
		err := os.Setenv("CNPG_VERSION", "100.9.1-test")
		Expect(err).To(BeNil())
		Expect(isDevTagVersion()).To(BeTrue())
	})
	It("returns false when CNPG_VERSION contains a release tag", func() {
		err := os.Setenv("CNPG_VERSION", "100.9.1")
		Expect(err).To(BeNil())
		Expect(isDevTagVersion()).To(BeFalse())
	})
})
