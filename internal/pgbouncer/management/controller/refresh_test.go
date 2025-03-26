/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RefreshConfigurationFiles", func() {
	var (
		tmpDir string
		files  config.ConfigurationFiles
		err    error
	)

	BeforeEach(func() {
		tmpDir, err = os.MkdirTemp("", "test")
		Expect(err).NotTo(HaveOccurred())
		files = make(config.ConfigurationFiles)
	})

	AfterEach(func() {
		err = os.RemoveAll(tmpDir)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("when no files are passed", func() {
		It("should return false and no error", func(ctx SpecContext) {
			changed, err := refreshConfigurationFiles(ctx, files)
			Expect(changed).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when files are passed", func() {
		BeforeEach(func() {
			files[filepath.Join(tmpDir, "config1")] = []byte("content1")
			files[filepath.Join(tmpDir, "config2")] = []byte("content2")
		})

		It("should write content to files and return true", func(ctx SpecContext) {
			changed, err := refreshConfigurationFiles(ctx, files)
			Expect(changed).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			for filename, content := range files {
				fileContent, err := os.ReadFile(filename) // nolint: gosec
				Expect(err).NotTo(HaveOccurred())
				Expect(fileContent).To(Equal(content))
			}
		})
	})

	Context("when given an invalid file path", func() {
		BeforeEach(func() {
			files["/proc/you-cannot-write-here.conf"] = []byte("content")
		})

		It("should return an error", func(ctx SpecContext) {
			_, err := refreshConfigurationFiles(ctx, files)
			Expect(err).To(HaveOccurred())
		})
	})
})
