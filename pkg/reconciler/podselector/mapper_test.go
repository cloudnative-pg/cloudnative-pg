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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("clusterMatchesPod", func() {
	It("returns true when pod labels match a selector", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PodSelectorRefs: []apiv1.PodSelectorRef{
					{
						Name: "app-pods",
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "myapp"},
						},
					},
				},
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "myapp"},
			},
		}
		Expect(clusterMatchesPod(cluster, pod)).To(BeTrue())
	})

	It("returns false when pod labels don't match any selector", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PodSelectorRefs: []apiv1.PodSelectorRef{
					{
						Name: "app-pods",
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "myapp"},
						},
					},
				},
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "other"},
			},
		}
		Expect(clusterMatchesPod(cluster, pod)).To(BeFalse())
	})

	It("returns false when cluster has no podSelectorRefs", func() {
		cluster := &apiv1.Cluster{}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "myapp"},
			},
		}
		Expect(clusterMatchesPod(cluster, pod)).To(BeFalse())
	})

	It("matches when any of multiple selectors match", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PodSelectorRefs: []apiv1.PodSelectorRef{
					{
						Name: "app-pods",
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "myapp"},
						},
					},
					{
						Name: "monitoring",
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"role": "monitor"},
						},
					},
				},
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"role": "monitor"},
			},
		}
		Expect(clusterMatchesPod(cluster, pod)).To(BeTrue())
	})

	It("matches using matchExpressions", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PodSelectorRefs: []apiv1.PodSelectorRef{
					{
						Name: "app-pods",
						Selector: metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"myapp", "otherapp"},
								},
							},
						},
					},
				},
			},
		}
		matchingPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "myapp"},
			},
		}
		nonMatchingPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "unrelated"},
			},
		}
		Expect(clusterMatchesPod(cluster, matchingPod)).To(BeTrue())
		Expect(clusterMatchesPod(cluster, nonMatchingPod)).To(BeFalse())
	})

	It("skips invalid selectors without erroring", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PodSelectorRefs: []apiv1.PodSelectorRef{
					{
						Name: "bad-selector",
						Selector: metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: "InvalidOperator",
								},
							},
						},
					},
				},
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "myapp"},
			},
		}
		Expect(clusterMatchesPod(cluster, pod)).To(BeFalse())
	})
})

var _ = Describe("MapExternalPodsToClusters", func() {
	const namespace = "default"

	It("returns requests for clusters whose selectors match the pod", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-cluster",
				Namespace: namespace,
			},
			Spec: apiv1.ClusterSpec{
				PodSelectorRefs: []apiv1.PodSelectorRef{
					{
						Name: "app-pods",
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "myapp"},
						},
					},
				},
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "matching-pod",
				Namespace: namespace,
				Labels:    map[string]string{"app": "myapp"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "img"}},
			},
		}
		fakeClient := newFakeClient(cluster, pod)
		mapFn := MapExternalPodsToClusters(fakeClient)

		requests := mapFn(context.Background(), pod)
		Expect(requests).To(HaveLen(1))
		Expect(requests[0].Name).To(Equal("my-cluster"))
	})

	It("returns no requests when no cluster selectors match", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-cluster",
				Namespace: namespace,
			},
			Spec: apiv1.ClusterSpec{
				PodSelectorRefs: []apiv1.PodSelectorRef{
					{
						Name: "app-pods",
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "myapp"},
						},
					},
				},
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unrelated-pod",
				Namespace: namespace,
				Labels:    map[string]string{"app": "other"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "img"}},
			},
		}
		fakeClient := newFakeClient(cluster, pod)
		mapFn := MapExternalPodsToClusters(fakeClient)

		requests := mapFn(context.Background(), pod)
		Expect(requests).To(BeEmpty())
	})

	It("skips clusters with no podSelectorRefs", func() {
		clusterWithRefs := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-with-refs",
				Namespace: namespace,
			},
			Spec: apiv1.ClusterSpec{
				PodSelectorRefs: []apiv1.PodSelectorRef{
					{
						Name: "app-pods",
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "myapp"},
						},
					},
				},
			},
		}
		clusterWithoutRefs := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-no-refs",
				Namespace: namespace,
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "matching-pod",
				Namespace: namespace,
				Labels:    map[string]string{"app": "myapp"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "img"}},
			},
		}
		fakeClient := newFakeClient(clusterWithRefs, clusterWithoutRefs, pod)
		mapFn := MapExternalPodsToClusters(fakeClient)

		requests := mapFn(context.Background(), pod)
		Expect(requests).To(HaveLen(1))
		Expect(requests[0].Name).To(Equal("cluster-with-refs"))
	})
})
