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
	"errors"
	"fmt"
	"io/fs"
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

	It("removes the directory", func() {
		var err error
		var result bool

		testDir, err := os.MkdirTemp(tempDir2, "testDir")
		Expect(err).ToNot(HaveOccurred())

		changed, err := WriteStringToFile(path.Join(testDir, "file.txt"), "this is a test file")
		Expect(changed).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		result, err = FileExists(path.Join(testDir, "file.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeTrue())

		err = RemoveDirectory(testDir)
		Expect(err).ToNot(HaveOccurred())

		result, err = FileExists(path.Join(testDir, "file.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("fails when the directory to be removed doesn't exist", func() {
		err := RemoveDirectory(path.Join(tempDir2, "not-existing"))
		Expect(err).To(MatchError(os.IsNotExist, "is not exists"))
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

var _ = Describe("RemoveFiles", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "test")
		Expect(err).NotTo(HaveOccurred())

		// Create some sample files and directories
		Expect(os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("test"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tempDir, "file2.txt"), []byte("test"), 0o600)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(tempDir, "dir1"), 0o750)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tempDir, "dir1", "file3.txt"), []byte("test"), 0o600)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(tempDir, "pg_replslot"), 0o750)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(tempDir, "pg_replslot", "myrepslot"), 0o750)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tempDir, "pg_replslot", "myrepslot", "state"), []byte("test"), 0o600)).To(Succeed())
	})

	AfterEach(func() {
		// Cleanup
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	It("removes specified files and directories", func(ctx SpecContext) {
		// Use the RemoveFiles function
		err := RemoveFiles(ctx, tempDir, []string{
			"file1.txt",
			"dir1/*",
			"non_existent_dir/*",
			"pg_replslot/*",
		})
		Expect(err).NotTo(HaveOccurred())

		// Assert files and directories are removed as expected
		_, err = os.Stat(filepath.Join(tempDir, "file1.txt"))
		Expect(os.IsNotExist(err)).To(BeTrue(), "Expected file1.txt to be removed")

		_, err = os.Stat(filepath.Join(tempDir, "file2.txt"))
		Expect(err).NotTo(HaveOccurred(), "Expected file2.txt to not be removed")

		_, err = os.Stat(filepath.Join(tempDir, "dir1", "file3.txt"))
		Expect(os.IsNotExist(err)).To(BeTrue(), "Expected dir1/file3.txt to be removed")

		_, err = os.Stat(filepath.Join(tempDir, "pg_replslot", "myrepslot", "state"))
		Expect(os.IsNotExist(err)).To(BeTrue(), "Expected pg_replslot/myrepslot/state to be removed")

		_, err = os.Stat(filepath.Join(tempDir, "pg_replslot", "myrepslot"))
		Expect(os.IsNotExist(err)).To(BeTrue(), "Expected pg_replslot/myrepslot directory to be removed")

		_, err = os.Stat(filepath.Join(tempDir, "dir1"))
		Expect(err).NotTo(HaveOccurred(), "Expected dir1 to not be removed")
	})
})

var _ = Describe("RemoveRestoreExcludedFiles", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "test")
		Expect(err).NotTo(HaveOccurred())

		// Create temporary directories and files
		for _, path := range excludedPathsFromRestore {
			fullPath := filepath.Join(tempDir, path)
			if len(path) >= 2 && path[len(path)-2:] == "/*" {
				dirToCreate := fullPath[:len(fullPath)-2]
				Expect(os.Mkdir(dirToCreate, 0o750)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(dirToCreate, "testfile.txt"), []byte("test"), 0o600)).To(Succeed())
				continue
			}
			// Ensure directory exists before creating the file
			dirOfTheFile := filepath.Dir(fullPath)
			if _, err := os.Stat(dirOfTheFile); os.IsNotExist(err) {
				Expect(os.MkdirAll(dirOfTheFile, 0o750)).To(Succeed())
			}
			Expect(os.WriteFile(fullPath, []byte("test"), 0o600)).To(Succeed())

		}
	})

	AfterEach(func() {
		_ = os.RemoveAll(tempDir)
	})

	It("should correctly remove specified files and directories", func(ctx SpecContext) {
		Expect(RemoveRestoreExcludedFiles(ctx, tempDir)).To(Succeed())

		for _, path := range excludedPathsFromRestore {
			fullPath := filepath.Join(tempDir, path)
			if len(path) >= 2 && path[len(path)-2:] == "/*" {
				_, err := os.Stat(filepath.Join(fullPath[:len(fullPath)-2], "testfile.txt"))
				Expect(os.IsNotExist(err)).To(BeTrue(), "Expected directory contents to be removed: "+fullPath)
			} else {
				_, err := os.Stat(fullPath)
				Expect(os.IsNotExist(err)).To(BeTrue(), "Expected file to be removed: "+fullPath)
			}
		}
	})
})

var _ = Describe("EnsureDirectoryExists", func() {
	var tempDir string
	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "test")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		// Cleanup
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})
	It("creates the directory with the right permissions", func() {
		newDir := filepath.Join(tempDir, "newDir")

		Expect(EnsureDirectoryExists(newDir)).To(Succeed())
		fileInfo2, err := os.Stat(newDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(fileInfo2.Mode().Perm()).To(Equal(fs.FileMode(0o700)))
	})
	It("errors out when it cannot create the directory", func() {
		err := EnsureDirectoryExists("/dev/foobar")
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, fs.ErrPermission)).To(BeTrue())
		pathErr, ok := err.(*os.PathError)
		Expect(ok).To(BeTrue())
		Expect(pathErr.Op).To(Equal("mkdir"))
	})
	It("errors out when Stat fails for other reasons", func() {
		err := EnsureDirectoryExists("illegalchar\x00")
		Expect(err).To(HaveOccurred())
		pathErr, ok := err.(*os.PathError)
		Expect(ok).To(BeTrue())
		Expect(pathErr.Op).To(Equal("stat"))
		Expect(err.Error()).To(ContainSubstring("invalid"))
	})
	It("ignores the permissions if the file already exists", func() {
		existingDir, err := os.MkdirTemp(tempDir, "existingDir")
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Chmod(existingDir, 0o600)).To(Succeed())

		Expect(EnsureDirectoryExists(existingDir)).To(Succeed())
		fileInfo2, err := os.Stat(existingDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(fileInfo2.Mode().Perm()).To(Equal(fs.FileMode(0o600)))
	})
})
