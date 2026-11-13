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
	"errors"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// resizeInstanceInPlace brings a running pod to the given resized version,
// if any, and reports whether it did. When the API server rejects the
// resize, the pod is left untouched for the caller to recreate it.
func (r *ClusterReconciler) resizeInstanceInPlace(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pod *corev1.Pod,
	resizedPod *corev1.Pod,
) (_ bool, err error) {
	if resizedPod == nil {
		return false, nil
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
			return false, nil
		}
		if err != nil {
			return false, err
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
		return false, fmt.Errorf("while refreshing the pod spec annotation after the in-place resize: %w", err)
	}
	*pod = *resized

	return true, nil
}
