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

package walrestore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// ErrStreamingRetry is returned when the WAL restore fails on purpose, to let
// a standby in continuous archive recovery try streaming replication.
// PostgreSQL moves to streaming only when restore_command fails, which never
// happens while the archive keeps up with the standby.
var ErrStreamingRetry = errors.New("WAL restore failed on purpose to retry streaming replication")

const (
	// streamingRetryInterval is how long a standby restores WAL files from
	// the archive before being pushed to try streaming replication
	streamingRetryInterval = 30 * time.Second

	// streamingRetryMaxInterval caps the interval, which doubles every time
	// streaming replication does not hold
	streamingRetryMaxInterval = 5 * time.Minute

	// streamingRetryFailFor is how long the WAL restores fail once the
	// interval has expired. Since PostgreSQL 15, a restore that fails while
	// prefetching WAL is retried before any other source is tried, so a
	// single failure is not enough
	streamingRetryFailFor = 5 * time.Second

	// streamingRetryIdleGap is the pause between two restores that tells the
	// standby has been streaming (or idle) in the meantime. It is as long as
	// the maximum interval, so that a slow connection attempt to the primary
	// is not mistaken for streaming
	streamingRetryIdleGap = streamingRetryMaxInterval
)

// streamingRetryStateFile stores the streamingRetryState, as every WAL restore
// runs in its own process
var streamingRetryStateFile = filepath.Join(postgres.ScratchDataDirectory, "wal-restore-streaming-retry.json")

// streamingRetrySpoolDirectory is where the WAL files prefetched by a restore
// wait to be requested
var streamingRetrySpoolDirectory = SpoolDirectory

// streamingRetryState tracks the WAL files a standby restores from the archive
type streamingRetryState struct {
	// WindowStart is the time of the first restore of the current interval
	WindowStart time.Time `json:"windowStart"`

	// LastSeen is the time of the latest restore
	LastSeen time.Time `json:"lastSeen"`

	// Interval is the current interval
	Interval time.Duration `json:"interval"`

	// FailUntil, when set, is the end of the forced failures
	FailUntil time.Time `json:"failUntil"`
}

// next returns the state after a restore at the given time, and whether the
// restore must fail
func (s streamingRetryState) next(now time.Time) (streamingRetryState, bool) {
	if now.Sub(s.LastSeen) >= streamingRetryIdleGap {
		s = streamingRetryState{WindowStart: now, Interval: streamingRetryInterval}
	}
	s.LastSeen = now

	switch {
	case now.Before(s.FailUntil):
		return s, true

	case !s.FailUntil.IsZero():
		// The standby is back on the archive soon after the forced failures:
		// streaming replication did not hold, wait longer before the next try
		s.FailUntil = time.Time{}
		s.WindowStart = now
		s.Interval = min(2*s.Interval, streamingRetryMaxInterval)
		return s, false

	case now.Sub(s.WindowStart) >= s.Interval:
		s.FailUntil = now.Add(streamingRetryFailFor)
		return s, true
	}

	return s, false
}

// checkStreamingRetry returns ErrStreamingRetry when the requested WAL file
// must not be restored, to let a standby in continuous archive recovery try
// streaming replication. It never fails outside standby mode, where a failed
// restore ends the recovery, and it never fails on errors.
func checkStreamingRetry(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podName string,
	pgData string,
	walName string,
	rewindMode bool,
) error {
	if rewindMode || !postgres.IsWALFile(walName) || !isStreamingAvailable(cluster, podName) {
		return nil
	}

	// This instance is being promoted, and must first replay the WAL files in
	// the archive. The instance manager reads the cluster from the same cache
	// before running pg_ctl promote
	if cluster.Status.TargetPrimary == podName && !cluster.IsReplica() {
		return nil
	}

	// The cluster status can be stale: PostgreSQL is in standby mode as long as
	// standby.signal exists, and removes it at the end of the recovery
	if isStandby, err := fileutils.FileExists(filepath.Join(pgData, "standby.signal")); err != nil || !isStandby {
		return nil
	}

	// The in-tree restorer and the barman-cloud plugin share the spool. Failing a
	// WAL file in it would leave it there, as a streaming standby never requests
	// it again. This comes before the state update, so that the failures start
	// with the next WAL file
	if spooled, err := fileutils.FileExists(filepath.Join(streamingRetrySpoolDirectory, walName)); err != nil || spooled {
		return nil
	}

	var state streamingRetryState
	if content, err := fileutils.ReadFile(streamingRetryStateFile); err == nil {
		// An unreadable state starts a new interval
		_ = json.Unmarshal(content, &state)
	}
	state, fail := state.next(time.Now())

	content, _ := json.Marshal(state)
	if err := os.WriteFile(streamingRetryStateFile, content, 0o600); err != nil {
		// Without a state, every restore would fail
		log.FromContext(ctx).Warning("Cannot store the streaming retry state", "error", err)
		return nil
	}
	if !fail {
		return nil
	}

	// A promotion waits for a restore to fail, and would then skip the WAL
	// files still in the archive. This is checked last, as pg_ctl promote can
	// run at any time
	if promoting, err := fileutils.FileExists(filepath.Join(pgData, "promote")); err != nil || promoting {
		return nil
	}

	log.FromContext(ctx).Info(
		"Failing the WAL restore on purpose to let PostgreSQL try streaming replication",
		"walName", walName,
		"interval", state.Interval,
		"failUntil", state.FailUntil)
	return ErrStreamingRetry
}
