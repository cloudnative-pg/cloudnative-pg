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
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

type livenessExecutor struct {
	cache    *ClusterCache
	instance *postgres.Instance

	// mux guards the isolation bookkeeping below, which is read and written
	// from the probe HTTP handlers.
	mux sync.Mutex
	// consecutiveIsolationFailures counts how many times in a row this instance
	// has been found isolated.
	consecutiveIsolationFailures int32
}

// NewLivenessChecker creates a new instance of the liveness probe checker
func NewLivenessChecker(
	instance *postgres.Instance,
	cache *ClusterCache,
) Checker {
	return &livenessExecutor{
		cache:    cache,
		instance: instance,
	}
}

// isolationCheckSucceeded records that this instance is not isolated right now,
// clearing any failures accumulated so far.
func (e *livenessExecutor) isolationCheckSucceeded() {
	e.mux.Lock()
	defer e.mux.Unlock()

	e.consecutiveIsolationFailures = 0
}

// isolationCheckFailed records that this instance has been found isolated, and
// reports whether that is now the case often enough in a row to shut PostgreSQL
// down. The threshold is the one the kubelet applies before it kills the
// container, so that a transient failure does not stop a primary that a single
// retry would have cleared.
//
// Reaching the threshold restarts the count rather than latching it, so that a
// request which did not stop the instance is followed by another one after a
// further threshold of failures, instead of one per failing probe from then on.
func (e *livenessExecutor) isolationCheckFailed(cluster *apiv1.Cluster) bool {
	e.mux.Lock()
	defer e.mux.Unlock()

	e.consecutiveIsolationFailures++
	if e.consecutiveIsolationFailures < cluster.GetLivenessProbeFailureThreshold() {
		return false
	}

	e.consecutiveIsolationFailures = 0
	return true
}

func (e *livenessExecutor) IsHealthy(
	ctx context.Context,
	w http.ResponseWriter,
) {
	contextLogger := log.FromContext(ctx)

	if e.instance.IsFenced() {
		// A fenced instance has no PostgreSQL to stop, and the operator would ignore
		// the request anyway while the fence is in place.
		e.isolationCheckSucceeded()
		_, _ = fmt.Fprint(w, "OK")
		return
	}

	isPrimary, isPrimaryErr := e.instance.IsPrimary()
	if isPrimaryErr != nil {
		contextLogger.Error(
			isPrimaryErr,
			"Error while checking the instance role, skipping automatic shutdown.")
		// This tells us nothing about isolation, and the probe reports success, so the
		// count has to restart here too: leaving it standing would let failures from
		// either side of this call add up as though they had been consecutive.
		e.isolationCheckSucceeded()
		_, _ = fmt.Fprint(w, "OK")
		return
	}

	if !isPrimary {
		// There's no need to restart a replica if isolated
		e.isolationCheckSucceeded()
		_, _ = fmt.Fprint(w, "OK")
		return
	}

	var cluster apiv1.Cluster
	err := e.cache.tryGetLatestClusterWithTimeout(ctx, &cluster)
	if err == nil {
		// We correctly reached the API server but, as a failsafe measure, we
		// exercise the reachability checker and leave a log message if something
		// is not right.
		// In this way, a network configuration problem can be discovered as
		// quickly as possible.
		if err := evaluateLivenessPinger(ctx, cluster); err != nil {
			contextLogger.Warning(
				"Instance connectivity error - liveness probe succeeding because "+
					"the API server is reachable",
				"err",
				err.Error(),
			)
		}
		e.isolationCheckSucceeded()
		_, _ = fmt.Fprint(w, "OK")
		return
	}

	contextLogger = contextLogger.WithValues("apiServerReachable", false,
		"apiServerErr", err.Error())

	if cluster.Name == "" {
		// We were never able to download a cluster definition. This should not
		// happen because we check the API server connectivity as soon as the
		// instance manager starts, before starting the probe web server.
		//
		// To be safe, we classify this instance manager to be not isolated and
		// postpone any decision to a later liveness probe call.
		contextLogger.Warning(
			"No cluster definition has been received, skipping automatic shutdown.")

		e.isolationCheckSucceeded()
		_, _ = fmt.Fprint(w, "OK")
		return
	}

	if err = evaluateLivenessPinger(ctx, cluster); err != nil {
		contextLogger.Error(err, "Instance connectivity error - liveness probe failing")

		// Failing the probe only asks the kubelet to restart the container, and the
		// shutdown that follows is the one meant for a planned termination: it lets
		// the sessions that are already open run to completion, so an isolated
		// primary would go on acknowledging writes it can no longer replicate for as
		// long as `.spec.smartShutdownTimeout` allows. Stop PostgreSQL here instead,
		// so that ending the writes does not depend on the kubelet's timing.
		//
		// Once the request has been taken up there is no point making another: the
		// shutdown it is running is exactly what keeps the lifecycle loop from reading
		// the channel, so a second one would just park a goroutine until the process
		// exits.
		if !e.instance.StoppedBecauseIsolated() && e.isolationCheckFailed(&cluster) {
			contextLogger.Info("Primary is isolated, requesting a fast shutdown of the PostgreSQL instance")
			// Sending on an unbuffered channel, so this cannot be issued inline without
			// risking blocking the handler past the probe's own timeout.
			go e.instance.RequestFastImmediateShutdownForIsolation()
		}

		http.Error(
			w,
			fmt.Sprintf("liveness check failed: %s", err.Error()),
			http.StatusInternalServerError,
		)
		return
	}

	contextLogger.Trace(
		"Instance connectivity test succeeded - liveness probe succeeding",
		"latestKnownInstancesReportedState", cluster.Status.InstancesReportedState,
	)
	e.isolationCheckSucceeded()
	_, _ = fmt.Fprint(w, "OK")
}

func evaluateLivenessPinger(
	ctx context.Context,
	cluster apiv1.Cluster,
) error {
	contextLogger := log.FromContext(ctx)

	var cfg *apiv1.IsolationCheckConfiguration
	if cluster.Spec.Probes != nil && cluster.Spec.Probes.Liveness != nil {
		cfg = cluster.Spec.Probes.Liveness.IsolationCheck
	}
	if cfg == nil {
		return nil
	}

	// This should never happen given that we set a default value. Fail fast.
	if cfg.Enabled == nil {
		return errors.New("enabled field is not set in the liveness isolation check configuration")
	}

	if !*cfg.Enabled {
		contextLogger.Debug("pinger config not enabled, skipping")
		return nil
	}

	if cluster.Spec.Instances == 1 {
		contextLogger.Debug("Only one instance present in the latest known cluster definition. Skipping automatic shutdown.")
		return nil
	}

	checker, err := buildInstanceReachabilityChecker(cfg)
	if err != nil {
		return fmt.Errorf("failed to build instance reachability checker: %w", err)
	}

	if err := checker.ensureInstancesAreReachable(&cluster); err != nil {
		return fmt.Errorf("liveness check failed: %w", err)
	}

	return nil
}
