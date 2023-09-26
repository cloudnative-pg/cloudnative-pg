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

package configfile

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
)

var _ = Describe("update Postgres configuration files", func() {
	var tmpDir string

	_ = BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "configuration-test-")
		Expect(err).NotTo(HaveOccurred())
	})

	_ = AfterEach(func() {
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
	})

	It("must append missing keys", func() {
		initialContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someHost user=someUser application_name=nodeNameBis'\n" +
			"recovery_target_timeline = 'latest'\n"

		testFile := filepath.Join(tmpDir, "custom.conf")
		Expect(fileutils.WriteStringToFile(testFile, initialContent)).To(BeTrue())

		Expect(UpdatePostgresConfigurationFile(testFile, map[string]string{
			"test.key":         "test.value",
			"primary_conninfo": "host=someUpdatedHost user=someUpdatedUser application_name=nodeNameBis",
		})).To(BeTrue())

		wantedContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someUpdatedHost user=someUpdatedUser application_name=nodeNameBis'\n" +
			"recovery_target_timeline = 'latest'\n" +
			"test.key = 'test.value'\n"

		finalContent, err := fileutils.ReadFile(testFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(finalContent)).To(Equal(wantedContent))
	})

	It("must remove missing managed keys", func() {
		initialContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someHost user=someUser application_name=nodeNameBis'\n" +
			"recovery_target_timeline = 'latest'\n"

		testFile := filepath.Join(tmpDir, "custom.conf")
		Expect(fileutils.WriteStringToFile(testFile, initialContent)).To(BeTrue())

		Expect(UpdatePostgresConfigurationFile(testFile, map[string]string{
			"test.key":         "test.value",
			"primary_conninfo": "host=someUpdatedHost user=someUpdatedUser application_name=nodeNameBis",
		}, "recovery_target_timeline")).To(BeTrue())

		wantedContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someUpdatedHost user=someUpdatedUser application_name=nodeNameBis'\n" +
			"test.key = 'test.value'\n"

		finalContent, err := fileutils.ReadFile(testFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(finalContent)).To(Equal(wantedContent))
	})

	It("must not change managed keys when present", func() {
		initialContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someHost user=someUser application_name=nodeNameBis'\n" +
			"recovery_target_timeline = 'latest'\n"

		testFile := filepath.Join(tmpDir, "custom.conf")
		Expect(fileutils.WriteStringToFile(testFile, initialContent)).To(BeTrue())

		Expect(UpdatePostgresConfigurationFile(testFile, map[string]string{
			"test.key":         "test.value",
			"primary_conninfo": "host=someUpdatedHost user=someUpdatedUser application_name=nodeNameBis",
		}, "primary_conninfo")).To(BeTrue())

		wantedContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someUpdatedHost user=someUpdatedUser application_name=nodeNameBis'\n" +
			"recovery_target_timeline = 'latest'\n" +
			"test.key = 'test.value'\n"

		finalContent, err := fileutils.ReadFile(testFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(finalContent)).To(Equal(wantedContent))
	})

	It("must work with missing files", func() {
		testFile := filepath.Join(tmpDir, "custom.conf")
		Expect(fileutils.FileExists(testFile)).To(BeFalse())

		Expect(UpdatePostgresConfigurationFile(testFile, map[string]string{
			"primary_conninfo": "host=someHost user=someUser application_name=nodeName",
		})).To(BeTrue())

		wantedContent := "primary_conninfo = 'host=someHost user=someUser application_name=nodeName'\n"

		finalContent, err := fileutils.ReadFile(testFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(finalContent)).To(Equal(wantedContent))
	})
})

var _ = Describe("Update configuration files", func() {
	It("must append missing keys", func() {
		initialContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someHost user=someUser application_name=nodeName'\n" +
			"recovery_target_timeline = 'latest'\n"

		updatedContent, _ := UpdateConfigurationContents(initialContent, map[string]string{
			"test.key": "test.value",
		})

		wantedContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someHost user=someUser application_name=nodeName'\n" +
			"recovery_target_timeline = 'latest'\n" +
			"test.key = 'test.value'\n"

		Expect(updatedContent).To(Equal(wantedContent))
	})

	It("must remove repeated keys", func() {
		initialContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someHost1 user=someUser1 application_name=nodeName1'\n" +
			"recovery_target_timeline = 'latest'\n" +
			"primary_conninfo = 'host=someHost2 user=someUser2 application_name=nodeName2'\n"

		updatedContent, _ := UpdateConfigurationContents(initialContent, map[string]string{
			"primary_conninfo": "host=someHost user=someUser application_name=nodeName",
		})

		wantedContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someHost user=someUser application_name=nodeName'\n" +
			"recovery_target_timeline = 'latest'\n"

		Expect(updatedContent).To(Equal(wantedContent))
	})
})

var _ = Describe("Remove configuration files option", func() {
	It("keeps the initial input if the option to be removed is not matched", func() {
		initialContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someHost user=someUser application_name=nodeName'\n" +
			"recovery_target_timeline = 'latest'\n"

		updatedContent := RemoveOptionsFromConfigurationContents(initialContent, "archive_mode")

		Expect(updatedContent).To(Equal(initialContent))
	})

	It("must delete lines with the given option", func() {
		initialContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someHost user=someUser application_name=nodeName'\n" +
			"archive_mode = 'on'\n" +
			"recovery_target_timeline = 'latest'\n" +
			"archive_mode = 'always'\n"

		updatedContent := RemoveOptionsFromConfigurationContents(initialContent, "archive_mode")

		wantedContent := "# Do not edit this file manually!\n" +
			"# It will be overwritten by the ALTER SYSTEM command.\n" +
			"primary_conninfo = 'host=someHost user=someUser application_name=nodeName'\n" +
			"recovery_target_timeline = 'latest'\n"

		Expect(updatedContent).To(Equal(wantedContent))
	})
})

var _ = Describe("ReadOptionsFromConfigurationContents", func() {
	It("should correctly parse content that contains comments", func() {
		initialContent := `
# Add settings for extensions here
shared_preload_libraries = 'bdr'
wal_level = 'logical'
track_commit_timestamp = on
`
		options := ReadOptionsFromConfigurationContents(initialContent,
			"shared_preload_libraries",
			"wal_level",
			"track_commit_timestamp",
		)
		Expect(options).To(HaveLen(3))
		Expect(options).To(Equal(map[string]string{
			"shared_preload_libraries": "'bdr'",
			"wal_level":                "'logical'",
			"track_commit_timestamp":   "on",
		}))
	})

	It("should return an empty map when parsing an empty content", func() {
		options := ReadOptionsFromConfigurationContents("",
			"shared_preload_libraries",
		)
		Expect(options).To(BeEmpty())
		Expect(options).To(Equal(map[string]string{}))
	})
})
