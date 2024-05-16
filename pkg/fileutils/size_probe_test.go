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
	"context"
	"fmt"
	"os"
	"path"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Size probe functions", func() {
	testFileName := path.Join(tempDir1, "_test_")

	AfterEach(func() {
		err := RemoveFile(testFileName)
		Expect(err).ToNot(HaveOccurred())
	})

	It("creates a file with a specific size", func(ctx SpecContext) {
		expectedSize := createFileBlockSize + 400

		err := createFileWithSize(ctx, testFileName, expectedSize)
		Expect(err).ToNot(HaveOccurred())

		info, err := os.Stat(testFileName)
		Expect(err).ToNot(HaveOccurred())
		Expect(int(info.Size())).To(Equal(expectedSize))
	})

	It("can create an empty file", func(ctx SpecContext) {
		err := createFileWithSize(ctx, testFileName, 0)
		Expect(err).ToNot(HaveOccurred())

		info, err := os.Stat(testFileName)
		Expect(err).ToNot(HaveOccurred())
		Expect(int(info.Size())).To(Equal(0))
	})

	It("can detect free space in a directory", func(ctx SpecContext) {
		result, err := NewDirectory(tempDir1).HasSpaceInDirectory(ctx, 100)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("errors out when the directory doesn't exist", func(ctx SpecContext) {
		result, err := NewDirectory(path.Join(tempDir1, "_not_existing_")).HasSpaceInDirectory(ctx, 100)
		Expect(err).To(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("can detect when there is no more free space in a directory", func(ctx SpecContext) {
		creatorFunction := func(_ context.Context, _ string, _ int) error {
			return &os.PathError{
				Err: syscall.ENOSPC,
			}
		}

		dir := NewDirectory(tempDir1)
		dir.createFileFunc = creatorFunction
		result, err := dir.HasSpaceInDirectory(ctx, 100)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeFalse())
	})
})

var _ = Describe("ENOSPC error checking", func() {
	It("does not detect a nil error as ENOSPC", func() {
		Expect(IsNoSpaceLeftOnDevice(nil)).To(BeFalse())
	})

	It("does not detect a generic error as ENOSPC", func() {
		Expect(IsNoSpaceLeftOnDevice(fmt.Errorf("a generic error"))).To(BeFalse())
	})

	It("detects ENOSPC errors", func() {
		testError := &os.PathError{
			Err: syscall.ENOSPC,
		}
		Expect(IsNoSpaceLeftOnDevice(testError)).To(BeTrue())
	})

	It("detects ENOSPC errors when they're wrapped in other errors", func() {
		var testError error
		testError = &os.PathError{
			Err: syscall.ENOSPC,
		}
		testError = fmt.Errorf("something bad happened: %w", testError)
		Expect(IsNoSpaceLeftOnDevice(testError)).To(BeTrue())
	})
})
