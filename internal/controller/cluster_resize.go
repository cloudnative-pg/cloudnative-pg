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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// resizeInstancesInPlace applies to the running instance pods the changes to
// the container resources that do not require recreating them, when the
// cluster asks for it. It runs before the rollout: a resized pod has its
// pod spec annotation aligned with the new resources and is seen as
// up-to-date, while any other pod is left to the rolling update.
func (r *ClusterReconciler) resizeInstancesInPlace(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instances *postgres.PostgresqlStatusList,
) error {
	if cluster.Spec.ResourcesUpdateStrategy != apiv1.ResourcesUpdateStrategyInPlace ||
		utils.IsPodSpecReconciliationDisabled(&cluster.ObjectMeta) {
		return nil
	}

	for i := range instances.Items {
		pod := instances.Items[i].Pod
		if cluster.IsInstanceFenced(pod.Name) {
			continue
		}
		if err := r.resizeInstanceInPlace(ctx, cluster, pod); err != nil {
			return fmt.Errorf("while resizing pod %s in place: %w", pod.Name, err)
		}
	}

	return nil
}

// resizeInstanceInPlace resizes an instance pod through the resize
// subresource when its stored pod spec differs from the target one only in
// container resources that can be changed on a running pod, then records the
// new resources in the pod spec annotation. The passed pod is updated too.
//
// The resize comes first: should recording it fail, the next reconciliation
// finds the live resources already in place and only repairs the annotation.
func (r *ClusterReconciler) resizeInstanceInPlace(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pod *corev1.Pod,
) (err error) {
	contextLogger := log.FromContext(ctx).WithValues("pod", pod.Name)

	// A conflict on the pod means it changed after our read. That change
	// starts a new reconciliation, which decides on the fresh pod. Other
	// conflicts, like one on a resource quota, come with no such event.
	defer func() {
		var status apierrs.APIStatus
		if apierrs.IsConflict(err) && errors.As(err, &status) &&
			status.Status().Details != nil && status.Status().Details.Kind == "pods" {
			contextLogger.Debug("Conflict while resizing the instance pod in place", "error", err.Error())
			err = ErrNextLoop
		}
	}()

	var storedPodSpec corev1.PodSpec
	if err := json.Unmarshal([]byte(pod.Annotations[utils.PodSpecAnnotationName]), &storedPodSpec); err != nil {
		// Without a valid pod spec annotation there is no record of the
		// resources the pod was created with
		return nil
	}

	targetPod, err := newInstanceForRunningPod(ctx, pod, cluster)
	if err != nil {
		contextLogger.Error(err, "while building the target instance to resize the pod")
		return nil
	}

	if match, _ := specs.ComparePodSpecs(storedPodSpec, targetPod.Spec); match {
		return nil
	}

	resizedPod := pod.DeepCopy()
	if !specs.ResizeInPlace(&storedPodSpec, &resizedPod.Spec, &targetPod.Spec, &pod.Status) {
		return nil
	}
	if match, _ := specs.ComparePodSpecs(storedPodSpec, targetPod.Spec); !match {
		// The resources are not the only change: the pod will be recreated
		return nil
	}

	// The lock makes a stale read fail with a conflict
	lock := []client.MergeFromOption{client.MergeFromWithOptimisticLock{}}
	if !equality.Semantic.DeepEqual(pod.Spec, resizedPod.Spec) {
		contextLogger.Info("Resizing instance pod in place")

		// A strategic merge patch carries only the resized containers. A
		// merge patch would carry the whole containers list, reverting any
		// container whose resources changed after the cache was populated,
		// including those injected by other actors, such as a service mesh.
		if err := r.SubResource("resize").Patch(ctx, resizedPod, client.StrategicMergeFrom(pod, lock...)); err != nil {
			// The resize cannot be applied to this pod: for example it would
			// change the pod QoS class, or the node or the API server do not
			// support it. The pod spec annotation is left untouched, so the
			// rollout recreates the pod.
			if apierrs.IsInvalid(err) || apierrs.IsBadRequest(err) ||
				apierrs.IsForbidden(err) || apierrs.IsNotFound(err) {
				r.Recorder.Eventf(cluster, "Warning", "InPlaceResizeFailed",
					"In-place resize of pod %s rejected, falling back to a rolling update: %v", pod.Name, err)
				return nil
			}
			return err
		}

		r.Recorder.Eventf(cluster, "Normal", "InPlaceResize",
			"Resized instance pod %s in place", pod.Name)
		// The pod is now the fresh answer of the API server, and the
		// kubelet is about to update its status: a lock would fail for nothing
		lock = nil
	}

	podSpecAnnotation, err := json.Marshal(storedPodSpec)
	if err != nil {
		return fmt.Errorf("while marshalling the pod spec annotation: %w", err)
	}

	origPod := pod.DeepCopy()
	pod.Annotations[utils.PodSpecAnnotationName] = string(podSpecAnnotation)
	return r.Patch(ctx, pod, client.MergeFromWithOptions(origPod, lock...))
}
