/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package readiness

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// ErrStreamingReplicaNotConnected is raised for streaming replicas that never connected to its primary
var ErrStreamingReplicaNotConnected = errors.New("streaming replica was never connected to the primary node")

// Data is the readiness checker structure
type Data struct {
	instance *postgres.Instance

	streamingReplicaValidated bool
}

// ForInstance creates a readiness checker for a certain instance
func ForInstance(instance *postgres.Instance) *Data {
	return &Data{
		instance: instance,
	}
}

// IsServerReady check if the instance is healthy and can really accept connections
func (data *Data) IsServerReady(ctx context.Context) error {
	if !data.instance.CanCheckReadiness() {
		return fmt.Errorf("instance is not ready yet")
	}
	superUserDB, err := data.instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	// We now check if the database is ready to accept
	// connections
	if err := superUserDB.PingContext(ctx); err != nil {
		return err
	}

	// If we already validated this streaming replica, everything
	// is fine
	if data.streamingReplicaValidated {
		return nil
	}

	// If this is a streaming replica, meaning that
	// primary_conninfo is not empty, we won't declare it ready
	// unless it connected one time successfully to its primary.
	//
	// We check this because a streaming replica that was
	// never connected to the primary could be incoherent,
	// and we want users to notice this as soon as possible
	row := superUserDB.QueryRowContext(
		ctx,
		`
		SELECT 
		    NOT pg_is_in_recovery() 
			OR length(coalesce(setting, '')) = 0
			OR pg_last_wal_replay_lsn() IS NOT NULL
		FROM pg_settings 
		WHERE name = 'primary_conninfo'
		`,
	)
	if err := row.Err(); err != nil {
		return err
	}

	var status bool
	if err := row.Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// PostgreSQL 11 do not have a `primary_conninfo` record
			// in `pg_settings` (we still have a recovery.conf), and
			// is not supported by the community.
			//
			// We don't support this feature, and we declare this replica
			// healthy, retaining the behavior the operator had before.
			data.streamingReplicaValidated = true
			return nil
		}
		return err
	}

	if !status {
		return ErrStreamingReplicaNotConnected
	}

	data.streamingReplicaValidated = true
	return nil
}
