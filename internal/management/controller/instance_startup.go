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

package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/controller"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/archiver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ErrTimelineDivergence is returned when a replica's timeline has diverged from
// the primary's timeline after a failover. This typically happens when a replica
// has a checkpoint on an older timeline that is past the fork point of the new timeline.
// The replica cannot recover normally and needs to be re-cloned.
type ErrTimelineDivergence struct {
	LocalTimeline   int
	PrimaryTimeline int
}

// Error implements the error interface
func (e ErrTimelineDivergence) Error() string {
	return fmt.Sprintf("timeline divergence detected: local timeline %d is behind primary timeline %d",
		e.LocalTimeline, e.PrimaryTimeline)
}

// verifyPgDataCoherenceForPrimary will abort the execution if the current server is a primary
// one from the PGDATA viewpoint, but is not classified as the target nor the
// current primary
func (r *InstanceReconciler) verifyPgDataCoherenceForPrimary(ctx context.Context, cluster *apiv1.Cluster) error {
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}
	if !isPrimary {
		return nil
	}
	contextLogger := log.FromContext(ctx)

	targetPrimary := cluster.Status.TargetPrimary
	currentPrimary := cluster.Status.CurrentPrimary

	contextLogger.Info("Cluster status",
		"currentPrimary", currentPrimary,
		"targetPrimary", targetPrimary,
		"isReplicaCluster", cluster.IsReplica())

	switch {
	case cluster.IsReplica():
		// I'm an old primary, and now I'm inside a replica cluster. This can
		// only happen when a primary cluster is demoted while being hibernated.
		// Otherwise, this would have been caught by the operator, and the operator
		// would have requested a replica cluster transition.
		// In this case, we're demoting the cluster immediately.
		contextLogger.Info("Detected transition to replica cluster after reconciliation " +
			"of the cluster is resumed, demoting immediately")
		return r.instance.Demote(ctx, cluster)

	case targetPrimary == r.instance.GetPodName():
		if currentPrimary == "" {
			// This means that this cluster has been just started up and the
			// current primary still need to be written
			contextLogger.Info("First primary instance bootstrap, marking myself as primary",
				"currentPrimary", currentPrimary,
				"targetPrimary", targetPrimary)

			oldCluster := cluster.DeepCopy()
			cluster.Status.CurrentPrimary = r.instance.GetPodName()
			cluster.Status.CurrentPrimaryTimestamp = pgTime.GetCurrentTimestamp()
			return r.client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
		}
		return nil

	default:
		// I'm an old primary and not the current one. I need to wait for
		// the switchover procedure to finish, and then I can demote myself
		// and start following the new primary
		contextLogger.Info("This is an old primary instance, waiting for the "+
			"switchover to finish",
			"currentPrimary", currentPrimary,
			"targetPrimary", targetPrimary)

		// Wait for the switchover to be reflected in the cluster metadata
		if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
			contextLogger.Info("Switchover in progress",
				"targetPrimary", cluster.Status.TargetPrimary,
				"currentPrimary", cluster.Status.CurrentPrimary)
			return controller.ErrNextLoop
		}

		contextLogger.Info("Switchover completed",
			"targetPrimary", cluster.Status.TargetPrimary,
			"currentPrimary", cluster.Status.CurrentPrimary)

		// Wait for the new primary to really accept connections
		err := r.instance.WaitForPrimaryAvailable(ctx)
		if err != nil {
			return err
		}

		// Clean up any stale pid file before executing pg_rewind
		err = r.instance.CleanUpStalePid()
		if err != nil {
			return err
		}

		// Set permission of postgres.auto.conf to 0600 to allow pg_rewind to write to it
		// the mode will be later reset by the reconciliation again, skip the error as
		// rewind may be not needed
		err = r.instance.SetPostgreSQLAutoConfWritable(true)
		if err != nil {
			contextLogger.Error(
				err, "Error while changing mode of the postgresql.auto.conf file before pg_rewind, skipped")
		}

		// We archive every WAL that have not been archived from the latest postmaster invocation.
		if err := archiver.ArchiveAllReadyWALs(ctx, cluster, r.instance.PgData); err != nil {
			var missingPluginError archiver.ErrMissingWALArchiverPlugin
			if errors.As(err, &missingPluginError) {
				// The instance initialization resulted in a fatal error.
				// We need the Pod to be rolled out to install the archiving plugin.
				r.systemInitialization.BroadcastError(err)
			}
			return fmt.Errorf("while ensuring all WAL files are archived: %w", err)
		}

		err = r.instance.Rewind(ctx)
		if err != nil {
			return fmt.Errorf("while executing pg_rewind: %w", err)
		}

		// Now I can demote myself
		return r.instance.Demote(ctx, cluster)
	}
}

// verifyPgDataCoherenceForReplica checks if a replica's timeline has diverged from the primary's
// timeline and handles recovery by signaling that a re-clone is needed.
//
// The scenario this handles:
// 1. Replica is on timeline N with checkpoint at LSN X
// 2. A failover occurs creating timeline N+1 which forked from timeline N at LSN Y
// 3. If X > Y, the replica has data that doesn't exist on the new timeline
// 4. PostgreSQL will refuse to start with "requested timeline is not a child of this server's history"
//
// Unlike a former primary (which can use pg_rewind), a pure replica that was never promoted
// cannot use pg_rewind because it never generated its own diverged WAL. The only solution
// is to delete PGDATA and re-clone from the new primary via pg_basebackup.
func (r *InstanceReconciler) verifyPgDataCoherenceForReplica(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	// Only handle replicas, not primaries (primaries use verifyPgDataCoherenceForPrimary)
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}
	if isPrimary {
		return nil
	}

	// Skip if this instance is the target primary (promotion in progress)
	if cluster.Status.TargetPrimary == r.instance.GetPodName() {
		return nil
	}

	// If cluster timeline is not yet set, skip verification
	clusterTimeline := cluster.Status.TimelineID
	if clusterTimeline == 0 {
		contextLogger.Debug("Cluster timeline not available, skipping timeline verification")
		return nil
	}

	// Get local timeline and checkpoint LSN from pg_controldata
	pgControlDataOutput, err := r.instance.GetPgControldata()
	if err != nil {
		return fmt.Errorf("while getting pg_controldata: %w", err)
	}

	pgControlData := utils.ParsePgControldataOutput(pgControlDataOutput)
	localTimelineStr := pgControlData.GetLatestCheckpointTimelineID()
	if localTimelineStr == "" {
		return fmt.Errorf("could not get timeline from pg_controldata output")
	}

	localTimeline, err := strconv.Atoi(localTimelineStr)
	if err != nil {
		return fmt.Errorf("while parsing local timeline %q: %w", localTimelineStr, err)
	}

	contextLogger.Info("Verifying replica timeline coherence",
		"localTimeline", localTimeline,
		"clusterTimeline", clusterTimeline)

	// If replica is on the same or newer timeline, no divergence possible
	if localTimeline >= clusterTimeline {
		return nil
	}

	// Replica is on an older timeline than the cluster. This happens after a failover.
	// We need to check if the replica's checkpoint is past the fork point.
	//
	// The fork point is recorded in the timeline history file for the cluster's timeline.
	// For example, if cluster is on timeline 21, pg_wal/00000015.history contains:
	//   20  18FC/2E000110  no recovery target specified
	// This means timeline 21 forked from timeline 20 at LSN 18FC/2E000110.
	//
	// If the replica's checkpoint LSN is > fork point LSN, it has diverged data
	// and cannot recover normally.

	localCheckpointLSN := pgControlData.GetLatestCheckpointLocation()
	if localCheckpointLSN == "" {
		contextLogger.Warning("Could not get checkpoint LSN from pg_controldata, skipping divergence check")
		return nil
	}

	// Read the timeline history file to get the fork point
	forkPointLSN, err := r.getForkPointLSN(ctx, clusterTimeline, localTimeline)
	if err != nil {
		// If we can't read the history file, let PostgreSQL try to start.
		// It will fail with a clear error if there's actually a divergence.
		contextLogger.Warning("Could not determine fork point LSN, letting PostgreSQL attempt recovery",
			"error", err,
			"clusterTimeline", clusterTimeline,
			"localTimeline", localTimeline)
		return nil
	}

	contextLogger.Info("Checking for timeline divergence",
		"localTimeline", localTimeline,
		"clusterTimeline", clusterTimeline,
		"localCheckpointLSN", localCheckpointLSN,
		"forkPointLSN", forkPointLSN)

	// Compare LSNs to detect divergence
	diverged, err := isLSNGreaterThan(localCheckpointLSN, forkPointLSN)
	if err != nil {
		contextLogger.Warning("Could not compare LSNs, letting PostgreSQL attempt recovery",
			"error", err,
			"localCheckpointLSN", localCheckpointLSN,
			"forkPointLSN", forkPointLSN)
		return nil
	}

	if !diverged {
		// Replica's checkpoint is before the fork point, it can recover normally
		contextLogger.Info("Replica checkpoint is before fork point, recovery should succeed",
			"localCheckpointLSN", localCheckpointLSN,
			"forkPointLSN", forkPointLSN)
		return nil
	}

	// Divergence confirmed: replica's checkpoint is past the fork point
	contextLogger.Warning("Detected timeline divergence on replica, signaling for re-clone",
		"localTimeline", localTimeline,
		"clusterTimeline", clusterTimeline,
		"localCheckpointLSN", localCheckpointLSN,
		"forkPointLSN", forkPointLSN,
		"reason", "replica checkpoint is past the fork point, cannot recover")

	// Create the error and broadcast it to signal this fatal initialization error.
	// The instance manager will exit with a specific exit code that the operator
	// will detect and use to mark this instance as unrecoverable, triggering
	// deletion of the PVC and re-cloning via pg_basebackup.
	divergenceErr := ErrTimelineDivergence{
		LocalTimeline:   localTimeline,
		PrimaryTimeline: clusterTimeline,
	}
	r.systemInitialization.BroadcastError(divergenceErr)

	return divergenceErr
}

// getForkPointLSN reads the timeline history file to find the LSN where
// the cluster timeline forked from the local timeline.
//
// Timeline history files are stored in pg_wal/ with the format NNNNNNNN.history
// where NNNNNNNN is the timeline ID in hex. The file contains lines like:
//
//	<parent_tli>  <switchpoint_lsn>  <reason>
//
// For example, 00000015.history (timeline 21) might contain:
//
//	20  18FC/2E000110  no recovery target specified
func (r *InstanceReconciler) getForkPointLSN(ctx context.Context, clusterTimeline, localTimeline int) (string, error) {
	contextLogger := log.FromContext(ctx)

	// Build the history file path: pg_wal/NNNNNNNN.history
	historyFileName := fmt.Sprintf("%08X.history", clusterTimeline)
	historyFilePath := filepath.Join(r.instance.PgData, "pg_wal", historyFileName)

	contextLogger.Debug("Reading timeline history file",
		"path", historyFilePath,
		"clusterTimeline", clusterTimeline,
		"localTimeline", localTimeline)

	content, err := os.ReadFile(historyFilePath) //nolint: gosec
	if err != nil {
		return "", fmt.Errorf("while reading history file %s: %w", historyFilePath, err)
	}

	// Parse the history file to find the fork point for our timeline
	return parseTimelineHistoryForForkPoint(string(content), localTimeline)
}

// parseTimelineHistoryForForkPoint parses a timeline history file content and
// returns the fork point LSN for the specified parent timeline.
//
// The history file format is:
//
//	<parent_tli>  <switchpoint_lsn>  <reason>
//
// Lines starting with # are comments.
func parseTimelineHistoryForForkPoint(content string, parentTimeline int) (string, error) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse: <tli>\t<lsn>\t<reason>
		// The fields are separated by tabs, and the reason may contain spaces
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 2 {
			// Try space separation as fallback
			fields = strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
		}

		tli, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}

		if tli == parentTimeline {
			return strings.TrimSpace(fields[1]), nil
		}
	}

	return "", fmt.Errorf("fork point for timeline %d not found in history", parentTimeline)
}

// isLSNGreaterThan compares two LSN strings and returns true if lsn1 > lsn2.
// LSN format is "XXXX/XXXXXXXX" where X is a hex digit.
func isLSNGreaterThan(lsn1, lsn2 string) (bool, error) {
	parse := func(lsn string) (uint64, uint64, error) {
		parts := strings.Split(lsn, "/")
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("invalid LSN format: %s", lsn)
		}
		high, err := strconv.ParseUint(parts[0], 16, 32)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid LSN high part: %s", parts[0])
		}
		low, err := strconv.ParseUint(parts[1], 16, 32)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid LSN low part: %s", parts[1])
		}
		return high, low, nil
	}

	high1, low1, err := parse(lsn1)
	if err != nil {
		return false, err
	}
	high2, low2, err := parse(lsn2)
	if err != nil {
		return false, err
	}

	if high1 != high2 {
		return high1 > high2, nil
	}
	return low1 > low2, nil
}

// ReconcileTablespaces ensures the mount points created for the tablespaces
// are there, and creates a subdirectory in each of them, which will therefore
// be owned by the `postgres` user (rather than `root` as the mount point),
// as required in order to hold PostgreSQL Tablespaces
func (r *InstanceReconciler) ReconcileTablespaces(
	ctx context.Context,
	cluster *apiv1.Cluster,
) error {
	const dataDir = "data"
	contextLogger := log.FromContext(ctx)

	if !cluster.ContainsTablespaces() {
		return nil
	}

	for _, tbsConfig := range cluster.Spec.Tablespaces {
		tbsName := tbsConfig.Name
		mountPoint := specs.MountForTablespace(tbsName)
		if tbsMount, err := fileutils.FileExists(mountPoint); err != nil {
			contextLogger.Error(err, "while checking for mountpoint", "instance",
				r.instance.GetPodName(), "tablespace", tbsName)
			return err
		} else if !tbsMount {
			contextLogger.Error(fmt.Errorf("mountpoint not found"),
				"mountpoint for tablespaces is missing",
				"instance", r.instance.GetPodName(), "tablespace", tbsName)
			continue
		}

		info, err := os.Lstat(mountPoint)
		if err != nil {
			return fmt.Errorf("while checking for tablespace mount point: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("the tablespace %s mount: %s is not a directory", tbsName, mountPoint)
		}
		err = fileutils.EnsureDirectoryExists(filepath.Join(mountPoint, dataDir))
		if err != nil {
			contextLogger.Error(err,
				"could not create data dir in tablespace mount",
				"instance", r.instance.GetPodName(), "tablespace", tbsName)
			return fmt.Errorf("while creating data dir in tablespace %s: %w", mountPoint, err)
		}
	}
	return nil
}
