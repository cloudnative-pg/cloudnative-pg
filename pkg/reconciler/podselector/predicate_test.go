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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/event"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ExternalPodsPredicate", func() {
	pred := ExternalPodsPredicate()

	It("allows pods not owned by a Cluster", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "external-pod",
			},
		}
		podWithIP := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "external-pod",
			},
			Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.1"}}},
		}
		Expect(pred.CreateFunc(event.CreateEvent{Object: pod})).To(BeTrue())
		Expect(pred.UpdateFunc(event.UpdateEvent{ObjectOld: pod, ObjectNew: podWithIP})).To(BeTrue())
		Expect(pred.DeleteFunc(event.DeleteEvent{Object: pod})).To(BeTrue())
		Expect(pred.GenericFunc(event.GenericEvent{Object: pod})).To(BeTrue())
	})

	It("rejects pods owned by a Cluster", func() {
		clusterOwner := []metav1.OwnerReference{
			{
				APIVersion: apiv1.SchemeGroupVersion.String(),
				Kind:       apiv1.ClusterKind,
				Name:       "test-cluster",
				Controller: ptr.To(true),
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "cluster-pod",
				OwnerReferences: clusterOwner,
			},
		}
		podWithIP := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "cluster-pod",
				OwnerReferences: clusterOwner,
			},
			Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.1"}}},
		}
		Expect(pred.CreateFunc(event.CreateEvent{Object: pod})).To(BeFalse())
		Expect(pred.UpdateFunc(event.UpdateEvent{ObjectOld: pod, ObjectNew: podWithIP})).To(BeFalse())
		Expect(pred.DeleteFunc(event.DeleteEvent{Object: pod})).To(BeFalse())
		Expect(pred.GenericFunc(event.GenericEvent{Object: pod})).To(BeFalse())
	})

	It("filters updates where only IP changed", func() {
		oldPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "external-pod"},
			Status:     corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.1"}}},
		}
		newPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "external-pod"},
			Status:     corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.2"}}},
		}
		Expect(pred.UpdateFunc(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod})).To(BeTrue())
	})

	It("filters out updates where nothing relevant changed", func() {
		oldPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "external-pod",
				Labels: map[string]string{"app": "myapp"},
			},
			Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.1"}}},
		}
		newPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "external-pod",
				Labels:          map[string]string{"app": "myapp"},
				ResourceVersion: "2",
			},
			Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.1"}}},
		}
		Expect(pred.UpdateFunc(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod})).To(BeFalse())
	})
})
