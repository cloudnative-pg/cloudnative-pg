/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package spool

import (
	"io/ioutil"
	"os"
	"path"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Spool", func() {
	var tmpDir string
	var tmpDir2 string
	var spool *WALSpool

	_ = BeforeEach(func() {
		var err error
		tmpDir, err = ioutil.TempDir("", "spool-test-")
		Expect(err).NotTo(HaveOccurred())

		tmpDir2, err = ioutil.TempDir("", "spool-test-tmp-")
		Expect(err).NotTo(HaveOccurred())

		spool, err = New(tmpDir)
		Expect(err).NotTo(HaveOccurred())
	})

	_ = AfterEach(func() {
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
		Expect(os.RemoveAll(tmpDir2)).To(Succeed())
	})

	It("create and removes files from/into the spool", func() {
		var err error
		const walFile = "000000020000068A00000002"

		// This WAL file doesn't exist
		Expect(spool.Contains(walFile)).To(BeFalse())

		// If I try to remove a WAL file that doesn't exist, I obtain an error
		err = spool.Remove(walFile)
		Expect(err).To(Equal(ErrorNonExistentFile))

		// I add it into the spool
		err = spool.Touch(walFile)
		Expect(err).NotTo(HaveOccurred())

		// Now the file exists
		Expect(spool.Contains(walFile)).To(BeTrue())

		// I can now remove it
		err = spool.Remove(walFile)
		Expect(err).NotTo(HaveOccurred())

		// And now it doesn't exist again
		Expect(spool.Contains(walFile)).To(BeFalse())
	})

	It("can move out files from the spool", func() {
		var err error
		const walFile = "000000020000068A00000003"

		err = spool.Touch(walFile)
		Expect(err).ToNot(HaveOccurred())

		// Move out this file
		destinationPath := path.Join(tmpDir2, "testFile")
		err = spool.MoveOut(walFile, destinationPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(spool.Contains(walFile)).To(BeFalse())
		Expect(fileutils.FileExists(destinationPath))
	})

	It("can determine names for each WAL files", func() {
		const walFile = "000000020000068A00000004"
		Expect(spool.FileName(walFile)).To(Equal(path.Join(tmpDir, walFile)))
	})
})
