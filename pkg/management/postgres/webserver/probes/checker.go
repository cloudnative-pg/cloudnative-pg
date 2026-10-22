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
	"net/http"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/types"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// probeType is the type of the probe
type probeType string

const (
	// probeTypeReadiness is the readiness probe
	probeTypeReadiness probeType = "readiness"
	// probeTypeStartup is the startup probe
	probeTypeStartup probeType = "startup"
)

type runner interface {
	// IsHealthy evaluates the status of PostgreSQL. If the probe is positive,
	// it returns a nil error, otherwise the error status describes why
	// the probe is failing
	IsHealthy(ctx context.Context, instance *postgres.Instance) error
}

// Checker executes the probe and writes the response to the request
type Checker interface {
	IsHealthy(ctx context.Context, w http.ResponseWriter)
}

type executor struct {
	probeType probeType
	cache     *ClusterCache
	instance  *postgres.Instance
}

// NewReadinessChecker creates a new instance of the readiness probe checker
func NewReadinessChecker(
	instance *postgres.Instance,
	cache *ClusterCache,
) Checker {
	return &executor{
		cache:     cache,
		instance:  instance,
		probeType: probeTypeReadiness,
	}
}

// NewStartupChecker creates a new instance of the startup probe checker
func NewStartupChecker(
	instance *postgres.Instance,
	cache *ClusterCache,
) Checker {
	return &executor{
		cache:     cache,
		instance:  instance,
		probeType: probeTypeStartup,
	}
}

// IsHealthy executes the underlying probe logic and writes a response to the request accordingly to the result obtained
func (e *executor) IsHealthy(
	ctx context.Context,
	w http.ResponseWriter,
) {
	contextLogger := log.FromContext(ctx)

	var cluster apiv1.Cluster
	if err := e.cache.tryGetLatestClusterWithTimeout(ctx, &cluster); err != nil {
		contextLogger = contextLogger.WithValues("apiServerReachable", false,
			"apiServerErr", err.Error())
		settingSource := "cached cluster definition"
		if cluster.Name == "" {
			settingSource = "default probe configuration"
		}
		contextLogger.Warning(
			fmt.Sprintf("%s probe using %s due to API server connectivity issue", e.probeType, settingSource),
		)
	}

	probeRunner := getProbeRunnerFromCluster(e.probeType, cluster)
	if e.probeType == probeTypeReadiness {
		probeRunner = notDivergedChecker{inner: probeRunner}
	}
	if err := probeRunner.IsHealthy(ctx, e.instance); err != nil {
		contextLogger.Warning(fmt.Sprintf("%s probe failing", e.probeType), "err", err.Error())
		http.Error(
			w,
			fmt.Sprintf("%s check failed: %s", e.probeType, err.Error()),
			http.StatusInternalServerError,
		)
		return
	}

	contextLogger.Trace(fmt.Sprintf("%s probe succeeding", e.probeType))
	_, _ = fmt.Fprint(w, "OK")
}

func getProbeRunnerFromCluster(probeType probeType, cluster apiv1.Cluster) runner {
	var probe *apiv1.ProbeWithStrategy
	if cluster.Spec.Probes != nil {
		switch probeType {
		case probeTypeStartup:
			probe = cluster.Spec.Probes.Startup

		case probeTypeReadiness:
			probe = cluster.Spec.Probes.Readiness
		}
	}

	switch {
	case probe == nil:
		return newPgIsReadyChecker(probeType)
	case probe.Type == apiv1.ProbeStrategyPgIsReady:
		return newPgIsReadyChecker(probeType)
	case probe.Type == apiv1.ProbeStrategyQuery:
		return pgQueryChecker{}
	case probe.Type == apiv1.ProbeStrategyStreaming:
		result := pgStreamingChecker{}
		if probe.MaximumLag != nil {
			result.maximumLag = ptr.To(probe.MaximumLag.AsDec().UnscaledBig().Uint64())
		}
		return result
	}

	return newPgIsReadyChecker(probeType)
}

// notDivergedChecker fails the readiness of a standby that can never follow
// the primary's timeline (see postgres.DetectTimelineDivergence): it holds
// writes the primary discarded and receives no new ones, so it must not
// serve reads.
type notDivergedChecker struct {
	inner runner
}

func (c notDivergedChecker) IsHealthy(ctx context.Context, instance *postgres.Instance) error {
	if err := c.inner.IsHealthy(ctx, instance); err != nil {
		return err
	}

	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting superuser connection pool: %w", err)
	}

	var replayLSN types.LSN
	if err := superUserDB.QueryRowContext(ctx,
		"SELECT COALESCE(pg_catalog.pg_last_wal_replay_lsn()::varchar, '')").Scan(&replayLSN); err != nil {
		return fmt.Errorf("while reading the replay position: %w", err)
	}

	divergence, err := instance.DetectTimelineDivergence(replayLSN)
	if err != nil {
		return fmt.Errorf("while checking for a timeline divergence: %w", err)
	}
	if divergence != nil {
		return fmt.Errorf("replayed up to %s on timeline %d, past the point (%s) where the primary's timeline %d "+
			"forked away from it", divergence.ReplayLSN, divergence.TimeLineID, divergence.ForkLSN,
			divergence.PrimaryTimeLineID)
	}

	return nil
}

// newPgIsReadyChecker creates the pg_isready strategy runner for the passed
// probe type, wrapping it in startupPgIsReadyChecker for the startup probe.
func newPgIsReadyChecker(probeType probeType) runner {
	if probeType == probeTypeStartup {
		return startupPgIsReadyChecker{inner: pgIsReadyChecker{}}
	}
	return pgIsReadyChecker{}
}
