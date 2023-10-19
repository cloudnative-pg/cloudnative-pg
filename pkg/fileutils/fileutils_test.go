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

package fileutils

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("File writing functions", func() {
	It("write a new file", func() {
		changed, err := WriteStringToFile(path.Join(tempDir1, "test.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
	})

	It("write a new file", func() {
		changed, err := WriteLinesToFile(path.Join(tempDir1, "test1.txt"), []string{"this", "is", "", "a", "test"})
		Expect(changed).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
	})

	It("detect if the file has changed or not", func() {
		changed, err := WriteStringToFile(path.Join(tempDir1, "test2.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		changed2, err := WriteStringToFile(path.Join(tempDir1, "test2.txt"), "this is a test")
		Expect(changed2).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	})

	It("create a new directory if needed", func() {
		changed, err := WriteStringToFile(path.Join(tempDir1, "test", "test3.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("File copying functions", func() {
	It("copy files", func() {
		changed, err := WriteStringToFile(path.Join(tempDir2, "test.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		result, err := FileExists(path.Join(tempDir2, "test2.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeFalse())

		err = CopyFile(path.Join(tempDir2, "test.txt"), path.Join(tempDir2, "test2.txt"))
		Expect(err).ToNot(HaveOccurred())

		result, err = FileExists(path.Join(tempDir2, "test2.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("creates directories when needed", func() {
		changed, err := WriteStringToFile(path.Join(tempDir2, "test3.txt"), "this is a test")
		Expect(changed).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		result, err := FileExists(path.Join(tempDir2, "temp", "test3.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeFalse())

		err = CopyFile(path.Join(tempDir2, "test.txt"), path.Join(tempDir2, "temp", "test3.txt"))
		Expect(err).ToNot(HaveOccurred())

		result, err = FileExists(path.Join(tempDir2, "temp", "test3.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("removes the content of a directory", func() {
		var err error
		var result bool

		result, err = FileExists(path.Join(tempDir2, "test3.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeTrue())

		result, err = FileExists(path.Join(tempDir2, "test3.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeTrue())

		result, err = FileExists(path.Join(tempDir2, "temp"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeTrue())

		result, err = FileExists(path.Join(tempDir2, "temp", "test3.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeTrue())

		err = RemoveDirectoryContent(tempDir2)
		Expect(err).ToNot(HaveOccurred())

		result, err = FileExists(path.Join(tempDir2, "test3.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeFalse())

		result, err = FileExists(path.Join(tempDir2, "test3.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeFalse())

		result, err = FileExists(path.Join(tempDir2, "temp"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeFalse())

		result, err = FileExists(path.Join(tempDir2, "temp", "test3.txt"))
		Expect(err).ToNot(HaveOccurred())
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
			err := os.WriteFile(file, []byte("fake_content"), 0o400)
			Expect(err).ShouldNot(HaveOccurred())
		}
		files, err := GetDirectoryContent(tempDir3)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(files).Should(ConsistOf(testFiles))
	})
})

var _ = Describe("function MoveDirectoryContent", func() {
	It("returns error if directory doesn't exist", func() {
		err := MoveDirectoryContent(filepath.Join(tempDir3, "not-exists"), filepath.Join(tempDir3, "not-exists"))
		Expect(err).Should(HaveOccurred())
	})
	It("moves files to a new directory recursively", func() {
		testFiles := make([]string, 10)
		sourceDir, err := os.MkdirTemp(os.TempDir(), "fileutils4_")
		Expect(err).ShouldNot(HaveOccurred())
		sourceSubDir, err := os.MkdirTemp(sourceDir, "fileutils44_")
		Expect(err).ShouldNot(HaveOccurred())
		diff := strings.TrimPrefix(sourceSubDir, sourceDir)
		Expect(diff).ShouldNot(BeEmpty())
		for i := 0; i < 10; i++ {
			testFiles[i] = fmt.Sprintf("test_file_%v", i)
			file := filepath.Join(sourceDir, testFiles[i])
			err := os.WriteFile(file, []byte("fake_content"), 0o400)
			Expect(err).ShouldNot(HaveOccurred())
		}
		// put files in subdirectory
		subTestFiles := make([]string, 4)
		for i := 0; i < 4; i++ {
			subTestFiles[i] = fmt.Sprintf("sub_test_file_%v", i)
			file := filepath.Join(sourceSubDir, subTestFiles[i])
			err := os.WriteFile(file, []byte("fake_content"), 0o400)
			Expect(err).ShouldNot(HaveOccurred())
		}
		files, err := GetDirectoryContent(sourceDir)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(files).Should(ContainElements(testFiles))
		subFiles, err := GetDirectoryContent(sourceSubDir)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(subFiles).Should(ConsistOf(subTestFiles))

		// move to a new location
		newDir, err := os.MkdirTemp(os.TempDir(), "fileutils5_")
		Expect(err).ShouldNot(HaveOccurred())
		err = MoveDirectoryContent(sourceDir, newDir)
		Expect(err).ShouldNot(HaveOccurred())

		// check contents in new location
		movedFiles, err := GetDirectoryContent(newDir)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(movedFiles).Should(ContainElements(testFiles))
		movedSubFiles, err := GetDirectoryContent(newDir + diff)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(movedSubFiles).Should(ConsistOf(subTestFiles))

		// check the original directory is empty
		files, err = GetDirectoryContent(sourceDir)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(files).Should(BeEmpty())
	})
})
