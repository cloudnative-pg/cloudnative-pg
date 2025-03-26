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
	cli       client.Client
	instance  *postgres.Instance
	probeType probeType
}

// NewReadinessChecker creates a new instance of the readiness probe checker
func NewReadinessChecker(
	cli client.Client,
	instance *postgres.Instance,
) Checker {
	return &executor{
		cli:       cli,
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
		cli:       cli,
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
	if err := e.cli.Get(
		ctx,
		client.ObjectKey{Namespace: e.instance.GetNamespaceName(), Name: e.instance.GetClusterName()},
		&cluster,
	); err != nil {
		contextLogger.Warning(
			fmt.Sprintf("%s check failed, cannot check Cluster definition", e.probeType),
			"err", err.Error(),
		)
		http.Error(
			w,
			fmt.Sprintf("%s check failed cannot get Cluster definition: %s", e.probeType, err.Error()),
			http.StatusInternalServerError,
		)
		return
	}

	probeRunner := getProbeRunnerFromCluster(e.probeType, cluster)
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
