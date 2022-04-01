/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package fileutils

import (
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("File writing functions", func() {
	It("write a new file", func() {
		changed, err := WriteStringToFile(path.Join(tempDir1, "test.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).To(BeNil())
	})

	It("detect if the file has changed or not", func() {
		changed, err := WriteStringToFile(path.Join(tempDir1, "test2.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).To(BeNil())

		changed2, err := WriteStringToFile(path.Join(tempDir1, "test2.txt"), "this is a test")
		Expect(changed2).To(BeFalse())
		Expect(err).To(BeNil())
	})

	It("create a new directory if needed", func() {
		changed, err := WriteStringToFile(path.Join(tempDir1, "test", "test3.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).To(BeNil())
	})
})

var _ = Describe("File copying functions", func() {
	It("copy files", func() {
		changed, err := WriteStringToFile(path.Join(tempDir2, "test.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).To(BeNil())

		result, err := FileExists(path.Join(tempDir2, "test2.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeFalse())

		err = CopyFile(path.Join(tempDir2, "test.txt"), path.Join(tempDir2, "test2.txt"))
		Expect(err).To(BeNil())

		result, err = FileExists(path.Join(tempDir2, "test2.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeTrue())
	})

	It("creates directories when needed", func() {
		changed, err := WriteStringToFile(path.Join(tempDir2, "test3.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).To(BeNil())

		result, err := FileExists(path.Join(tempDir2, "temp", "test3.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeFalse())

		err = CopyFile(path.Join(tempDir2, "test.txt"), path.Join(tempDir2, "temp", "test3.txt"))
		Expect(err).To(BeNil())

		result, err = FileExists(path.Join(tempDir2, "temp", "test3.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeTrue())
	})

	It("removes the content of a directory", func() {
		var err error
		var result bool

		result, err = FileExists(path.Join(tempDir2, "test3.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeTrue())

		result, err = FileExists(path.Join(tempDir2, "test3.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeTrue())

		result, err = FileExists(path.Join(tempDir2, "temp"))
		Expect(err).To(BeNil())
		Expect(result).To(BeTrue())

		result, err = FileExists(path.Join(tempDir2, "temp", "test3.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeTrue())

		err = RemoveDirectoryContent(tempDir2)
		Expect(err).To(BeNil())

		result, err = FileExists(path.Join(tempDir2, "test3.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeFalse())

		result, err = FileExists(path.Join(tempDir2, "test3.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeFalse())

		result, err = FileExists(path.Join(tempDir2, "temp"))
		Expect(err).To(BeNil())
		Expect(result).To(BeFalse())

		result, err = FileExists(path.Join(tempDir2, "temp", "test3.txt"))
		Expect(err).To(BeNil())
		Expect(result).To(BeFalse())
	})
})

var _ = Describe("function GetDirectoryContent", func() {
	It("returns error if directory doesn't exist", func() {
		_, err := GetDirectoryContent(filepath.Join(tempDir3, "not-exists"))
		Expect(err).Should(HaveOccurred())
	})
	It("returns the list of file names in a directory", func() {
		testFiles := make([]string, 10)
		for i := 0; i < 10; i++ {
			testFiles[i] = fmt.Sprintf("test_file_%v", i)
			file := filepath.Join(tempDir3, testFiles[i])
			err := ioutil.WriteFile(file, []byte("fake_content"), 0o400)
			Expect(err).ShouldNot(HaveOccurred())
		}
		files, err := GetDirectoryContent(tempDir3)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(files).Should(ConsistOf(testFiles))
	})
})
