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

package hibernation

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// Reconcile reconciles the cluster hibernation status.
func Reconcile(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	instances []corev1.Pod,
) (*ctrl.Result, error) {
	hibernationCondition := meta.FindStatusCondition(cluster.Status.Conditions, HibernationConditionType)
	if hibernationCondition == nil {
		// This means that hibernation has not been requested
		return nil, nil
	}

	switch hibernationCondition.Reason {
	case HibernationConditionReasonDeletingPods:
		return reconcileDeletePods(ctx, c, instances)

	case HibernationConditionReasonWaitingPodsDeletion:
		return &ctrl.Result{RequeueAfter: 5 * time.Second}, nil

	default:
		return &ctrl.Result{}, nil
	}
}

func reconcileDeletePods(
	ctx context.Context,
	c client.Client,
	instances []corev1.Pod,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if len(instances) == 0 {
		return &ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	var podToBeDeleted *corev1.Pod
	for idx := range instances {
		if specs.IsPodPrimary(instances[idx]) {
			podToBeDeleted = &instances[idx]
		}
	}

	if podToBeDeleted == nil {
		// The primary Pod has already been deleted, we can
		// delete the replicas
		podToBeDeleted = &instances[0]
	}

	// The Pod list is sorted and the primary instance
	// will always be the first one, if present
	contextLogger.Info("Deleting Pod as requested by the hibernation procedure", "podName", podToBeDeleted.Name)
	deletionResult := c.Delete(ctx, podToBeDeleted)
	return &ctrl.Result{RequeueAfter: 5 * time.Second}, deletionResult
}
