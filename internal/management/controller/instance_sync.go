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

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// resetFailoverQuorumObject resets the content of the sync quorum object
// to prevent unsafe failovers when we are changing the configuration
func (r *InstanceReconciler) resetFailoverQuorumObject(ctx context.Context, cluster *apiv1.Cluster) error {
	if !r.shouldManageFailoverQuorumObject(ctx, cluster) {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var livingQuorumStatus apiv1.FailoverQuorum

		err := r.client.Get(ctx, client.ObjectKeyFromObject(cluster), &livingQuorumStatus)
		if err != nil {
			return err
		}

		livingQuorumStatus.Status = apiv1.FailoverQuorumStatus{}
		return r.client.Status().Update(ctx, &livingQuorumStatus)
	})
}

// updateFailoverQuorumObject updates the sync quorum object reading the
// current synchronous replica metadata from the PG instance
func (r *InstanceReconciler) updateFailoverQuorumObject(ctx context.Context, cluster *apiv1.Cluster) error {
	if !r.shouldManageFailoverQuorumObject(ctx, cluster) {
		return nil
	}

	metadata, err := r.Instance().GetSynchronousReplicationMetadata(ctx)
	if err != nil {
		return err
	}

	newStatus := apiv1.FailoverQuorumStatus{}
	if metadata != nil {
		newStatus.Method = metadata.Method
		newStatus.Primary = r.instance.GetPodName()
		newStatus.StandbyNumber = metadata.NumSync

		// We ensure the primary is not included in the standby names
		newStatus.StandbyNames = make([]string, 0, len(metadata.StandbyNames))
		for _, name := range metadata.StandbyNames {
			if name == newStatus.Primary {
				continue
			}
			newStatus.StandbyNames = append(newStatus.StandbyNames, name)
		}
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var livingQuorumStatus apiv1.FailoverQuorum

		err := r.client.Get(ctx, client.ObjectKeyFromObject(cluster), &livingQuorumStatus)
		if err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(livingQuorumStatus.Status, newStatus) {
			return nil
		}

		updatedQuorumStatus := livingQuorumStatus.DeepCopy()
		updatedQuorumStatus.Status = newStatus
		return r.client.Status().Update(ctx, updatedQuorumStatus)
	})
}

func (r *InstanceReconciler) shouldManageFailoverQuorumObject(ctx context.Context, cluster *apiv1.Cluster) bool {
	contextLogger := log.FromContext(ctx)

	if cluster.Status.TargetPrimary != r.instance.GetPodName() {
		return false
	}
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return false
	}
	if cluster.Spec.PostgresConfiguration.Synchronous == nil {
		return false
	}

	failoverQuorumActive, err := cluster.IsFailoverQuorumActive()
	if err != nil {
		contextLogger.Error(err, "Failed to determine if sync quorum is active")
		failoverQuorumActive = false
	}

	if !failoverQuorumActive {
		return false
	}

	return true
}
