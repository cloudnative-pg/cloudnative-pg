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

var _ = Describe("Reconcile", func() {
	const namespace = "default"

	It("clears status when no selectors are defined", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: namespace,
			},
			Status: apiv1.ClusterStatus{
				PodSelectorRefs: []apiv1.PodSelectorRefStatus{
					{Name: "old", IPs: []string{"10.0.0.1"}},
				},
			},
		}
		fakeClient := newFakeClient(cluster)

		err := Reconcile(context.Background(), fakeClient, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Status.PodSelectorRefs).To(BeNil())
	})

	It("resolves pod IPs for matching selectors", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
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
		pod1 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: namespace,
				Labels:    map[string]string{"app": "myapp"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "img"}},
			},
			Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.5"}}},
		}
		pod2 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: namespace,
				Labels:    map[string]string{"app": "myapp"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "img"}},
			},
			Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.12"}}},
		}
		fakeClient := newFakeClient(cluster, pod1, pod2)

		err := Reconcile(context.Background(), fakeClient, cluster)
		Expect(err).ToNot(HaveOccurred())

		Expect(cluster.Status.PodSelectorRefs).To(HaveLen(1))
		Expect(cluster.Status.PodSelectorRefs[0].Name).To(Equal("app-pods"))
		Expect(cluster.Status.PodSelectorRefs[0].IPs).To(ConsistOf("10.0.0.5", "10.0.0.12"))
	})

	It("returns empty IPs when no pods match", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: namespace,
			},
			Spec: apiv1.ClusterSpec{
				PodSelectorRefs: []apiv1.PodSelectorRef{
					{
						Name: "app-pods",
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "nonexistent"},
						},
					},
				},
			},
		}
		fakeClient := newFakeClient(cluster)

		err := Reconcile(context.Background(), fakeClient, cluster)
		Expect(err).ToNot(HaveOccurred())

		Expect(cluster.Status.PodSelectorRefs).To(HaveLen(1))
		Expect(cluster.Status.PodSelectorRefs[0].Name).To(Equal("app-pods"))
		Expect(cluster.Status.PodSelectorRefs[0].IPs).To(BeEmpty())
	})

	It("excludes terminating pods from resolved IPs", func() {
		now := metav1.Now()
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
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
		healthyPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "healthy-pod",
				Namespace: namespace,
				Labels:    map[string]string{"app": "myapp"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "img"}},
			},
			Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.5"}}},
		}
		terminatingPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "terminating-pod",
				Namespace:         namespace,
				Labels:            map[string]string{"app": "myapp"},
				DeletionTimestamp: &now,
				Finalizers:        []string{"test-finalizer"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "img"}},
			},
			Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.99"}}},
		}
		fakeClient := newFakeClient(cluster, healthyPod, terminatingPod)

		err := Reconcile(context.Background(), fakeClient, cluster)
		Expect(err).ToNot(HaveOccurred())

		Expect(cluster.Status.PodSelectorRefs).To(HaveLen(1))
		Expect(cluster.Status.PodSelectorRefs[0].IPs).To(ConsistOf("10.0.0.5"))
	})

	It("does nothing when no selectors are defined and status is empty", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: namespace,
			},
		}
		fakeClient := newFakeClient(cluster)

		err := Reconcile(context.Background(), fakeClient, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Status.PodSelectorRefs).To(BeNil())
	})
})
