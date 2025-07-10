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
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

type livenessExecutor struct {
	cli      client.Client
	instance *postgres.Instance

	lastestKnownCluster *apiv1.Cluster
}

// NewLivenessChecker creates a new instance of the liveness probe checker
func NewLivenessChecker(
	cli client.Client,
	instance *postgres.Instance,
) Checker {
	return &livenessExecutor{
		cli:      cli,
		instance: instance,
	}
}

// tryRefreshLatestClusterWithTimeout refreshes the latest cluster definition, returns a bool indicating if the
// operation was successful
func (e *livenessExecutor) tryRefreshLatestClusterWithTimeout(ctx context.Context, timeout time.Duration) bool {
	timeoutContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cluster apiv1.Cluster
	err := e.cli.Get(
		timeoutContext,
		client.ObjectKey{Namespace: e.instance.GetNamespaceName(), Name: e.instance.GetClusterName()},
		&cluster,
	)
	if err != nil {
		return false
	}

	e.lastestKnownCluster = cluster.DeepCopy()
	return true
}

func (e *livenessExecutor) IsHealthy(
	ctx context.Context,
	w http.ResponseWriter,
) {
	contextLogger := log.FromContext(ctx)

	isPrimary, isPrimaryErr := e.instance.IsPrimary()
	if isPrimaryErr != nil {
		contextLogger.Error(
			isPrimaryErr,
			"Error while checking the instance role, skipping automatic shutdown.")
		_, _ = fmt.Fprint(w, "OK")
		return
	}

	if !isPrimary {
		// There's no need to restart a replica if isolated
		_, _ = fmt.Fprint(w, "OK")
		return
	}

	// We set a safe context timeout of 500ms to avoid a failed request from taking
	// more time than the minimum configurable timeout (1s) of the container's livenessProbe,
	// which otherwise could have triggered a restart of the instance.
	if clusterRefreshed := e.tryRefreshLatestClusterWithTimeout(ctx, 500*time.Millisecond); clusterRefreshed {
		// We correctly reached the API server but, as a failsafe measure, we
		// exercise the reachability checker and leave a log message if something
		// is not right.
		// In this way a network configuration problem can be discovered as
		// quickly as possible.
		if err := evaluateLivenessPinger(ctx, e.lastestKnownCluster.DeepCopy()); err != nil {
			contextLogger.Warning(
				"Instance connectivity error - liveness probe succeeding because "+
					"the API server is reachable",
				"err",
				err.Error(),
			)
		}
		_, _ = fmt.Fprint(w, "OK")
		return
	}

	contextLogger = contextLogger.WithValues("apiServerReachable", false)

	if e.lastestKnownCluster == nil {
		// We were never able to download a cluster definition. This should not
		// happen because we check the API server connectivity as soon as the
		// instance manager starts, before starting the probe web server.
		//
		// To be safe, we classify this instance manager to be not isolated and
		// postpone any decision to a later liveness probe call.
		contextLogger.Warning(
			"No cluster definition has been received, skipping automatic shutdown.")

		_, _ = fmt.Fprint(w, "OK")
		return
	}

	err := evaluateLivenessPinger(ctx, e.lastestKnownCluster.DeepCopy())
	if err != nil {
		contextLogger.Error(err, "Instance connectivity error - liveness probe failing")
		http.Error(
			w,
			fmt.Sprintf("liveness check failed: %s", err.Error()),
			http.StatusInternalServerError,
		)
		return
	}

	contextLogger.Debug(
		"Instance connectivity test succeeded - liveness probe succeeding",
		"latestKnownInstancesReportedState", e.lastestKnownCluster.Status.InstancesReportedState,
	)
	_, _ = fmt.Fprint(w, "OK")
}

func evaluateLivenessPinger(
	ctx context.Context,
	cluster *apiv1.Cluster,
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

	if err := checker.ensureInstancesAreReachable(cluster); err != nil {
		return fmt.Errorf("liveness check failed: %w", err)
	}

	return nil
}
