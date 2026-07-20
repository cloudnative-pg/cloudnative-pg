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

// MarkerIdentity identifies the instance that bootstrapped a data directory.
// It is stored in the completion marker so the marker certifies "THIS instance
// bootstrapped this data directory", not merely "someone did". This matters
// because the marker lives inside PGDATA and therefore travels inside volume
// snapshots: a PVC cloned from a snapshot already carries the source instance's
// marker, and without an identity check the clone would wrongly be considered
// already bootstrapped and skip the restore/restoresnapshot phase-0 work.
type MarkerIdentity struct {
	// Namespace is the namespace of the cluster the instance belongs to.
	Namespace string `json:"namespace"`

	// ClusterName is the name of the cluster the instance belongs to.
	ClusterName string `json:"clusterName"`

	// ClusterUID is the UID of the cluster the instance belongs to. It is the
	// field that distinguishes a cluster from a later cluster reusing the same
	// name and namespace (e.g. an in-place recreation restoring from snapshots).
	ClusterUID string `json:"clusterUID"`

	// PodName is the name of the instance pod that ran the bootstrap.
	PodName string `json:"podName"`
}

// completionMarker is the content of the file written inside PGDATA once an
// in-process bootstrap has finished successfully.
type completionMarker struct {
	// Mode is the bootstrap method that produced the data directory.
	Mode string `json:"mode"`

	// CompletedAt is the time the bootstrap completed.
	CompletedAt time.Time `json:"completedAt"`

	// OperatorVersion is the instance manager version that ran the bootstrap.
	OperatorVersion string `json:"operatorVersion"`

	// Identity is the instance that ran the bootstrap.
	Identity MarkerIdentity `json:"identity"`
}

// markerPath returns the path of the completion marker for the given PGDATA.
func markerPath(pgData string) string {
	return filepath.Join(pgData, constants.BootstrapCompletedFile)
}

// IsCompleted reports whether a previous in-process bootstrap of THIS instance
// already finished successfully. It returns true only when the completion marker
// exists, parses, and records exactly the given identity. A missing marker, an
// unparseable or old-format marker, or a marker written by a different instance
// (e.g. one inherited from a volume snapshot) all mean not completed, so phase-0
// runs again. Only a genuine I/O error reading the marker is surfaced.
func IsCompleted(pgData string, identity MarkerIdentity) (bool, error) {
	data, err := os.ReadFile(markerPath(pgData)) // #nosec G304 -- path is PGDATA, controlled by the operator
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("while reading the bootstrap completion marker: %w", err)
	}

	var marker completionMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		// A marker we cannot parse does not certify that this instance
		// bootstrapped the directory, so treat it as not completed rather than
		// failing startup.
		return false, nil
	}

	return marker.Identity == identity, nil
}

// WriteCompletedMarker writes the completion marker as the last step of a
// bootstrap, recording the identity of the instance that ran it. It is written
// durably: WriteFileAtomic fsyncs the file contents, and we additionally fsync
// PGDATA so the directory entry created by the atomic rename survives a node
// power loss, not just a container restart.
func WriteCompletedMarker(pgData string, mode Mode, identity MarkerIdentity) error {
	marker := completionMarker{
		Mode:            string(mode),
		CompletedAt:     time.Now(),
		OperatorVersion: versions.Version,
		Identity:        identity,
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
