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

package replicaclusterswitch

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// errPostgresNotShutDown is raised when PostgreSQL is not shut down
// and we required to archive the shutdown checkpoint WAL file
var errPostgresNotShutDown = fmt.Errorf("expected postmaster to be shut down")

// generateDemotionToken gets the demotion token from
// the current primary and archives the WAL containing the shutdown
// checkpoint entry
func generateDemotionToken(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instanceClient remote.InstanceClient,
	instancesStatus postgres.PostgresqlStatusList,
) (string, error) {
	contextLogger := log.FromContext(ctx).WithName("shutdown_checkpoint")

	var primaryInstance *postgres.PostgresqlStatus
	for idx := range instancesStatus.Items {
		// The designed primary didn't start but have already
		// been demoted with the signal files.
		// We can't use `item.IsPrimary` to tell if it is
		// a primary or not, and we need to rely on
		// the `currentPrimary` field
		item := instancesStatus.Items[idx]
		if item.Pod.Name == cluster.Status.CurrentPrimary {
			primaryInstance = &item
			break
		}
	}

	if primaryInstance == nil {
		return "", fmt.Errorf(
			"could not detect the designated primary while extracting the shutdown checkpoint token")
	}

	rawPgControlData, err := instanceClient.GetPgControlDataFromInstance(ctx, primaryInstance.Pod)
	if err != nil {
		return "", fmt.Errorf("could not get pg_controldata from Pod %s: %w", primaryInstance.Pod.Name, err)
	}
	parsed := utils.ParsePgControldataOutput(rawPgControlData)
	pgDataState := parsed.GetDatabaseClusterState()

	if !utils.PgDataState(pgDataState).IsShutdown(ctx) {
		// PostgreSQL is still not shut down, waiting
		// until the shutdown is completed
		return "", errPostgresNotShutDown
	}

	token, err := parsed.CreatePromotionToken()
	if err != nil {
		return "", err
	}
	if token == cluster.Status.DemotionToken {
		contextLogger.Debug("no changes in the token value, skipping")
		return "", nil
	}

	partialArchiveWALName, err := instanceClient.ArchivePartialWAL(ctx, primaryInstance.Pod)
	if err != nil {
		return "", fmt.Errorf("could not archive shutdown checkpoint wal file: %w", err)
	}

	if parsed.GetREDOWALFile() != partialArchiveWALName {
		return "", fmt.Errorf("unexpected shutdown checkpoint wal file archived, expected: %s, got: %s",
			parsed.GetREDOWALFile(),
			partialArchiveWALName,
		)
	}

	return token, nil
}
