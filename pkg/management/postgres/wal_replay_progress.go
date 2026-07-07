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
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// DefaultWALSegmentSize is the default WAL segment size of PostgreSQL,
// used when the actual segment size cannot be read from the control file
const DefaultWALSegmentSize = int64(16 * 1024 * 1024)

const (
	// DefaultWALReplayProgressWindow is how long an observed sign of WAL
	// replay progress keeps the startup probe from failing, unless
	// configured otherwise. The replay position advances at WAL segment
	// granularity (16MB by default), so the window has to be longer than
	// the time PostgreSQL may reasonably spend replaying a single
	// segment.
	DefaultWALReplayProgressWindow = 5 * time.Minute

	// walReplayMinSampleInterval limits how often we scan the processes
	// to sample the replay position.
	walReplayMinSampleInterval = 5 * time.Second

	// procDirectory is where the proc filesystem is mounted
	procDirectory = "/proc"
)

// walReplayProgress tracks evidence that PostgreSQL is still making
// progress replaying WAL while it is not accepting connections yet. It is
// consulted by the startup probe to avoid failing (and having the Pod
// killed) during a long but healthy recovery, and by the liveness probe to
// detect a stalled recovery after the startup probe has been reported as
// passed.
type walReplayProgress struct {
	mu sync.Mutex

	// lastProgressAt is the last time we observed an advancement of the
	// replay position
	lastProgressAt time.Time

	// lastPosition is the last replay position sampled from the startup
	// process title
	lastPosition string

	// lastSampleAt is the last time we sampled the replay position
	lastSampleAt time.Time

	// startupSkipped records that the startup probe was reported as
	// passed because WAL replay was in progress
	startupSkipped bool

	// completed records that PostgreSQL started accepting connections,
	// after which replay progress tracking is no longer needed
	completed bool

	// walSegmentSize caches the WAL segment size read from the control
	// file, zero until the first successful read
	walSegmentSize int64

	// stateFile is where the one-shot latches are persisted so that they
	// survive an in-place restart of the instance manager (online
	// upgrade): the kubelet keeps its startup probe state across the
	// re-exec, so we have to keep ours too. Empty disables persistence.
	stateFile string
}

// walReplayProgressState is the persisted part of walReplayProgress
type walReplayProgressState struct {
	StartupSkipped bool `json:"startupSkipped,omitempty"`
	Completed      bool `json:"completed,omitempty"`
}

// persist saves the one-shot latches to the state file. It must be called
// with the mutex held. Errors are logged and ignored: without persistence
// the latches simply behave as before, in memory only.
func (w *walReplayProgress) persist() {
	if w.stateFile == "" {
		return
	}

	content, err := json.Marshal(walReplayProgressState{
		StartupSkipped: w.startupSkipped,
		Completed:      w.completed,
	})
	if err != nil {
		log.Warning("Error while encoding the WAL replay progress state", "err", err.Error())
		return
	}

	tempFile := w.stateFile + ".tmp"
	if err := os.WriteFile(tempFile, content, 0o600); err != nil {
		log.Warning("Error while writing the WAL replay progress state", "err", err.Error())
		return
	}
	if err := os.Rename(tempFile, w.stateFile); err != nil {
		log.Warning("Error while saving the WAL replay progress state", "err", err.Error())
	}
}

// load restores the one-shot latches from the state file, if present.
// When a startup probe skip was latched by a previous instance manager
// process, the stall clock restarts from now: a progressing instance gets
// a full fresh timeout, while a stuck one is still detected.
func (w *walReplayProgress) load(stateFile string, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.stateFile = stateFile

	content, err := os.ReadFile(stateFile) //nolint:gosec
	if err != nil {
		return
	}

	var state walReplayProgressState
	if err := json.Unmarshal(content, &state); err != nil {
		log.Warning("Discarding unreadable WAL replay progress state", "err", err.Error())
		return
	}

	w.startupSkipped = state.StartupSkipped
	w.completed = state.Completed
	if w.startupSkipped && !w.completed {
		w.lastProgressAt = now
	}
}

func (w *walReplayProgress) markProgress(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastProgressAt = now
}

// observePosition compares a sampled replay position with the previous
// sample, and records progress when it changed. A single sample carries no
// evidence of progress, so the first observation only stores the position.
func (w *walReplayProgress) observePosition(position string, now time.Time) {
	if position == "" {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.lastPosition == position {
		return
	}
	if w.lastPosition != "" {
		w.lastProgressAt = now
	}
	w.lastPosition = position
}

// shouldSample rate-limits replay position sampling, returning true at
// most once every walReplayMinSampleInterval
func (w *walReplayProgress) shouldSample(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.completed {
		return false
	}
	if !w.lastSampleAt.IsZero() && now.Sub(w.lastSampleAt) < walReplayMinSampleInterval {
		return false
	}
	w.lastSampleAt = now
	return true
}

func (w *walReplayProgress) isProgressing(now time.Time, window time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.lastProgressAt.IsZero() && now.Sub(w.lastProgressAt) <= window
}

// markCompleted latches the completed flag, returning true on the first
// transition
func (w *walReplayProgress) markCompleted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.completed {
		return false
	}
	w.completed = true
	w.persist()
	return true
}

func (w *walReplayProgress) isCompleted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.completed
}

// markStartupSkipped latches the startup skip flag, returning true on the
// first transition
func (w *walReplayProgress) markStartupSkipped() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.startupSkipped {
		return false
	}
	w.startupSkipped = true
	w.persist()
	return true
}

func (w *walReplayProgress) isStartupSkipped() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.startupSkipped
}

// isStalled tells whether replay stopped making progress for longer than
// the passed timeout after the startup probe was reported as passed. It
// can only happen while PostgreSQL never started accepting connections.
func (w *walReplayProgress) isStalled(now time.Time, timeout time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.startupSkipped || w.completed {
		return false
	}
	return now.Sub(w.lastProgressAt) > timeout
}

// walReplayTitleRegex extracts the name of the WAL segment being replayed
// from the title of the PostgreSQL startup process. The startup process
// sets its title to "recovering <segment>" every time it opens a WAL
// segment, whether it comes from pg_wal, streaming replication or the WAL
// archive, so the extracted segment name is a replay position advancing
// at segment granularity in every form of recovery, on any PostgreSQL
// version and regardless of the log messages locale. The title is not
// maintained when update_process_title is disabled, in which case no
// replay progress is ever detected and the startup probe behaves as if
// this feature did not exist.
var walReplayTitleRegex = regexp.MustCompile(`\bstartup recovering ([0-9A-F]+)`)

// currentWALReplaySegment returns the WAL segment the PostgreSQL startup
// process is replaying, by scanning the titles of the processes found in
// the passed proc filesystem. It returns an empty string when no process
// is replaying WAL, including while the startup process is waiting for
// the next segment to become available.
func currentWALReplaySegment(procPath string) string {
	entries, err := os.ReadDir(procPath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}

		rawCmdline, err := os.ReadFile(filepath.Join(procPath, entry.Name(), "cmdline")) //nolint:gosec
		if err != nil {
			continue
		}

		cmdline := string(bytes.ReplaceAll(rawCmdline, []byte{0}, []byte{' '}))
		if !strings.HasPrefix(cmdline, "postgres: ") {
			continue
		}

		if matches := walReplayTitleRegex.FindStringSubmatch(cmdline); matches != nil {
			return matches[1]
		}
	}

	return ""
}

// MarkWALReplayProgress records evidence that WAL replay is progressing
func (instance *Instance) MarkWALReplayProgress() {
	instance.walReplayProgress.markProgress(time.Now())
}

// MarkWALReplayCompleted records that PostgreSQL started accepting
// connections, disarming WAL replay progress tracking. It returns true
// the first time the transition happens.
func (instance *Instance) MarkWALReplayCompleted() bool {
	return instance.walReplayProgress.markCompleted()
}

// WALReplayCompleted tells whether PostgreSQL started accepting
// connections at some point during the life of this process
func (instance *Instance) WALReplayCompleted() bool {
	return instance.walReplayProgress.isCompleted()
}

// SampleWALReplayPosition samples the current replay position from the
// title of the PostgreSQL startup process, recording progress when it
// advanced since the previous sample. Sampling is rate-limited, so it is
// safe to invoke it on every probe request.
func (instance *Instance) SampleWALReplayPosition() {
	now := time.Now()
	if !instance.walReplayProgress.shouldSample(now) {
		return
	}

	instance.walReplayProgress.observePosition(currentWALReplaySegment(procDirectory), now)
}

// IsWALReplayProgressing tells whether we observed evidence of WAL replay
// progress within the passed window
func (instance *Instance) IsWALReplayProgressing(window time.Duration) bool {
	return instance.walReplayProgress.isProgressing(time.Now(), window)
}

// MarkStartupSkippedForWALReplay records that the startup probe was
// reported as passed because WAL replay was in progress. It returns true
// the first time the transition happens.
func (instance *Instance) MarkStartupSkippedForWALReplay() bool {
	return instance.walReplayProgress.markStartupSkipped()
}

// LoadWALReplayProgressState restores the WAL replay probe latches
// persisted by a previous instance manager process of this Pod, and
// enables their persistence for this process
func (instance *Instance) LoadWALReplayProgressState() {
	instance.walReplayProgress.load(
		filepath.Join(postgres.ScratchDataDirectory, "wal-replay-progress.json"),
		time.Now(),
	)
}

// StartupSkippedForWALReplay tells whether the startup probe was reported
// as passed because WAL replay was in progress
func (instance *Instance) StartupSkippedForWALReplay() bool {
	return instance.walReplayProgress.isStartupSkipped()
}

// IsWALReplayStalledFor tells whether WAL replay stopped making progress
// for longer than the passed timeout after the startup probe was reported
// as passed
func (instance *Instance) IsWALReplayStalledFor(timeout time.Duration) bool {
	return instance.walReplayProgress.isStalled(time.Now(), timeout)
}

// WALSegmentSize returns the WAL segment size of the instance read from
// the control file, or DefaultWALSegmentSize when it cannot be
// determined. The value is cached after the first successful read.
func (instance *Instance) WALSegmentSize() int64 {
	instance.walReplayProgress.mu.Lock()
	cached := instance.walReplayProgress.walSegmentSize
	instance.walReplayProgress.mu.Unlock()
	if cached != 0 {
		return cached
	}

	out, err := instance.GetPgControldata()
	if err != nil {
		log.Debug("Error while reading the WAL segment size", "err", err.Error())
		return DefaultWALSegmentSize
	}

	size, err := utils.ParsePgControldataOutput(out).GetBytesPerWALSegment()
	if err != nil {
		log.Debug("Error while parsing the WAL segment size", "err", err.Error())
		return DefaultWALSegmentSize
	}

	instance.walReplayProgress.mu.Lock()
	instance.walReplayProgress.walSegmentSize = int64(size)
	instance.walReplayProgress.mu.Unlock()
	return int64(size)
}
