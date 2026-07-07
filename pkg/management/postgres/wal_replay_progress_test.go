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

package postgres

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WAL replay progress tracking", func() {
	var (
		tracker *walReplayProgress
		now     time.Time
	)

	BeforeEach(func() {
		tracker = &walReplayProgress{}
		now = time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	})

	Context("progress freshness", func() {
		It("is not progressing when no progress was ever observed", func() {
			Expect(tracker.isProgressing(now)).To(BeFalse())
		})

		It("is progressing after progress was observed within the window", func() {
			tracker.markProgress(now)
			Expect(tracker.isProgressing(now.Add(walReplayProgressWindow))).To(BeTrue())
		})

		It("is not progressing when the last observation is older than the window", func() {
			tracker.markProgress(now)
			Expect(tracker.isProgressing(now.Add(walReplayProgressWindow + time.Second))).To(BeFalse())
		})
	})

	Context("control file position observations", func() {
		It("does not record progress on the first observation", func() {
			tracker.observePosition("0/3000000|0/2000028", now)
			Expect(tracker.isProgressing(now)).To(BeFalse())
		})

		It("does not record progress when the position did not change", func() {
			tracker.observePosition("0/3000000|0/2000028", now)
			tracker.observePosition("0/3000000|0/2000028", now.Add(time.Minute))
			Expect(tracker.isProgressing(now.Add(time.Minute))).To(BeFalse())
		})

		It("records progress when the position changed", func() {
			tracker.observePosition("0/3000000|0/2000028", now)
			tracker.observePosition("0/4000000|0/2000028", now.Add(time.Minute))
			Expect(tracker.isProgressing(now.Add(time.Minute))).To(BeTrue())
		})

		It("records progress when only the checkpoint REDO location changed", func() {
			tracker.observePosition("0/3000000|0/2000028", now)
			tracker.observePosition("0/3000000|0/2FFFFD8", now.Add(time.Minute))
			Expect(tracker.isProgressing(now.Add(time.Minute))).To(BeTrue())
		})

		It("ignores empty positions", func() {
			tracker.observePosition("0/3000000|0/2000028", now)
			tracker.observePosition("", now.Add(time.Minute))
			tracker.observePosition("0/3000000|0/2000028", now.Add(2*time.Minute))
			Expect(tracker.isProgressing(now.Add(2 * time.Minute))).To(BeFalse())
		})
	})

	Context("control file sampling rate limit", func() {
		It("allows the first sample", func() {
			Expect(tracker.shouldSample(now)).To(BeTrue())
		})

		It("denies a sample taken too early", func() {
			Expect(tracker.shouldSample(now)).To(BeTrue())
			Expect(tracker.shouldSample(now.Add(walReplayMinSampleInterval - time.Second))).To(BeFalse())
		})

		It("allows a sample after the minimum interval", func() {
			Expect(tracker.shouldSample(now)).To(BeTrue())
			Expect(tracker.shouldSample(now.Add(walReplayMinSampleInterval))).To(BeTrue())
		})

		It("denies any sample once replay completed", func() {
			tracker.markCompleted()
			Expect(tracker.shouldSample(now)).To(BeFalse())
		})
	})

	Context("stall detection", func() {
		const timeout = time.Hour

		It("is not stalled when the startup probe was never skipped", func() {
			tracker.markProgress(now)
			Expect(tracker.isStalled(now.Add(2*timeout), timeout)).To(BeFalse())
		})

		It("is not stalled while progress is more recent than the timeout", func() {
			tracker.markProgress(now)
			tracker.markStartupSkipped()
			Expect(tracker.isStalled(now.Add(timeout), timeout)).To(BeFalse())
		})

		It("is stalled when progress stopped for longer than the timeout", func() {
			tracker.markProgress(now)
			tracker.markStartupSkipped()
			Expect(tracker.isStalled(now.Add(timeout+time.Second), timeout)).To(BeTrue())
		})

		It("is not stalled once replay completed", func() {
			tracker.markProgress(now)
			tracker.markStartupSkipped()
			tracker.markCompleted()
			Expect(tracker.isStalled(now.Add(2*timeout), timeout)).To(BeFalse())
		})
	})

	Context("replay position from the startup process title", func() {
		var procPath string

		// addProcess creates a fake proc filesystem entry whose cmdline
		// contains the passed process title, using NUL separators as the
		// real proc filesystem does
		addProcess := func(pid string, title string) {
			pidPath := filepath.Join(procPath, pid)
			Expect(os.MkdirAll(pidPath, 0o750)).To(Succeed())
			cmdline := append([]byte(title), 0)
			Expect(os.WriteFile(filepath.Join(pidPath, "cmdline"), cmdline, 0o600)).To(Succeed())
		}

		BeforeEach(func() {
			procPath = GinkgoT().TempDir()
		})

		It("extracts the segment being replayed from the startup process title", func() {
			addProcess("1", "/controller/manager instance run")
			addProcess("42", "postgres: startup recovering 000000010000000000000003")
			addProcess("43", "postgres: walwriter")
			Expect(currentWALReplaySegment(procPath)).To(Equal("000000010000000000000003"))
		})

		It("extracts the segment when cluster_name is part of the title", func() {
			addProcess("42", "postgres: cluster-example: startup recovering 000000010000000000000003")
			Expect(currentWALReplaySegment(procPath)).To(Equal("000000010000000000000003"))
		})

		It("returns an empty position when no process is replaying WAL", func() {
			addProcess("1", "/controller/manager instance run")
			addProcess("43", "postgres: walwriter")
			Expect(currentWALReplaySegment(procPath)).To(BeEmpty())
		})

		It("returns an empty position while the startup process is waiting for WAL", func() {
			addProcess("42", "postgres: startup waiting for 000000010000000000000003")
			Expect(currentWALReplaySegment(procPath)).To(BeEmpty())
		})

		It("ignores non PostgreSQL processes mentioning a similar title", func() {
			addProcess("42", "grep startup recovering 000000010000000000000003")
			Expect(currentWALReplaySegment(procPath)).To(BeEmpty())
		})

		It("ignores non numeric proc entries and processes without a cmdline", func() {
			Expect(os.MkdirAll(filepath.Join(procPath, "sys"), 0o750)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(procPath, "77"), 0o750)).To(Succeed())
			Expect(currentWALReplaySegment(procPath)).To(BeEmpty())
		})
	})

	Context("state persistence", func() {
		var stateFile string

		BeforeEach(func() {
			stateFile = filepath.Join(GinkgoT().TempDir(), "state.json")
		})

		It("latches each transition only once", func() {
			Expect(tracker.markStartupSkipped()).To(BeTrue())
			Expect(tracker.markStartupSkipped()).To(BeFalse())
			Expect(tracker.markCompleted()).To(BeTrue())
			Expect(tracker.markCompleted()).To(BeFalse())
		})

		It("restores a startup skip latch, restarting the stall clock", func() {
			tracker.load(stateFile, now)
			tracker.markProgress(now)
			tracker.markStartupSkipped()

			loadTime := now.Add(30 * time.Minute)
			restored := &walReplayProgress{}
			restored.load(stateFile, loadTime)

			Expect(restored.isStartupSkipped()).To(BeTrue())
			Expect(restored.isCompleted()).To(BeFalse())
			Expect(restored.isStalled(loadTime.Add(time.Hour), time.Hour)).To(BeFalse())
			Expect(restored.isStalled(loadTime.Add(time.Hour+time.Second), time.Hour)).To(BeTrue())
		})

		It("restores a completed latch without arming stall detection", func() {
			tracker.load(stateFile, now)
			tracker.markStartupSkipped()
			tracker.markCompleted()

			restored := &walReplayProgress{}
			restored.load(stateFile, now.Add(time.Hour))

			Expect(restored.isCompleted()).To(BeTrue())
			Expect(restored.isStalled(now.Add(48*time.Hour), time.Hour)).To(BeFalse())
		})

		It("starts from a clean state when the file is missing", func() {
			tracker.load(stateFile, now)
			Expect(tracker.isStartupSkipped()).To(BeFalse())
			Expect(tracker.isCompleted()).To(BeFalse())
		})

		It("discards an unreadable state file", func() {
			Expect(os.WriteFile(stateFile, []byte("not json"), 0o600)).To(Succeed())
			tracker.load(stateFile, now)
			Expect(tracker.isStartupSkipped()).To(BeFalse())
			Expect(tracker.isCompleted()).To(BeFalse())
		})

		It("does not write any file when persistence is not configured", func() {
			dir := GinkgoT().TempDir()
			tracker.markStartupSkipped()
			entries, err := os.ReadDir(dir)
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(BeEmpty())
		})
	})

	Context("instance level API", func() {
		It("exposes progress marking and freshness", func() {
			instance := &Instance{}
			Expect(instance.IsWALReplayProgressing()).To(BeFalse())

			instance.MarkWALReplayProgress()
			Expect(instance.IsWALReplayProgressing()).To(BeTrue())
		})

		It("latches startup skip and completion", func() {
			instance := &Instance{}
			Expect(instance.StartupSkippedForWALReplay()).To(BeFalse())
			Expect(instance.WALReplayCompleted()).To(BeFalse())

			instance.MarkStartupSkippedForWALReplay()
			instance.MarkWALReplayCompleted()
			Expect(instance.StartupSkippedForWALReplay()).To(BeTrue())
			Expect(instance.WALReplayCompleted()).To(BeTrue())
		})
	})
})
