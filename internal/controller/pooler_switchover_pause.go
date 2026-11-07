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
	"time"

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

	// pausedDuringSwitchoverActive is the annotation value marking a pooler that is
	// currently auto-paused for an ongoing switchover.
	pausedDuringSwitchoverActive = "true"

	// pausedDuringSwitchoverTimedOut is the annotation value marking a pooler that was
	// force-resumed after the safety timeout. It prevents re-pausing the same ongoing
	// switchover, which would otherwise flap pause/resume until the switchover completes.
	pausedDuringSwitchoverTimedOut = "timedout"
)

// reconcileSwitchoverPause checks if the pooler should be paused or resumed
// based on the switchover state of the referenced cluster. It returns the delay
// after which the pooler should be reconciled again: while the pooler is paused
// this is the time left before the safety timeout, so the timeout is enforced
// even when no cluster event wakes the controller. A zero delay means no
// self-triggered requeue is needed.
func (r *PoolerReconciler) reconcileSwitchoverPause(
	ctx context.Context,
	pooler *apiv1.Pooler,
	cluster *apiv1.Cluster,
) (time.Duration, error) {
	contextLogger := log.FromContext(ctx).WithName("pooler_switchover_pause")

	// The feature only applies to enabled poolers with automated integration.
	// If it is disabled (or the pooler is no longer automated) but we previously
	// paused it, fall through to the completion path so we undo our own pause
	// instead of leaving the pooler stuck paused.
	if !pooler.Spec.PgBouncer.ShouldPauseDuringSwitchover() || !pooler.IsAutomatedIntegration() {
		return 0, r.handleSwitchoverComplete(ctx, pooler, contextLogger)
	}

	switchoverInProgress := cluster.Status.CurrentPrimary != "" &&
		cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary

	if switchoverInProgress {
		return r.handleSwitchoverInProgress(ctx, pooler, contextLogger)
	}

	return 0, r.handleSwitchoverComplete(ctx, pooler, contextLogger)
}

// handleSwitchoverInProgress pauses the pooler if not already paused for switchover.
// It returns the delay before the next reconcile, used to enforce the pause timeout.
func (r *PoolerReconciler) handleSwitchoverInProgress(
	ctx context.Context,
	pooler *apiv1.Pooler,
	contextLogger log.Logger,
) (time.Duration, error) {
	// Already paused by us - check timeout
	if pooler.Status.PausedForSwitchover {
		return r.checkPauseTimeout(ctx, pooler, contextLogger)
	}

	// We already force-resumed this switchover after the timeout. Do not pause
	// again, otherwise we would flap pause/resume until the switchover completes.
	// The marker is cleared once the switchover finishes (handleSwitchoverComplete).
	if pooler.Annotations[utils.PausedDuringSwitchoverAnnotationName] == pausedDuringSwitchoverTimedOut {
		contextLogger.Debug("Pooler already force-resumed after timeout, not re-pausing",
			"pooler", pooler.Name)
		return 0, nil
	}

	// Skip if already manually paused (no annotation from us)
	if pooler.Spec.PgBouncer != nil && pooler.Spec.PgBouncer.IsPaused() {
		contextLogger.Debug("Pooler already manually paused, skipping auto-pause",
			"pooler", pooler.Name)
		return 0, nil
	}

	// Pause the pooler, then requeue after the timeout so we force-resume even if
	// the switchover never completes and no further cluster event arrives.
	if err := r.pausePoolerForSwitchover(ctx, pooler, contextLogger); err != nil {
		return 0, err
	}
	return pooler.Spec.PgBouncer.GetPauseDuringSwitchoverTimeout(), nil
}

// handleSwitchoverComplete resumes the pooler if it was paused by us, and clears
// a leftover timeout marker from a switchover we previously gave up on.
func (r *PoolerReconciler) handleSwitchoverComplete(
	ctx context.Context,
	pooler *apiv1.Pooler,
	contextLogger log.Logger,
) error {
	// Resume if we paused it. Status is the operator-owned pause signal, so a partial
	// write that set the annotation but not the status simply leaves the pooler
	// unpaused, and the next reconcile re-pauses it from the switchover state.
	if pooler.Status.PausedForSwitchover {
		return r.resumePoolerAfterSwitchover(ctx, pooler, contextLogger)
	}

	// Not paused anymore: clear the timeout marker left by an earlier force-resume so
	// the next switchover can be paused again.
	if _, markerPresent := pooler.Annotations[utils.PausedDuringSwitchoverAnnotationName]; markerPresent {
		origPooler := pooler.DeepCopy()
		delete(pooler.Annotations, utils.PausedDuringSwitchoverAnnotationName)
		if err := r.Patch(ctx, pooler, client.MergeFrom(origPooler)); err != nil {
			return fmt.Errorf("while clearing switchover marker on pooler %s: %w", pooler.Name, err)
		}
	}

	return nil
}

// pausePoolerForSwitchover marks the pooler as paused for switchover. It only writes
// operator-owned fields: an annotation as ownership marker and the status flag that
// makes the instance manager issue PgBouncer PAUSE. The user-owned spec is never
// touched, so a GitOps controller has nothing to revert.
func (r *PoolerReconciler) pausePoolerForSwitchover(
	ctx context.Context,
	pooler *apiv1.Pooler,
	contextLogger log.Logger,
) error {
	origPooler := pooler.DeepCopy()

	// Add annotation
	if pooler.Annotations == nil {
		pooler.Annotations = make(map[string]string)
	}
	pooler.Annotations[utils.PausedDuringSwitchoverAnnotationName] = pausedDuringSwitchoverActive

	if err := r.Patch(ctx, pooler, client.MergeFrom(origPooler)); err != nil {
		return fmt.Errorf("while marking pooler %s paused for switchover: %w", pooler.Name, err)
	}

	// Update status: this is what makes the instance manager issue PAUSE.
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

// resumePoolerAfterSwitchover resumes the pooler and clears the switchover marker,
// used on the normal path once the switchover has completed.
func (r *PoolerReconciler) resumePoolerAfterSwitchover(
	ctx context.Context,
	pooler *apiv1.Pooler,
	contextLogger log.Logger,
) error {
	return r.resumePooler(ctx, pooler, "", contextLogger)
}

// resumePooler clears the paused status so the instance manager issues PgBouncer
// RESUME. markerAnnotation, when non-empty, is written to the switchover annotation
// instead of removing it, so a timed-out switchover is remembered and not paused
// again while still in progress. Like the pause path, it only writes operator-owned
// fields and never the user-owned spec.
func (r *PoolerReconciler) resumePooler(
	ctx context.Context,
	pooler *apiv1.Pooler,
	markerAnnotation string,
	contextLogger log.Logger,
) error {
	origPooler := pooler.DeepCopy()

	if markerAnnotation != "" {
		if pooler.Annotations == nil {
			pooler.Annotations = make(map[string]string)
		}
		pooler.Annotations[utils.PausedDuringSwitchoverAnnotationName] = markerAnnotation
	} else {
		delete(pooler.Annotations, utils.PausedDuringSwitchoverAnnotationName)
	}

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
// While the timeout has not elapsed it returns the time left, so the caller can requeue
// and enforce it without relying on an external event.
func (r *PoolerReconciler) checkPauseTimeout(
	ctx context.Context,
	pooler *apiv1.Pooler,
	contextLogger log.Logger,
) (time.Duration, error) {
	if pooler.Status.PausedForSwitchoverTimestamp == "" {
		return 0, nil
	}

	pauseDuration, err := pgTime.DifferenceBetweenTimestamps(
		pgTime.GetCurrentTimestamp(),
		pooler.Status.PausedForSwitchoverTimestamp,
	)
	if err != nil {
		contextLogger.Error(err, "while calculating pause duration")
		return 0, nil
	}

	timeout := pooler.Spec.PgBouncer.GetPauseDuringSwitchoverTimeout()
	if pauseDuration < timeout {
		return timeout - pauseDuration, nil
	}

	contextLogger.Warning("Pooler pause timeout exceeded, forcing resume",
		"pooler", pooler.Name,
		"pauseDuration", pauseDuration,
		"timeout", timeout)

	// Leave the timed-out marker so we do not immediately re-pause this switchover.
	if err := r.resumePooler(ctx, pooler, pausedDuringSwitchoverTimedOut, contextLogger); err != nil {
		return 0, fmt.Errorf("while forcing resume on timeout: %w", err)
	}

	r.Recorder.Eventf(pooler, "Warning", PoolerPauseTimeoutEventReason,
		"Force resumed pooler after timeout (%v) - switchover may not have completed successfully",
		timeout)

	return 0, nil
}
