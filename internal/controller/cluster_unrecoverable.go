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
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	if len(targetInstances) == 0 {
		return ctrl.Result{}, nil
	}

	podName := targetInstances[0]

	logger.Info("Deleting unrecoverable instance", "podName", podName)
	r.Recorder.Eventf(cluster, "Normal", "DeleteUnrecoverableInstance",
		"Deleting unrecoverable instance %v (pods and PVCs)", podName)

	// A graceful delete is a no-op on a Pod that is already Terminating, so a Pod
	// stuck past its own deletion deadline would never be removed and its PVCs
	// (blocked by the pvc-protection finalizer) could never be deleted. Force-remove
	// such a Pod first so that ensureInstanceIsDeleted below can make progress.
	if pod := findInstancePodByName(resources, podName); pod != nil && isPodStuckTerminating(pod) {
		logger.Info(
			"Force-removing unrecoverable instance stuck past its deletion deadline",
			"podName", podName,
			"deletionTimestamp", pod.DeletionTimestamp,
		)
		if err := r.Delete(ctx, pod, client.GracePeriodSeconds(0)); err != nil && !apierrs.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	if err := r.ensureInstanceIsDeleted(ctx, cluster, podName); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// findInstancePodByName returns the managed instance Pod with the given name, or
// nil when no such Pod is present in the managed resources.
func findInstancePodByName(resources *managedResources, name string) *corev1.Pod {
	for i := range resources.instances.Items {
		if resources.instances.Items[i].Name == name {
			return &resources.instances.Items[i]
		}
	}
	return nil
}

// isPodStuckTerminating reports whether a Pod has been asked to terminate but is
// still present past its own deletion deadline. DeletionTimestamp is the moment
// (deletion request time plus the termination grace period) at which the object
// is expected to be gone. A Pod that lingers past it has already been given its
// entire termination budget, so it will not disappear on another graceful delete
// and has to be force-removed.
func isPodStuckTerminating(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp != nil && time.Now().After(pod.DeletionTimestamp.Time)
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
