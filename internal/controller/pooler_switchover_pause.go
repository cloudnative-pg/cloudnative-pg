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
	// PoolerPausedEventReason is the event reason when a pooler is paused during switchover
	PoolerPausedEventReason = "PoolerPaused"

	// PoolerResumedEventReason is the event reason when a pooler is resumed after switchover
	PoolerResumedEventReason = "PoolerResumed"

	// PoolerPauseTimeoutEventReason is the event reason when a pooler is resumed due to timeout
	PoolerPauseTimeoutEventReason = "PoolerPauseTimeout"
)

// reconcileSwitchoverPause checks if the pooler should be paused or resumed
// based on the switchover state of the referenced cluster.
func (r *PoolerReconciler) reconcileSwitchoverPause(
	ctx context.Context,
	pooler *apiv1.Pooler,
	cluster *apiv1.Cluster,
) error {
	contextLogger := log.FromContext(ctx).WithName("pooler_switchover_pause")

	// Check if feature is enabled
	if !pooler.Spec.PgBouncer.ShouldPauseDuringSwitchover() {
		return nil
	}

	// Skip poolers without automated integration
	if !pooler.IsAutomatedIntegration() {
		return nil
	}

	switchoverInProgress := cluster.Status.CurrentPrimary != "" &&
		cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary

	if switchoverInProgress {
		return r.handleSwitchoverInProgress(ctx, pooler, contextLogger)
	}

	return r.handleSwitchoverComplete(ctx, pooler, contextLogger)
}

// handleSwitchoverInProgress pauses the pooler if not already paused for switchover.
func (r *PoolerReconciler) handleSwitchoverInProgress(
	ctx context.Context,
	pooler *apiv1.Pooler,
	contextLogger log.Logger,
) error {
	// Already paused by us - check timeout
	if pooler.Status.PausedForSwitchover {
		return r.checkPauseTimeout(ctx, pooler, contextLogger)
	}

	// Skip if already manually paused (no annotation from us)
	if pooler.Spec.PgBouncer != nil && pooler.Spec.PgBouncer.IsPaused() {
		contextLogger.Debug("Pooler already manually paused, skipping auto-pause",
			"pooler", pooler.Name)
		return nil
	}

	// Pause the pooler
	return r.pausePoolerForSwitchover(ctx, pooler, contextLogger)
}

// handleSwitchoverComplete resumes the pooler if it was paused by us.
func (r *PoolerReconciler) handleSwitchoverComplete(
	ctx context.Context,
	pooler *apiv1.Pooler,
	contextLogger log.Logger,
) error {
	// Only resume if we paused it
	if !pooler.Status.PausedForSwitchover {
		return nil
	}

	return r.resumePoolerAfterSwitchover(ctx, pooler, contextLogger)
}

// pausePoolerForSwitchover sets the pooler to paused state and marks it.
func (r *PoolerReconciler) pausePoolerForSwitchover(
	ctx context.Context,
	pooler *apiv1.Pooler,
	contextLogger log.Logger,
) error {
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

	if err := r.Patch(ctx, pooler, client.MergeFrom(origPooler)); err != nil {
		return fmt.Errorf("while pausing pooler %s for switchover: %w", pooler.Name, err)
	}

	// Update status
	origPooler = pooler.DeepCopy()
	pooler.Status.PausedForSwitchover = true
	pooler.Status.PausedForSwitchoverTimestamp = pgTime.GetCurrentTimestamp()
	if err := r.Status().Patch(ctx, pooler, client.MergeFrom(origPooler)); err != nil {
		return fmt.Errorf("while updating pooler %s status for pause: %w", pooler.Name, err)
	}

	contextLogger.Info("Paused pooler for switchover", "pooler", pooler.Name)
	r.Recorder.Eventf(pooler, "Normal", PoolerPausedEventReason,
		"Paused pooler for switchover/failover")

	return nil
}

// resumePoolerAfterSwitchover resumes the pooler and clears the switchover state.
func (r *PoolerReconciler) resumePoolerAfterSwitchover(
	ctx context.Context,
	pooler *apiv1.Pooler,
	contextLogger log.Logger,
) error {
	origPooler := pooler.DeepCopy()

	// Set unpaused
	paused := false
	if pooler.Spec.PgBouncer != nil {
		pooler.Spec.PgBouncer.Paused = &paused
	}

	// Remove annotation
	delete(pooler.Annotations, utils.PausedDuringSwitchoverAnnotationName)

	if err := r.Patch(ctx, pooler, client.MergeFrom(origPooler)); err != nil {
		return fmt.Errorf("while resuming pooler %s after switchover: %w", pooler.Name, err)
	}

	// Update status
	origPooler = pooler.DeepCopy()
	pooler.Status.PausedForSwitchover = false
	pooler.Status.PausedForSwitchoverTimestamp = ""
	if err := r.Status().Patch(ctx, pooler, client.MergeFrom(origPooler)); err != nil {
		return fmt.Errorf("while updating pooler %s status for resume: %w", pooler.Name, err)
	}

	contextLogger.Info("Resumed pooler after switchover", "pooler", pooler.Name)
	r.Recorder.Eventf(pooler, "Normal", PoolerResumedEventReason,
		"Resumed pooler after switchover/failover completed")

	return nil
}

// checkPauseTimeout checks if the pooler has been paused for too long and forces resume.
func (r *PoolerReconciler) checkPauseTimeout(
	ctx context.Context,
	pooler *apiv1.Pooler,
	contextLogger log.Logger,
) error {
	if pooler.Status.PausedForSwitchoverTimestamp == "" {
		return nil
	}

	pauseDuration, err := pgTime.DifferenceBetweenTimestamps(
		pgTime.GetCurrentTimestamp(),
		pooler.Status.PausedForSwitchoverTimestamp,
	)
	if err != nil {
		contextLogger.Error(err, "while calculating pause duration")
		return nil
	}

	timeout := pooler.Spec.PgBouncer.GetPauseDuringSwitchoverTimeout()
	if pauseDuration < timeout {
		return nil
	}

	contextLogger.Warning("Pooler pause timeout exceeded, forcing resume",
		"pooler", pooler.Name,
		"pauseDuration", pauseDuration,
		"timeout", timeout)

	if err := r.resumePoolerAfterSwitchover(ctx, pooler, contextLogger); err != nil {
		return fmt.Errorf("while forcing resume on timeout: %w", err)
	}

	r.Recorder.Eventf(pooler, "Warning", PoolerPauseTimeoutEventReason,
		"Force resumed pooler after timeout (%v) - switchover may not have completed successfully",
		timeout)

	return nil
}
