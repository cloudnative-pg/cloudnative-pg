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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"

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
