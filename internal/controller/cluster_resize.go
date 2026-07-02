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
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// errInPlaceResizeRejected is raised when the API server rejects an in-place
// resize of the resources of an instance pod, for example because the change
// would alter the pod QoS class. The caller reacts by falling back to the
// standard rollout, recreating the pod.
var errInPlaceResizeRejected = errors.New("in-place pod resize rejected")

// resizeInstanceInPlace applies a resource-only drift to a running instance
// pod through the resize subresource, then refreshes the pod spec annotation
// so that the drift detection quiesces. A failure to refresh the annotation
// is tolerated: the next reconciliation finds the live resources already
// up-to-date and only repairs the annotation.
func (r *ClusterReconciler) resizeInstanceInPlace(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pod *corev1.Pod,
	reason string,
) error {
	contextLogger := log.FromContext(ctx)

	targetPod, err := newInstanceForRunningPod(ctx, pod, cluster)
	if err != nil {
		return fmt.Errorf("while building the target instance to resize the pod: %w", err)
	}

	if drifts := specs.GetResizableContainerResourceDrifts(&pod.Spec, &targetPod.Spec); len(drifts) > 0 {
		contextLogger.Info("Resizing instance pod in place",
			"pod", pod.Name,
			"reason", reason)

		patchedPod := pod.DeepCopy()
		applyContainerResourceDrifts(&patchedPod.Spec, drifts)
		if err := r.SubResource("resize").Patch(ctx, patchedPod, client.MergeFrom(pod)); err != nil {
			if apierrs.IsInvalid(err) || apierrs.IsBadRequest(err) || apierrs.IsForbidden(err) {
				r.Recorder.Eventf(cluster, "Warning", "InPlaceResizeFailed",
					"In-place resize of pod %s rejected, falling back to a rolling update: %v", pod.Name, err)
				return fmt.Errorf("%w: %v", errInPlaceResizeRejected, err)
			}
			return err
		}

		r.Recorder.Eventf(cluster, "Normal", "InPlaceResize",
			"Resized instance pod %s in place", pod.Name)
	}

	if err := r.refreshPodSpecAnnotationResources(ctx, pod, targetPod); err != nil {
		contextLogger.Info(
			"Cannot refresh the pod spec annotation after the in-place resize, will retry",
			"pod", pod.Name,
			"error", err.Error())
	}

	return nil
}

// refreshPodSpecAnnotationResources aligns the container resources recorded
// in the pod spec annotation with the target instance. Only the resources are
// overwritten: the rest of the stored spec is preserved, as it may
// legitimately differ from a freshly built instance (for example, the
// bootstrap init container image when in-place instance manager updates are
// enabled).
func (r *ClusterReconciler) refreshPodSpecAnnotationResources(
	ctx context.Context,
	pod *corev1.Pod,
	targetPod *corev1.Pod,
) error {
	podSpecAnnotation, ok := pod.Annotations[utils.PodSpecAnnotationName]
	if !ok {
		return nil
	}

	var storedPodSpec corev1.PodSpec
	if err := json.Unmarshal([]byte(podSpecAnnotation), &storedPodSpec); err != nil {
		return fmt.Errorf("while unmarshalling the pod spec annotation: %w", err)
	}

	drifts := specs.GetContainerResourceDrifts(&storedPodSpec, &targetPod.Spec)
	if len(drifts) == 0 {
		return nil
	}
	applyContainerResourceDrifts(&storedPodSpec, drifts)

	podSpecMarshaled, err := json.Marshal(storedPodSpec)
	if err != nil {
		return fmt.Errorf("while marshalling the pod spec annotation: %w", err)
	}

	patchedPod := pod.DeepCopy()
	patchedPod.Annotations[utils.PodSpecAnnotationName] = string(podSpecMarshaled)
	return r.Patch(ctx, patchedPod, client.MergeFrom(pod))
}

// applyContainerResourceDrifts sets the resources of the drifted containers
// of the given pod spec to their target value
func applyContainerResourceDrifts(spec *corev1.PodSpec, drifts []specs.ContainerResourceDrift) {
	for _, drift := range drifts {
		containers := spec.Containers
		if drift.InitContainer {
			containers = spec.InitContainers
		}
		for i := range containers {
			if containers[i].Name == drift.Name {
				containers[i].Resources = drift.Target
				break
			}
		}
	}
}
