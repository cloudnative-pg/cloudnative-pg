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

package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// completionMarker is the content of the file written inside PGDATA once an
// in-process bootstrap has finished successfully.
type completionMarker struct {
	// Mode is the bootstrap method that produced the data directory.
	Mode string `json:"mode"`

	// CompletedAt is the time the bootstrap completed.
	CompletedAt time.Time `json:"completedAt"`

	// OperatorVersion is the instance manager version that ran the bootstrap.
	OperatorVersion string `json:"operatorVersion"`
}

// markerPath returns the path of the completion marker for the given PGDATA.
func markerPath(pgData string) string {
	return filepath.Join(pgData, constants.BootstrapCompletedFile)
}

// IsCompleted reports whether the completion marker is present inside PGDATA,
// meaning a previous in-process bootstrap already finished successfully.
func IsCompleted(pgData string) (bool, error) {
	return fileutils.FileExists(markerPath(pgData))
}

// WriteCompletedMarker writes the completion marker as the last step of a
// bootstrap. It is written durably: WriteFileAtomic fsyncs the file contents,
// and we additionally fsync PGDATA so the directory entry created by the atomic
// rename survives a node power loss, not just a container restart.
func WriteCompletedMarker(pgData string, mode Mode) error {
	marker := completionMarker{
		Mode:            string(mode),
		CompletedAt:     time.Now(),
		OperatorVersion: versions.Version,
	}

	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("while marshalling the bootstrap completion marker: %w", err)
	}

	if _, err := fileutils.WriteFileAtomic(markerPath(pgData), data, 0o600); err != nil {
		return fmt.Errorf("while writing the bootstrap completion marker: %w", err)
	}

	if err := fsyncDirectory(pgData); err != nil {
		return fmt.Errorf("while fsyncing PGDATA after writing the completion marker: %w", err)
	}

	return nil
}

// fsyncDirectory flushes a directory entry to stable storage so that a file
// creation or rename inside it is durable.
func fsyncDirectory(path string) error {
	d, err := os.Open(path) // #nosec G304 -- path is PGDATA, controlled by the operator
	if err != nil {
		return err
	}

	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
