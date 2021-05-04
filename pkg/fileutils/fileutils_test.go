/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package fileutils

import (
	"io/ioutil"
	"os"
	"path"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("File writing functions", func() {
	tempDir, err := ioutil.TempDir(os.TempDir(), "fileutils_")
	if err != nil {
		panic(err)
	}
	defer func() {
		err = os.RemoveAll(tempDir)
		Expect(err).To(BeNil())
	}()

	It("write a new file", func() {
		changed, err := WriteStringToFile(path.Join(tempDir, "test.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).To(BeNil())
	})

	It("detect if the file has changed or not", func() {
		changed, err := WriteStringToFile(path.Join(tempDir, "test2.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).To(BeNil())

		changed2, err := WriteStringToFile(path.Join(tempDir, "test2.txt"), "this is a test")
		Expect(changed2).To(BeFalse())
		Expect(err).To(BeNil())
	})

	It("create a new directory if needed", func() {
		changed, err := WriteStringToFile(path.Join(tempDir, "test", "test3.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).To(BeNil())
	})
})

var _ = Describe("File copying functions", func() {
	tempDir, err := ioutil.TempDir(os.TempDir(), "fileutils_")
	if err != nil {
		panic(err)
	}
	defer func() {
		err = os.RemoveAll(tempDir)
		Expect(err).To(BeNil())
	}()

	It("copy files", func() {
		changed, err := WriteStringToFile(path.Join(tempDir, "test.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).To(BeNil())

		result, err := FileExists(path.Join(tempDir, "test2.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeFalse())

		err = CopyFile(path.Join(tempDir, "test.txt"), path.Join(tempDir, "test2.txt"))
		Expect(err).To(BeNil())

		result, err = FileExists(path.Join(tempDir, "test2.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeTrue())
	})

	It("creates directories when needed", func() {
		changed, err := WriteStringToFile(path.Join(tempDir, "test3.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).To(BeNil())

		result, err := FileExists(path.Join(tempDir, "temp", "test3.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeFalse())

		err = CopyFile(path.Join(tempDir, "test.txt"), path.Join(tempDir, "temp", "test3.txt"))
		Expect(err).To(BeNil())

		result, err = FileExists(path.Join(tempDir, "temp", "test3.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeTrue())
	})
})
