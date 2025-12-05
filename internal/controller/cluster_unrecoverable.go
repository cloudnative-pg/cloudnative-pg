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
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// reconcileUnrecoverableInstances handles the instances whose storage is not
// recoverable by deleting their PVC and their Pods.
func (r *ClusterReconciler) reconcileUnrecoverableInstances(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	targetInstances := collectNamesOfUnrecoverableInstances(ctx, cluster, resources)
	if len(targetInstances) > 0 {
		podName := targetInstances[0]

		logger.Info("Deleting unrecoverable instance", "podName", podName)
		if err := r.ensureInstanceIsDeleted(ctx, cluster, podName); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func collectNamesOfUnrecoverableInstances(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) []string {
	instancesToDelete := stringset.New()

	logger := log.FromContext(ctx)

	protectedInstances := stringset.New()
	protectedInstances.Put(cluster.Status.TargetPrimary)
	protectedInstances.Put(cluster.Status.CurrentPrimary)

	for _, pod := range resources.instances.Items {
		if !isPodUnrecoverable(ctx, &pod) {
			continue
		}

		if protectedInstances.Has(pod.Name) {
			logger.Info(
				"Refusing to delete protected instances even if they are unrecoverable",
				"podName", pod.Name,
				"targetPrimary", cluster.Status.TargetPrimary,
				"currentPrimary", cluster.Status.CurrentPrimary,
				"protectedInstances", protectedInstances.ToSortedList(),
			)
			continue
		}

		instancesToDelete.Put(pod.Name)
	}

	// We sort the pods to have a deterministic behavior
	return instancesToDelete.ToSortedList()
}

// isPodUnrecoverable checks if a Pod is declared unrecoverable
// looking at its annotation
func isPodUnrecoverable(ctx context.Context, pod *corev1.Pod) bool {
	logger := log.FromContext(ctx)

	s, ok := pod.Annotations[utils.UnrecoverableInstanceAnnotationName]
	if !ok || s == "" {
		return false
	}

	v, err := strconv.ParseBool(s)
	if err != nil {
		logger.Warning("Invalid unrecoverable annotation content, skipping",
			"podName", pod.Name,
			"value", s,
			"err", err.Error())
		return false
	}

	return v
}
