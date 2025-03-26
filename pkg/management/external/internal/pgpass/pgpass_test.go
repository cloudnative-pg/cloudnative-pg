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

package pgpass

import (
	"path/filepath"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pgpass file generation", func() {
	var pgPassFile string
	BeforeEach(func() {
		tmpDir := GinkgoT().TempDir()
		pgPassFile = filepath.Join(tmpDir, ".pgpass")
	})

	When("the file is empty", func() {
		It("doesn't crash", func() {
			err := From().Write(pgPassFile)
			Expect(err).ToNot(HaveOccurred())

			content, err := fileutils.ReadFile(pgPassFile)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(BeEmpty())
		})
	})

	When("can overwrite if the file is not empty", func() {
		It("change the password as expected", func() {
			err := From(
				NewConnectionInfo(map[string]string{
					"host":   "pgtest.com",
					"port":   "5432",
					"dbname": "postgres",
					"user":   "postgres",
				}, "password"),
			).Write(pgPassFile)
			Expect(err).ToNot(HaveOccurred())
			content, err := fileutils.ReadFile(pgPassFile)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal(
				"pgtest.com:5432:*:postgres:password"))
			err = From(
				NewConnectionInfo(map[string]string{
					"host":   "pgtest.com",
					"port":   "5432",
					"dbname": "postgres",
					"user":   "postgres",
				}, "pwd"),
			).Write(pgPassFile)
			Expect(err).ToNot(HaveOccurred())
			content, err = fileutils.ReadFile(pgPassFile)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal(
				"pgtest.com:5432:*:postgres:pwd"))
		})
	})

	When("we have multiple connection strings", func() {
		It("correctly generates the content", func() {
			err := From(
				NewConnectionInfo(map[string]string{
					"host":   "pgtest.com",
					"port":   "5432",
					"dbname": "postgres",
					"user":   "postgres",
				}, "password"),
				NewConnectionInfo(map[string]string{
					"host":   "pgtwo.com",
					"port":   "5432",
					"dbname": "app",
					"user":   "app",
				}, "app"),
			).Write(pgPassFile)
			Expect(err).ToNot(HaveOccurred())

			content, err := fileutils.ReadFile(pgPassFile)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal(
				"pgtest.com:5432:*:postgres:password\n" +
					"pgtwo.com:5432:*:app:app"))
		})
	})
})

var _ = Describe("pgpass structure manipulation", func() {
	It("can create an empty pgpass file content", func() {
		content := Empty()
		Expect(content.info).To(BeEmpty())
	})

	It("can add a line to an empty pgpass file content", func() {
		content := Empty().WithLine(NewConnectionInfo(map[string]string{
			"host":   "pgtest.com",
			"port":   "5432",
			"dbname": "postgres",
			"user":   "postgres",
		}, "password"))

		Expect(content.info).To(HaveLen(1))
	})
})
