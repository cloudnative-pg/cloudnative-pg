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

package persistentvolumeclaim

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LatestGeneratedNode self-heal", func() {
	clusterName := "myCluster"
	makeClusterPVC := func(serial string) corev1.PersistentVolumeClaim {
		return makePVC(clusterName, serial, serial, NewPgDataCalculator(), false)
	}

	It("raises LatestGeneratedNode to the highest observed instance serial", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
			Status:     apiv1.ClusterStatus{LatestGeneratedNode: 0},
		}
		EnrichStatus(ctx, cluster, nil, nil, []corev1.PersistentVolumeClaim{
			makeClusterPVC("1"),
			makeClusterPVC("2"),
		})
		Expect(cluster.Status.LatestGeneratedNode).To(Equal(2))
	})

	It("never lowers LatestGeneratedNode below the persisted value", func(ctx SpecContext) {
		// Stale PVC informer: the counter was already advanced past the PVCs
		// currently visible (the higher PVC has not reached the cache yet).
		// Rolling the counter back would re-allocate a serial that is already
		// in flight, so the self-heal must only ever move it forward.
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
			Status:     apiv1.ClusterStatus{LatestGeneratedNode: 5},
		}
		EnrichStatus(ctx, cluster, nil, nil, []corev1.PersistentVolumeClaim{
			makeClusterPVC("2"),
		})
		Expect(cluster.Status.LatestGeneratedNode).To(Equal(5))
	})
})
