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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Unrecoverable replicas", func() {
	DescribeTable(
		"unrecoverable annotation parsing",
		func(ctx SpecContext, hasAnnotation bool, value string, isUnrecoverable bool) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			}

			if hasAnnotation {
				pod.Annotations[utils.UnrecoverableInstanceAnnotationName] = value
			}

			Expect(isPodUnrecoverable(ctx, pod)).To(Equal(isUnrecoverable))
		},
		Entry("unrecoverable instance", true, "true", true),
		Entry("not unrecoverable instance", true, "false", false),
		Entry("instance without annotation", false, "", false),
		Entry("instance with empty annotation", true, "", false),
	)

	It("Detects unrecoverable instances", func(ctx SpecContext) {
		makePodWithUnrecoverableAnnotation := func(name, v string) corev1.Pod {
			return corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
					Annotations: map[string]string{
						utils.UnrecoverableInstanceAnnotationName: v,
					},
				},
			}
		}

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{},
			Spec: apiv1.ClusterSpec{
				Instances: 5,
			},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "cluster-example-1",
				TargetPrimary:  "cluster-example-2",
			},
		}

		// this pod won't be deleted even if it is marked unrecoverable because it is
		// the current primary
		currentPrimaryPod := makePodWithUnrecoverableAnnotation("cluster-example-1", "true")

		// this pod won't be deleted even if it is marked unrecoverable because it is
		// the target primary
		targetPrimaryPod := makePodWithUnrecoverableAnnotation("cluster-example-2", "true")

		// this pod will be deleted as it is not the primary nor the candidate primary and is
		// unrecoverable
		unrecoverablePodFour := makePodWithUnrecoverableAnnotation("cluster-example-4", "true")

		// this is a standard instance
		instanceFive := makePodWithUnrecoverableAnnotation("cluster-example-5", "false")

		// this pod will be deleted as it is not the primary nor the candidate primary and is
		// unrecoverable
		unrecoverablePodThree := makePodWithUnrecoverableAnnotation("cluster-example-3", "true")

		result := collectNamesOfUnrecoverableInstances(
			ctx,
			cluster,
			&managedResources{
				instances: corev1.PodList{
					Items: []corev1.Pod{
						currentPrimaryPod,
						targetPrimaryPod,
						unrecoverablePodThree,
						unrecoverablePodFour,
						instanceFive,
					},
				},
			},
		)

		Expect(result).To(ConsistOf("cluster-example-3", "cluster-example-4"))
	})

	It("Detects unrecoverable instances even when pods are not ready", func(ctx SpecContext) {
		makeNonReadyPod := func(name string, unrecoverable string) corev1.Pod {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
					Annotations: map[string]string{
						utils.UnrecoverableInstanceAnnotationName: unrecoverable,
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}
			return pod
		}

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{},
			Spec: apiv1.ClusterSpec{
				Instances: 2,
			},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "cluster-example-1",
				TargetPrimary:  "cluster-example-1",
			},
		}

		// Primary pod is ready
		primaryPod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "cluster-example-1",
				Annotations: map[string]string{},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		// Replica pod is not ready (e.g., postgres process not running, startup probe
		// failing) but annotated as unrecoverable by the user
		nonReadyUnrecoverablePod := makeNonReadyPod("cluster-example-2", "true")

		result := collectNamesOfUnrecoverableInstances(
			ctx,
			cluster,
			&managedResources{
				instances: corev1.PodList{
					Items: []corev1.Pod{
						primaryPod,
						nonReadyUnrecoverablePod,
					},
				},
			},
		)

		Expect(result).To(ConsistOf("cluster-example-2"))
	})
})
