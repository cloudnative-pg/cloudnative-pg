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

package restore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupRestoreTargetDirectories(t *testing.T) {
	tempDir := t.TempDir()
	pgData := filepath.Join(tempDir, "pgdata")
	pgWal := filepath.Join(tempDir, "pgwal")
	preservedData := filepath.Join(tempDir, "pgdata_20260915T120000")

	for _, directory := range []string{pgData, pgWal, preservedData} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(pgData, "PG_VERSION"), []byte("17"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pgWal, "wal-segment"), []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}

	cleanupRestoreTargetDirectories(context.Background(), pgData, pgWal)

	for _, directory := range []string{pgData, pgWal} {
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, got %v", directory, err)
		}
	}
	if _, err := os.Stat(preservedData); err != nil {
		t.Fatalf("expected preserved data directory to remain, got %v", err)
	}
}

func TestCleanupRestoreTargetDirectoriesWithEmptyWalPath(t *testing.T) {
	pgData := filepath.Join(t.TempDir(), "pgdata")
	if err := os.MkdirAll(pgData, 0o700); err != nil {
		t.Fatal(err)
	}

	cleanupRestoreTargetDirectories(context.Background(), pgData, "")

	if _, err := os.Stat(pgData); !os.IsNotExist(err) {
		t.Fatalf("expected PGDATA to be removed, got %v", err)
	}
}
