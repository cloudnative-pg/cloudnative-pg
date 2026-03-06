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

package podselector

import (
	"context"
	"slices"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
)

// Reconcile resolves each podSelectorRef to pod IPs and updates cluster status.
func Reconcile(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
) error {
	contextLogger := log.FromContext(ctx)

	selectorRefs := cluster.Spec.PodSelectorRefs
	if len(selectorRefs) == 0 {
		// Clear status if no selectors defined
		if len(cluster.Status.PodSelectorRefs) > 0 {
			return status.PatchWithOptimisticLock(
				ctx, c, cluster,
				func(cluster *apiv1.Cluster) {
					cluster.Status.PodSelectorRefs = nil
				},
			)
		}
		return nil
	}

	resolved := make([]apiv1.PodSelectorRefStatus, 0, len(selectorRefs))

	for i := range selectorRefs {
		ref := &selectorRefs[i]

		selector, err := metav1.LabelSelectorAsSelector(&ref.Selector)
		if err != nil {
			contextLogger.Error(err, "Invalid label selector", "selectorRef", ref.Name)
			resolved = append(resolved, apiv1.PodSelectorRefStatus{Name: ref.Name})
			continue
		}

		var podList corev1.PodList
		if err := c.List(ctx, &podList,
			client.InNamespace(cluster.Namespace),
			client.MatchingLabelsSelector{Selector: selector},
		); err != nil {
			return err
		}

		// Collect unique IPs from non-terminating pods (including all
		// addresses for dual-stack pods).
		ipSet := stringset.New()
		for j := range podList.Items {
			pod := &podList.Items[j]
			if pod.DeletionTimestamp != nil {
				continue
			}
			for _, podIP := range pod.Status.PodIPs {
				if podIP.IP != "" {
					ipSet.Put(podIP.IP)
				}
			}
		}

		resolved = append(resolved, apiv1.PodSelectorRefStatus{
			Name: ref.Name,
			IPs:  ipSet.ToSortedList(),
		})
	}

	// Skip the status patch if nothing changed
	if slices.EqualFunc(cluster.Status.PodSelectorRefs, resolved, func(a, b apiv1.PodSelectorRefStatus) bool {
		return a.Name == b.Name && slices.Equal(a.IPs, b.IPs)
	}) {
		return nil
	}

	return status.PatchWithOptimisticLock(
		ctx, c, cluster,
		func(cluster *apiv1.Cluster) {
			cluster.Status.PodSelectorRefs = resolved
		},
	)
}
