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

package probes

import (
	"context"
	"fmt"
	"math"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// pgStreamingChecker checks if the replica is connected via streaming
// replication and, optionally, if the lag is within the specified maximum
type pgStreamingChecker struct {
	maximumLag *uint64
}

// IsHealthy implements the runner interface
func (c pgStreamingChecker) IsHealthy(ctx context.Context, instance *postgres.Instance) error {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting superuser connection pool: %w", err)
	}

	var configuredLag uint64 = math.MaxUint64
	if c.maximumLag != nil {
		configuredLag = *c.maximumLag
	}

	// At this point, the instance is already running.
	// The startup probe succeeds if the instance satisfies any of the following conditions:
	// - It is a primary instance.
	// - It is a log shipping replica (including a designated primary).
	// - It is a streaming replica with replication lag below the specified threshold.
	//   If no lag threshold is specified, the startup probe succeeds if the replica has successfully connected
	//   to its source at least once.
	row := superUserDB.QueryRowContext(
		ctx,
		`
        WITH
          lag AS (
            SELECT
              (latest_end_lsn - pg_last_wal_replay_lsn()) AS value,
              latest_end_time
            FROM pg_catalog.pg_stat_wal_receiver
          )
        SELECT
          CASE
            WHEN NOT pg_is_in_recovery()
              THEN true
            WHEN (SELECT coalesce(setting, '') = '' FROM pg_catalog.pg_settings WHERE name = 'primary_conninfo')
              THEN true
            WHEN (SELECT value FROM lag) <= $1
              THEN true
            ELSE false
          END AS ready_to_start,
          COALESCE((SELECT value FROM lag), 0) AS lag,
          COALESCE((SELECT latest_end_time FROM lag), '-infinity') AS latest_end_time
		`,
		configuredLag,
	)
	var status bool
	var detectedLag uint64
	var latestEndTime string
	if err := row.Scan(&status, &detectedLag, &latestEndTime); err != nil {
		return fmt.Errorf("streaming replication check failed (scan): %w", err)
	}

	if !status {
		if detectedLag > configuredLag {
			return &ReplicaLaggingError{
				DetectedLag:   detectedLag,
				ConfiguredLag: configuredLag,
				LatestEndTime: latestEndTime,
			}
		}
		return fmt.Errorf("replica not connected via streaming replication")
	}

	return nil
}

// ReplicaLaggingError is raised when a replica is lagging more
// than the configured cap
type ReplicaLaggingError struct {
	// DetectedLag is the lag that was detected
	DetectedLag uint64

	// ConfiguredLag is the lag as configured in the probe
	ConfiguredLag uint64

	// LatestEndTime is the time of last write-ahead log location reported to
	// origin WAL sender
	LatestEndTime string
}

func (e *ReplicaLaggingError) Error() string {
	return fmt.Sprintf(
		"streaming replica lagging; detectedLag=%v configuredLag=%v latestEndTime=%s",
		e.DetectedLag,
		e.ConfiguredLag,
		e.LatestEndTime,
	)
}
