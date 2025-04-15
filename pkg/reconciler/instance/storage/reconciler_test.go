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

package storage

import (
	"io/fs"
	"os"
	"path"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WAL Storage reconciler", func() {
	var pgDataDir string
	var separateWALVolumeDir string
	var separateWALVolumeWALDir string
	var opts walDirectoryReconcilerOptions

	BeforeEach(func() {
		tempDir := GinkgoT().TempDir()
		pgDataDir = path.Join(tempDir, "pg_data")
		separateWALVolumeDir = path.Join(tempDir, "separate_wal")
		separateWALVolumeWALDir = path.Join(tempDir, "separate_wal", "pg_wal")

		opts = walDirectoryReconcilerOptions{
			pgWalDirectory:        path.Join(pgDataDir, "pg_wal"),
			walVolumeDirectory:    separateWALVolumeDir,
			walVolumeWalDirectory: separateWALVolumeWALDir,
		}
	})

	It("will not error out if a separate WAL storage doesn't exist", func(ctx SpecContext) {
		err := internalReconcileWalDirectory(ctx, opts)
		Expect(err).ToNot(HaveOccurred())
	})

	It("won't change anything if pg_wal is already a symlink", func(ctx SpecContext) {
		err := fileutils.EnsureDirectoryExists(pgDataDir)
		Expect(err).ToNot(HaveOccurred())

		err = fileutils.EnsureDirectoryExists(separateWALVolumeDir)
		Expect(err).ToNot(HaveOccurred())

		err = os.Symlink(separateWALVolumeDir, opts.pgWalDirectory)
		Expect(err).ToNot(HaveOccurred())

		err = internalReconcileWalDirectory(ctx, opts)
		Expect(err).ToNot(HaveOccurred())
	})

	It("moves the existing WALs to the target volume", func(ctx SpecContext) {
		wal1 := path.Join(opts.pgWalDirectory, "000000010000000100000001")
		wal2 := path.Join(opts.pgWalDirectory, "000000010000000100000002")
		wal3 := path.Join(opts.pgWalDirectory, "000000010000000100000003")

		By("creating a pg_wal directory and a separate WAL volume directory", func() {
			err := fileutils.EnsureDirectoryExists(opts.pgWalDirectory)
			Expect(err).ToNot(HaveOccurred())

			err = fileutils.EnsureDirectoryExists(separateWALVolumeDir)
			Expect(err).ToNot(HaveOccurred())
		})

		By("creating a few WALs file in pg_wal", func() {
			_, err := fileutils.WriteStringToFile(wal1, "wal content")
			Expect(err).ToNot(HaveOccurred())

			_, err = fileutils.WriteStringToFile(wal2, "wal content")
			Expect(err).ToNot(HaveOccurred())

			_, err = fileutils.WriteStringToFile(wal3, "wal content")
			Expect(err).ToNot(HaveOccurred())
		})

		By("reconciling the WALs to the target volume", func() {
			err := internalReconcileWalDirectory(ctx, opts)
			Expect(err).ToNot(HaveOccurred())
		})

		By("checking if pg_wal is a symlink", func() {
			pgWalDirInfo, err := os.Lstat(opts.pgWalDirectory)
			Expect(err).ToNot(HaveOccurred())
			Expect(pgWalDirInfo.Mode().Type()).To(Equal(fs.ModeSymlink))
		})

		By("checking the WAL files are in the target volume", func() {
			Expect(fileutils.FileExists(wal1)).To(BeTrue())
			Expect(fileutils.FileExists(wal2)).To(BeTrue())
			Expect(fileutils.FileExists(wal3)).To(BeTrue())
		})
	})
})
