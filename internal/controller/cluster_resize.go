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
		resizedPod := resizedPodFor(ctx, pod, cluster)
		if err := r.resizeInstanceInPlace(ctx, cluster, pod, resizedPod); err != nil {
			return fmt.Errorf("while resizing pod %s in place: %w", pod.Name, err)
		}
	}

	return nil
}

// resizedPodFor returns the pod with the new resources when they are the
// only difference with the target pod spec and they can be applied to a
// running pod, nil otherwise
func resizedPodFor(ctx context.Context, pod *corev1.Pod, cluster *apiv1.Cluster) *corev1.Pod {
	var storedPodSpec corev1.PodSpec
	if err := json.Unmarshal([]byte(pod.Annotations[utils.PodSpecAnnotationName]), &storedPodSpec); err != nil {
		// Without a valid pod spec annotation there is no record of the
		// resources the pod was created with
		return nil
	}

	targetPod, err := newInstanceForRunningPod(ctx, pod, cluster)
	if err != nil {
		log.FromContext(ctx).Error(err, "while building the target instance to resize the pod", "pod", pod.Name)
		return nil
	}

	if match, _ := specs.ComparePodSpecs(storedPodSpec, targetPod.Spec); match {
		return nil
	}

	resizedPod, _ := specs.ResizePod(pod, &storedPodSpec, &targetPod.Spec)
	return resizedPod
}

// resizeInstanceInPlace brings a running pod to the given resized version,
// if any. When the API server rejects the
// resize, the pod is left untouched for the caller to recreate it.
func (r *ClusterReconciler) resizeInstanceInPlace(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pod *corev1.Pod,
	resizedPod *corev1.Pod,
) (err error) {
	if resizedPod == nil {
		return nil
	}

	// A conflict on the pod means it changed after our read. That change
	// starts a new reconciliation, which decides on the fresh pod. Other
	// conflicts, like one on a resource quota, come with no such event.
	defer func() {
		var status apierrs.APIStatus
		if apierrs.IsConflict(err) && errors.As(err, &status) &&
			status.Status().Details != nil && status.Status().Details.Kind == "pods" {
			log.FromContext(ctx).Debug("Conflict while resizing the instance pod in place",
				"pod", pod.Name, "error", err.Error())
			err = ErrNextLoop
		}
	}()

	// The spec is already resized when a previous attempt failed to
	// refresh the annotation. The lock makes a stale read fail with a
	// conflict.
	lock := []client.MergeFromOption{client.MergeFromWithOptimisticLock{}}
	resized := pod.DeepCopy()
	if !equality.Semantic.DeepEqual(pod.Spec, resizedPod.Spec) {
		resized.Spec = resizedPod.Spec
		// The API server decides what it and the node support: with the
		// feature off there is no resize subresource (NotFound), and a
		// resize the node or a LimitRange cannot accept is forbidden.
		err = r.SubResource("resize").Patch(ctx, resized, client.StrategicMergeFrom(pod, lock...))
		if apierrs.IsInvalid(err) || apierrs.IsBadRequest(err) || apierrs.IsForbidden(err) || apierrs.IsNotFound(err) {
			r.Recorder.Eventf(cluster, "Warning", "InPlaceResizeFailed",
				"In-place resize of pod %s rejected, falling back to a rolling update: %v", pod.Name, err)
			return nil
		}
		if err != nil {
			return err
		}
		r.Recorder.Eventf(cluster, "Normal", "InPlaceResize", "Requested the in-place resize of instance pod %s", pod.Name)
		// The pod is the fresh answer of the API server, and the kubelet
		// is about to update its status: a lock would fail for nothing.
		lock = nil
	}

	// The annotation is refreshed last: if this fails, the pod still looks
	// outdated and the next reconciliation repeats this step only.
	patch := client.MergeFromWithOptions(resized.DeepCopy(), lock...)
	resized.Annotations[utils.PodSpecAnnotationName] = resizedPod.Annotations[utils.PodSpecAnnotationName]
	if err = r.Patch(ctx, resized, patch); err != nil {
		return fmt.Errorf("while refreshing the pod spec annotation after the in-place resize: %w", err)
	}
	*pod = *resized

	return nil
}
