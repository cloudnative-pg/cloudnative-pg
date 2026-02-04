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
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// PoolersPausedEventReason is the event reason when poolers are paused during switchover
	PoolersPausedEventReason = "PoolersPaused"

	// PoolersResumedEventReason is the event reason when poolers are resumed after switchover
	PoolersResumedEventReason = "PoolersResumed"

	// PoolersPauseTimeoutEventReason is the event reason when poolers are resumed due to timeout
	PoolersPauseTimeoutEventReason = "PoolersPauseTimeout"
)

// pausePoolersDuringSwitchover pauses all eligible poolers during switchover/failover.
// It only pauses poolers that:
// - Use automated integration (not manual auth)
// - Are not already paused (either manually or by a previous auto-pause)
func (r *ClusterReconciler) pausePoolersDuringSwitchover(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx).WithName("pooler_pause")

	// Check if feature is enabled
	if !cluster.Spec.ShouldPausePoolersDuringSwitchover() {
		return nil
	}

	// Check if already paused by us
	if cluster.Status.PoolersPausedForSwitchover {
		contextLogger.Debug("Poolers already paused for switchover, skipping")
		return nil
	}

	// Get all poolers for this cluster
	poolers, err := r.getClusterPoolers(ctx, cluster)
	if err != nil {
		return fmt.Errorf("while getting poolers for cluster %s: %w", cluster.Name, err)
	}

	if len(poolers.Items) == 0 {
		contextLogger.Debug("No poolers found for cluster")
		return nil
	}

	// Pause eligible poolers
	pausedCount := 0
	var lastErr error
	for i := range poolers.Items {
		pooler := &poolers.Items[i]

		// Skip poolers without automated integration
		if !pooler.IsAutomatedIntegration() {
			contextLogger.Debug("Skipping pooler without automated integration",
				"pooler", pooler.Name)
			continue
		}

		// Skip already paused poolers (manual pause)
		if pooler.Spec.PgBouncer != nil && pooler.Spec.PgBouncer.IsPaused() {
			contextLogger.Debug("Skipping already paused pooler",
				"pooler", pooler.Name)
			continue
		}

		// Pause the pooler
		if err := r.pausePooler(ctx, pooler); err != nil {
			contextLogger.Error(err, "while pausing pooler", "pooler", pooler.Name)
			lastErr = err
			continue
		}
		pausedCount++
		contextLogger.Info("Paused pooler for switchover", "pooler", pooler.Name)
	}

	if pausedCount > 0 {
		// Update cluster status
		origCluster := cluster.DeepCopy()
		cluster.Status.PoolersPausedForSwitchover = true
		cluster.Status.PoolersPausedTimestamp = pgTime.GetCurrentTimestamp()
		if err := r.Status().Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
			return fmt.Errorf("while updating cluster status for pooler pause: %w", err)
		}

		// Emit event
		r.Recorder.Eventf(cluster, "Normal", PoolersPausedEventReason,
			"Paused %d pooler(s) for switchover/failover", pausedCount)
	}

	return lastErr
}

// resumePoolersAfterSwitchover resumes poolers that were automatically paused during switchover.
// It only resumes poolers that have the pausedDuringSwitchover annotation.
func (r *ClusterReconciler) resumePoolersAfterSwitchover(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx).WithName("pooler_resume")

	// Check if poolers were paused by us
	if !cluster.Status.PoolersPausedForSwitchover {
		return nil
	}

	// Get all poolers for this cluster
	poolers, err := r.getClusterPoolers(ctx, cluster)
	if err != nil {
		return fmt.Errorf("while getting poolers for cluster %s: %w", cluster.Name, err)
	}

	// Resume poolers that have our annotation
	resumedCount := 0
	var lastErr error
	for i := range poolers.Items {
		pooler := &poolers.Items[i]

		// Only resume poolers that were paused by us
		if pooler.Annotations == nil ||
			pooler.Annotations[utils.PausedDuringSwitchoverAnnotationName] != "true" {
			continue
		}

		// Resume the pooler
		if err := r.resumePooler(ctx, pooler); err != nil {
			contextLogger.Error(err, "while resuming pooler", "pooler", pooler.Name)
			lastErr = err
			continue
		}
		resumedCount++
		contextLogger.Info("Resumed pooler after switchover", "pooler", pooler.Name)
	}

	// Update cluster status
	origCluster := cluster.DeepCopy()
	cluster.Status.PoolersPausedForSwitchover = false
	cluster.Status.PoolersPausedTimestamp = ""
	if err := r.Status().Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
		return fmt.Errorf("while updating cluster status for pooler resume: %w", err)
	}

	if resumedCount > 0 {
		// Emit event
		r.Recorder.Eventf(cluster, "Normal", PoolersResumedEventReason,
			"Resumed %d pooler(s) after switchover/failover completed", resumedCount)
	}

	return lastErr
}

// checkPoolerPauseTimeout checks if poolers have been paused for too long and forces resume.
// This prevents indefinite service disruption if switchover fails.
func (r *ClusterReconciler) checkPoolerPauseTimeout(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx).WithName("pooler_pause_timeout")

	// Check if poolers are paused
	if !cluster.Status.PoolersPausedForSwitchover {
		return nil
	}

	// Check if timestamp is set
	if cluster.Status.PoolersPausedTimestamp == "" {
		return nil
	}

	// Calculate duration since pause
	pauseDuration, err := pgTime.DifferenceBetweenTimestamps(
		pgTime.GetCurrentTimestamp(),
		cluster.Status.PoolersPausedTimestamp,
	)
	if err != nil {
		contextLogger.Error(err, "while calculating pause duration")
		return nil
	}

	// Check if timeout exceeded
	timeout := cluster.Spec.GetPoolerPauseDuringSwitchoverTimeout()
	if pauseDuration < timeout {
		return nil
	}

	contextLogger.Warning("Pooler pause timeout exceeded, forcing resume",
		"pauseDuration", pauseDuration,
		"timeout", timeout)

	// Force resume
	if err := r.resumePoolersAfterSwitchover(ctx, cluster); err != nil {
		return fmt.Errorf("while forcing resume on timeout: %w", err)
	}

	// Emit warning event
	r.Recorder.Eventf(cluster, "Warning", PoolersPauseTimeoutEventReason,
		"Force resumed poolers after timeout (%v) - switchover may not have completed successfully",
		timeout)

	return nil
}

// getClusterPoolers returns all poolers associated with the given cluster.
func (r *ClusterReconciler) getClusterPoolers(ctx context.Context, cluster *apiv1.Cluster) (*apiv1.PoolerList, error) {
	var poolers apiv1.PoolerList

	err := r.List(ctx, &poolers,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{poolerClusterKey: cluster.Name})
	if err != nil {
		return nil, err
	}

	return &poolers, nil
}

// pausePooler sets the pooler to paused state and adds the annotation.
func (r *ClusterReconciler) pausePooler(ctx context.Context, pooler *apiv1.Pooler) error {
	origPooler := pooler.DeepCopy()

	// Set paused
	paused := true
	if pooler.Spec.PgBouncer == nil {
		pooler.Spec.PgBouncer = &apiv1.PgBouncerSpec{}
	}
	pooler.Spec.PgBouncer.Paused = &paused

	// Add annotation
	if pooler.Annotations == nil {
		pooler.Annotations = make(map[string]string)
	}
	pooler.Annotations[utils.PausedDuringSwitchoverAnnotationName] = "true"

	return r.Patch(ctx, pooler, client.MergeFrom(origPooler))
}

// resumePooler sets the pooler to unpaused state and removes the annotation.
func (r *ClusterReconciler) resumePooler(ctx context.Context, pooler *apiv1.Pooler) error {
	origPooler := pooler.DeepCopy()

	// Set unpaused
	paused := false
	if pooler.Spec.PgBouncer != nil {
		pooler.Spec.PgBouncer.Paused = &paused
	}

	// Remove annotation
	delete(pooler.Annotations, utils.PausedDuringSwitchoverAnnotationName)

	return r.Patch(ctx, pooler, client.MergeFrom(origPooler))
}
