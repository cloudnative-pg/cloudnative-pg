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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// storageCheckCacheDuration is how long a successful storage check result is cached
	// to avoid I/O spikes from concurrent probe requests.
	storageCheckCacheDuration = 2 * time.Second

	// writeProbeFileName is the temporary probe file name used to test volume writability.
	writeProbeFileName = ".cnpg_write_probe"
)

// CheckStorageWritability verifies that the instance's data volume (and WAL volume if separate)
// is responsive and writable. It writes a tiny probe file, syncs it to disk (fsync), closes,
// and deletes it. If the volume is hung (e.g. fsfreeze, hung storage controller, dead disk),
// the check times out and returns an error.
func (instance *Instance) CheckStorageWritability(ctx context.Context) error {
	cluster := instance.GetClusterOrDefault()
	if !cluster.IsDataVolumeCheckEnabled() {
		return nil
	}

	timeout := cluster.GetDataVolumeCheckTimeout()

	instance.storageCheckTracker.mu.Lock()
	if time.Since(instance.storageCheckTracker.lastChecked) < storageCheckCacheDuration {
		err := instance.storageCheckTracker.lastError
		instance.storageCheckTracker.mu.Unlock()
		return err
	}

	// If a check is already running and has been running for longer than the timeout,
	// the volume is hung. Fail fast without launching more stuck goroutines.
	if instance.storageCheckTracker.inFlight {
		if time.Since(instance.storageCheckTracker.startTime) >= timeout {
			instance.storageCheckTracker.mu.Unlock()
			return fmt.Errorf("storage writability check timed out after %s: volume is unresponsive/hung", timeout)
		}
		instance.storageCheckTracker.mu.Unlock()
		return instance.storageCheckTracker.lastError
	}

	instance.storageCheckTracker.inFlight = true
	instance.storageCheckTracker.startTime = time.Now()
	instance.storageCheckTracker.mu.Unlock()

	err := instance.executeStorageCheckWithTimeout(ctx, timeout)

	instance.storageCheckTracker.mu.Lock()
	instance.storageCheckTracker.inFlight = false
	instance.storageCheckTracker.lastChecked = time.Now()
	instance.storageCheckTracker.lastError = err
	instance.storageCheckTracker.mu.Unlock()

	return err
}

func (instance *Instance) executeStorageCheckWithTimeout(ctx context.Context, timeout time.Duration) error {
	if instance.PgData == "" {
		return nil
	}

	dirs := []string{instance.PgData}

	walDir := filepath.Join(instance.PgData, "pg_wal")
	if evalWal, err := filepath.EvalSymlinks(walDir); err == nil && evalWal != walDir && evalWal != instance.PgData {
		dirs = append(dirs, evalWal)
	}

	done := make(chan error, 1)
	go func() {
		for _, dir := range dirs {
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				continue
			}
			if err := testDirectoryWritability(dir); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("storage writability check timed out after %s: volume is unresponsive/hung", timeout)
	case <-ctx.Done():
		return fmt.Errorf("storage writability check interrupted: %w", ctx.Err())
	}
}

func testDirectoryWritability(dir string) error {
	probeFile := filepath.Join(dir, writeProbeFileName)
	f, err := os.OpenFile(probeFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open probe file in %s: %w", dir, err)
	}
	_, writeErr := f.Write([]byte("1"))
	syncErr := f.Sync()
	closeErr := f.Close()
	_ = os.Remove(probeFile)

	if writeErr != nil {
		return fmt.Errorf("failed to write probe file in %s: %w", dir, writeErr)
	}
	if syncErr != nil {
		return fmt.Errorf("failed to fsync probe file in %s: %w", dir, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close probe file in %s: %w", dir, closeErr)
	}
	return nil
}
