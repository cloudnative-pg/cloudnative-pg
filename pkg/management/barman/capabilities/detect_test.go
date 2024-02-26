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

package capabilities

import (
	"github.com/blang/semver"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Correct detecting the barman capabilities", func() {
	It("All version support in 3.4 and above", func() {
		version, err := semver.ParseTolerant("3.4.0")
		Expect(err).ToNot(HaveOccurred())
		capabilities, _ := Detect(&version)
		Expect(capabilities).To(Equal(&Capabilities{
			Version:                    &version,
			hasName:                    true,
			HasAzure:                   true,
			HasS3:                      true,
			HasGoogle:                  true,
			HasRetentionPolicy:         true,
			HasTags:                    true,
			HasCheckWalArchive:         true,
			HasSnappy:                  true,
			HasErrorCodesForWALRestore: true,
			HasAzureManagedIdentity:    true,
		}))
	})

	It("test barman version below 3.4 should has no name backup", func() {
		version, err := semver.ParseTolerant("3.0.0")
		Expect(err).ToNot(HaveOccurred())
		capabilities, _ := Detect(&version)
		Expect(capabilities).To(Equal(&Capabilities{
			Version:                    &version,
			HasAzure:                   true,
			HasS3:                      true,
			HasGoogle:                  true,
			HasRetentionPolicy:         true,
			HasTags:                    true,
			HasCheckWalArchive:         true,
			HasSnappy:                  true,
			HasErrorCodesForWALRestore: true,
			HasAzureManagedIdentity:    true,
		}))
	})

	It("test barman version below 2.19.0 should has no google credentials ", func() {
		version, err := semver.ParseTolerant("2.18.0")
		Expect(err).ToNot(HaveOccurred())
		capabilities, _ := Detect(&version)
		Expect(capabilities).To(Equal(&Capabilities{
			Version:                    &version,
			HasAzure:                   true,
			HasS3:                      true,
			HasRetentionPolicy:         true,
			HasTags:                    true,
			HasCheckWalArchive:         true,
			HasSnappy:                  true,
			HasErrorCodesForWALRestore: true,
			HasAzureManagedIdentity:    true,
		}))
	})

	// 2.17.0 should NOT support following
	// google credentials
	// tag
	// HasCheckWalArchive
	// HasSnappy
	// HasErrorCodesForWALRestore
	// HasAzureManagedIdentity
	It("test barman version below 2.18 should not support various options", func() {
		version, err := semver.ParseTolerant("2.17.0")
		Expect(err).ToNot(HaveOccurred())
		capabilities, _ := Detect(&version)
		Expect(capabilities).To(Equal(&Capabilities{
			Version:            &version,
			HasAzure:           true,
			HasS3:              true,
			HasRetentionPolicy: true,
		}))
	})

	It("test barman version below 2.14.0 should has no HasRetentionPolicy ", func() {
		version, err := semver.ParseTolerant("2.13.0")
		Expect(err).ToNot(HaveOccurred())
		capabilities, _ := Detect(&version)
		Expect(capabilities).To(Equal(&Capabilities{
			Version:  &version,
			HasAzure: true,
			HasS3:    true,
		}))
	})

	It("test barman version below 2.13.0 should has no aws and azure credentials ", func() {
		version, err := semver.ParseTolerant("2.12.0")
		Expect(err).ToNot(HaveOccurred())
		capabilities, _ := Detect(&version)
		Expect(capabilities).To(Equal(&Capabilities{
			Version: &version,
		}))
	})
})
