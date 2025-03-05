/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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

// ProbeType is the type of the probe
type ProbeType string

const (
	// ProbeTypeReadiness is the readiness probe
	ProbeTypeReadiness ProbeType = "readiness"
	// ProbeTypeStartup is the startup probe
	ProbeTypeStartup ProbeType = "startup"
)

type runner interface {
	// IsHealthy evaluates the status of PostgreSQL. If the probe is positive,
	// it returns a nil error, otherwise the error status describes why
	// the probe is failing
	IsHealthy(ctx context.Context, instance *postgres.Instance) error
}

// Checker is the interface for the probe checker
type Checker interface {
	IsHealthy(ctx context.Context, w http.ResponseWriter, probeType ProbeType)
}

type executor struct {
	cli      client.Client
	instance *postgres.Instance
}

// NewChecker creates a new instance of the probe checker
func NewChecker(
	cli client.Client,
	instance *postgres.Instance,
) Checker {
	return &executor{
		cli:      cli,
		instance: instance,
	}
}

// IsHealthy executes the underlying probe logic and writes a response to the request accordingly to the result obtained
func (e *executor) IsHealthy(
	ctx context.Context,
	w http.ResponseWriter,
	probeType ProbeType,
) {
	contextLogger := log.FromContext(ctx)

	var cluster apiv1.Cluster
	if err := e.cli.Get(
		ctx,
		client.ObjectKey{Namespace: e.instance.GetNamespaceName(), Name: e.instance.GetClusterName()},
		&cluster,
	); err != nil {
		contextLogger.Warning(
			fmt.Sprintf("%s check failed, cannot check Cluster definition", probeType),
			"err", err.Error(),
		)
		http.Error(
			w,
			fmt.Sprintf("%s check failed cannot get Cluster definition: %s", probeType, err.Error()),
			http.StatusInternalServerError,
		)
		return
	}

	probeRunner := getProbeRunnerFromCluster(probeType, cluster)
	if err := probeRunner.IsHealthy(ctx, e.instance); err != nil {
		contextLogger.Warning(fmt.Sprintf("%s probe failing", probeType), "err", err.Error())
		http.Error(
			w,
			fmt.Sprintf("%s check failed: %s", probeType, err.Error()),
			http.StatusInternalServerError,
		)
		return
	}

	contextLogger.Trace(fmt.Sprintf("%s probe succeeding", probeType))
	_, _ = fmt.Fprint(w, "OK")
}

func getProbeRunnerFromCluster(probeType ProbeType, cluster apiv1.Cluster) runner {
	var probe *apiv1.ProbeWithStrategy
	if cluster.Spec.Probes != nil {
		switch probeType {
		case ProbeTypeStartup:
			probe = cluster.Spec.Probes.Startup

		case ProbeTypeReadiness:
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
