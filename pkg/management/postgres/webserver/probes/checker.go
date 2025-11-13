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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	cache     *clusterCache
	instance  *postgres.Instance
}

// NewReadinessChecker creates a new instance of the readiness probe checker
func NewReadinessChecker(
	cli client.Client,
	instance *postgres.Instance,
) Checker {
	return &executor{
		cache: newClusterCache(
			cli,
			client.ObjectKey{Namespace: instance.GetNamespaceName(), Name: instance.GetClusterName()},
		),
		instance:  instance,
		probeType: probeTypeReadiness,
	}
}

// NewStartupChecker creates a new instance of the startup probe checker
func NewStartupChecker(
	cli client.Client,
	instance *postgres.Instance,
) Checker {
	return &executor{
		cache: newClusterCache(
			cli,
			client.ObjectKey{Namespace: instance.GetNamespaceName(), Name: instance.GetClusterName()},
		),
		instance:  instance,
		probeType: probeTypeStartup,
	}
}

// IsHealthy executes the underlying probe logic and writes a response to the request accordingly to the result obtained
func (e *executor) IsHealthy(
	ctx context.Context,
	w http.ResponseWriter,
) {
	contextLogger := log.FromContext(ctx).WithValues("probeType", e.probeType)

	if clusterRefreshed := e.cache.tryRefreshLatestClusterWithTimeout(ctx); clusterRefreshed {
		probeRunner := getProbeRunnerFromCluster(e.probeType, *e.cache.getLatestKnownCluster())
		if err := probeRunner.IsHealthy(ctx, e.instance); err != nil {
			contextLogger.Warning("probe failing", "err", err.Error())
			http.Error(
				w,
				fmt.Sprintf("%s check failed: %s", e.probeType, err.Error()),
				http.StatusInternalServerError,
			)
			return
		}

		contextLogger.Trace("probe succeeding")
		_, _ = fmt.Fprint(w, "OK")
		return
	}

	contextLogger = contextLogger.WithValues("apiServerReachable", false)

	// Use the cached cluster definition as a fallback, or create a default if none exists
	cluster := e.cache.getLatestKnownCluster()
	if cluster == nil {
		// We were never able to download a cluster definition. This should not
		// happen because we check the API server connectivity as soon as the
		// instance manager starts, before starting the probe web server.
		//
		// To be safe, we use an empty cluster with default probe settings.
		contextLogger.Warning("no cluster definition has been received, using default probe settings")
		cluster = &apiv1.Cluster{}
	} else {
		contextLogger.Warning("probe using cached cluster definition due to API server connectivity issue")
	}

	probeRunner := getProbeRunnerFromCluster(e.probeType, *cluster)
	if err := probeRunner.IsHealthy(ctx, e.instance); err != nil {
		contextLogger.Warning("probe failing", "err", err.Error())
		http.Error(
			w,
			fmt.Sprintf("%s check failed: %s", e.probeType, err.Error()),
			http.StatusInternalServerError,
		)
		return
	}

	contextLogger.Debug("probe succeeding with cached cluster definition")
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
		return pgIsReadyChecker{}
	case probe.Type == apiv1.ProbeStrategyPgIsReady:
		return pgIsReadyChecker{}
	case probe.Type == apiv1.ProbeStrategyQuery:
		return pgQueryChecker{}
	case probe.Type == apiv1.ProbeStrategyStreaming:
		result := pgStreamingChecker{}
		if probe.MaximumLag != nil {
			result.maximumLag = ptr.To(probe.MaximumLag.AsDec().UnscaledBig().Uint64())
		}
		return result
	}

	return pgIsReadyChecker{}
}
