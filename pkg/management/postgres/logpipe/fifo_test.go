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

package logpipe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils/compatibility"
	"github.com/cloudnative-pg/machinery/pkg/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func isFifo(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeNamedPipe != 0
}

var _ = Describe("ensureLogFifo", func() {
	var (
		dir      string
		fileName string
		logger   log.Logger
	)

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		fileName = filepath.Join(dir, "postgres.json")
		logger = log.FromContext(context.Background())
	})

	When("no filesystem entry exists at the path", func() {
		It("creates a FIFO", func() {
			Expect(ensureLogFifo(logger, fileName)).To(Succeed())
			Expect(isFifo(fileName)).To(BeTrue())
		})
	})

	When("a FIFO already exists at the path", func() {
		It("succeeds without touching it", func() {
			Expect(ensureLogFifo(logger, fileName)).To(Succeed())
			info, err := os.Lstat(fileName)
			Expect(err).ToNot(HaveOccurred())

			Expect(ensureLogFifo(logger, fileName)).To(Succeed())
			infoAfter, err := os.Lstat(fileName)
			Expect(err).ToNot(HaveOccurred())
			Expect(infoAfter.ModTime()).To(Equal(info.ModTime()))
		})
	})

	When("a stale regular file is planted at the path (the #11201 scenario)", func() {
		It("reports ErrExistsNotFifo and removes the stale file, so a following call creates the FIFO", func() {
			Expect(os.WriteFile(fileName, []byte("not a fifo"), 0o600)).To(Succeed())

			err := ensureLogFifo(logger, fileName)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, compatibility.ErrExistsNotFifo)).To(BeTrue())

			_, statErr := os.Lstat(fileName)
			Expect(os.IsNotExist(statErr)).To(BeTrue())

			Expect(ensureLogFifo(logger, fileName)).To(Succeed())
			Expect(isFifo(fileName)).To(BeTrue())
		})
	})

	When("a symlink resolves to a genuine FIFO (Stat follows it)", func() {
		It("accepts it without removing anything, since the resolved path is a usable FIFO", func() {
			targetFifo := filepath.Join(dir, "target.fifo")
			Expect(ensureLogFifo(logger, targetFifo)).To(Succeed())
			Expect(isFifo(targetFifo)).To(BeTrue())

			symlinkPath := filepath.Join(dir, "postgres.json")
			Expect(os.Symlink(targetFifo, symlinkPath)).To(Succeed())

			// CreateFifo now resolves the path (os.Stat), matching the reader
			// that opens it, so a link to a real FIFO is a valid stream: keep it.
			Expect(ensureLogFifo(logger, symlinkPath)).To(Succeed())

			// both the symlink and its target survive untouched
			_, statErr := os.Lstat(symlinkPath)
			Expect(statErr).ToNot(HaveOccurred())
			Expect(isFifo(targetFifo)).To(BeTrue())
		})
	})

	When("a symlink resolves to a regular file (Stat follows it to a non-FIFO)", func() {
		It("reports ErrExistsNotFifo and removes the symlink, leaving the target untouched", func() {
			target := filepath.Join(dir, "target.txt")
			Expect(os.WriteFile(target, []byte("not a fifo"), 0o600)).To(Succeed())

			symlinkPath := filepath.Join(dir, "postgres.json")
			Expect(os.Symlink(target, symlinkPath)).To(Succeed())

			err := ensureLogFifo(logger, symlinkPath)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, compatibility.ErrExistsNotFifo)).To(BeTrue())

			// os.Remove drops the symlink name, not its target...
			_, statErr := os.Lstat(symlinkPath)
			Expect(os.IsNotExist(statErr)).To(BeTrue())

			// ...so the file it pointed at survives (os.Remove cannot have
			// touched the target through the link)
			_, statErr = os.Lstat(target)
			Expect(statErr).ToNot(HaveOccurred())
		})
	})

	When("the parent directory does not exist", func() {
		It("returns an error without attempting to remove anything", func() {
			missingParent := filepath.Join(dir, "does-not-exist", "postgres.json")
			err := ensureLogFifo(logger, missingParent)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, compatibility.ErrExistsNotFifo)).To(BeFalse())
		})
	})
})

var _ = Describe("waitBeforeRetry", func() {
	When("the context is already cancelled", func() {
		It("returns immediately", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			start := time.Now()
			waitBeforeRetry(ctx)
			Expect(time.Since(start)).To(BeNumerically("<", retryBackoff))
		})
	})

	When("the context is cancelled while waiting", func() {
		It("returns before the full backoff elapses", func() {
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				time.Sleep(10 * time.Millisecond)
				cancel()
			}()

			start := time.Now()
			waitBeforeRetry(ctx)
			Expect(time.Since(start)).To(BeNumerically("<", retryBackoff))
		})
	})
})
