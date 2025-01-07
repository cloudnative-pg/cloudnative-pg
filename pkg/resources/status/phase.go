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

package status

import (
	"context"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// RegisterPhase update phase in the status cluster with the
// proper reason
func RegisterPhase(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	phase string,
	reason string,
) error {
	origPhase := cluster.Status.Phase
	if err := PatchWithOptimisticLock(ctx, cli, cluster,
		func(cluster *apiv1.Cluster) {
			cluster.Status.Phase = phase
			cluster.Status.PhaseReason = reason
		},
		ReconcileClusterReadyConditionTX,
	); err != nil {
		return err
	}

	contextLogger := log.FromContext(ctx)

	modifiedPhase := cluster.Status.Phase

	if modifiedPhase != apiv1.PhaseHealthy && origPhase == apiv1.PhaseHealthy {
		contextLogger.Info("Cluster has become unhealthy")
	}
	if modifiedPhase == apiv1.PhaseHealthy && origPhase != apiv1.PhaseHealthy {
		contextLogger.Info("Cluster has become healthy")
	}

	return nil
}
