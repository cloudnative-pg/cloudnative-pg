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

package controllers

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cluster_scale unit tests", func() {
	It("should make sure that scale down works correctly", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)

		resources := &managedResources{
			pvcs: corev1.PersistentVolumeClaimList{Items: generateFakePVC(cluster)},
			jobs: batchv1.JobList{Items: generateFakeInitDBJobs(cluster)},
			pods: corev1.PodList{Items: generateFakeClusterPods(cluster, true)},
		}

		sacrificialPodBefore := getSacrificialPod(resources.pods.Items)
		err := k8sClient.Get(
			ctx,
			types.NamespacedName{Name: sacrificialPodBefore.Name, Namespace: cluster.Namespace},
			&corev1.Pod{},
		)
		Expect(err).To(BeNil())

		err = clusterReconciler.scaleDownCluster(
			ctx,
			cluster,
			resources,
		)
		Expect(err).To(BeNil())

		sacrificialPod := getSacrificialPod(resources.pods.Items)
		err = k8sClient.Get(
			ctx,
			types.NamespacedName{Name: sacrificialPod.Name, Namespace: cluster.Namespace},
			&corev1.Pod{},
		)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})
