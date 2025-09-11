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

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/controller"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/archiver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

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
