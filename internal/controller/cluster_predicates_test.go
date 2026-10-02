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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("nodesPredicate", func() {
	fakeReconciler := &ClusterReconciler{
		drainTaints: configuration.DefaultDrainTaints,
	}
	nodesPredicateFunctions := fakeReconciler.nodesPredicate()

	pod := &corev1.Pod{}
	nodeWithNoTaints := &corev1.Node{}
	unschedulableNode := &corev1.Node{
		Spec: corev1.NodeSpec{
			Unschedulable: true,
		},
	}
	nodeWithKarpenterNoSchedulableTaint := &corev1.Node{
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{
					Key:    "karpenter.sh/disrupted",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
	}
	nodeWithKarpenterNoExecuteTaint := &corev1.Node{
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{
					Key:    "karpenter.sh/disrupted",
					Effect: corev1.TaintEffectNoExecute,
				},
			},
		},
	}
	nodeWithAutoscalerTaint := &corev1.Node{
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{
					Key: "ToBeDeletedByClusterAutoscaler",
				},
			},
		},
	}

	DescribeTable(
		"always skips node creation",
		func(node client.Object, expectedResult bool) {
			createEvent := event.CreateEvent{
				Object: node,
			}

			result := nodesPredicateFunctions.Create(createEvent)
			Expect(result).To(Equal(expectedResult))
		},
		Entry("with a node", nodeWithNoTaints, false),
		Entry("with a pod", pod, false),
	)

	DescribeTable(
		"always skips node delete",
		func(node client.Object, expectedResult bool) {
			deleteEvent := event.DeleteEvent{
				Object: node,
			}

			result := nodesPredicateFunctions.Delete(deleteEvent)
			Expect(result).To(Equal(expectedResult))
		},
		Entry("with a node", nodeWithNoTaints, false),
		Entry("with a pod", pod, false),
	)

	DescribeTable(
		"always skips generic events",
		func(node client.Object, expectedResult bool) {
			genericEvent := event.GenericEvent{
				Object: node,
			}

			result := nodesPredicateFunctions.Generic(genericEvent)
			Expect(result).To(Equal(expectedResult))
		},
		Entry("with a node", nodeWithNoTaints, false),
		Entry("with a pod", pod, false),
	)

	DescribeTable(
		"node updates",
		func(objectOld, objectNew client.Object, expectedResult bool) {
			updateEventOldToNew := event.UpdateEvent{
				ObjectOld: objectOld,
				ObjectNew: objectNew,
			}
			updateEventNewToOld := event.UpdateEvent{
				ObjectOld: objectOld,
				ObjectNew: objectNew,
			}

			result := nodesPredicateFunctions.Update(updateEventOldToNew)
			Expect(result).To(Equal(expectedResult))

			result = nodesPredicateFunctions.Update(updateEventNewToOld)
			Expect(result).To(Equal(expectedResult))
		},
		Entry("with the same node",
			nodeWithNoTaints, nodeWithNoTaints, false),
		Entry("with the same tainted node",
			nodeWithKarpenterNoSchedulableTaint, nodeWithKarpenterNoSchedulableTaint, false),
		Entry("when a node becomes unschedulable",
			nodeWithNoTaints, unschedulableNode, true),
		Entry("when a node gets the karpenter disruption taint",
			nodeWithNoTaints, nodeWithKarpenterNoSchedulableTaint, true),
		Entry("when a node gets the karpenter disruption taint value changed",
			nodeWithKarpenterNoSchedulableTaint, nodeWithKarpenterNoExecuteTaint, true),
		Entry("when a node taints changed",
			nodeWithKarpenterNoSchedulableTaint, nodeWithAutoscalerTaint, true),
	)
})

var _ = Describe("isBeingDeletedPredicate", func() {
	deletionTime := metav1.Now()
	beingDeleted := &apiv1.Database{
		ObjectMeta: metav1.ObjectMeta{
			DeletionTimestamp: &deletionTime,
			Finalizers:        []string{utils.DatabaseFinalizerName},
		},
	}
	notBeingDeleted := &apiv1.Database{}

	// The create event matters most: on an operator restart the initial cache
	// sync delivers lingering resources as create events, and that is the moment
	// the deletion cleanup must be re-triggered.
	It("admits an object carrying a deletionTimestamp on every event type", func() {
		Expect(isBeingDeletedPredicate.Create(event.CreateEvent{Object: beingDeleted})).To(BeTrue())
		Expect(isBeingDeletedPredicate.Update(event.UpdateEvent{ObjectNew: beingDeleted})).To(BeTrue())
		Expect(isBeingDeletedPredicate.Delete(event.DeleteEvent{Object: beingDeleted})).To(BeTrue())
		Expect(isBeingDeletedPredicate.Generic(event.GenericEvent{Object: beingDeleted})).To(BeTrue())
	})

	It("admits an object that is not being deleted but belongs to the initialization scan", func() {
		Expect(isBeingDeletedPredicate.Create(event.CreateEvent{
			Object:          notBeingDeleted,
			IsInInitialList: true,
		})).To(BeTrue())
	})

	It("rejects an object that is not being deleted on every event type", func() {
		Expect(isBeingDeletedPredicate.Create(event.CreateEvent{Object: notBeingDeleted})).To(BeFalse())
		Expect(isBeingDeletedPredicate.Update(event.UpdateEvent{ObjectNew: notBeingDeleted})).To(BeFalse())
		Expect(isBeingDeletedPredicate.Delete(event.DeleteEvent{Object: notBeingDeleted})).To(BeFalse())
		Expect(isBeingDeletedPredicate.Generic(event.GenericEvent{Object: notBeingDeleted})).To(BeFalse())
	})
})
