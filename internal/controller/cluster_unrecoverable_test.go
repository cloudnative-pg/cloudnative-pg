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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newUnrecoverableReconciler builds a ClusterReconciler backed by a fake client.
// The job owner index is required because the deletion path lists the instance
// jobs; the interceptor lets tests observe the delete options passed to the client.
func newUnrecoverableReconciler(
	interceptorFuncs interceptor.Funcs,
	initObjs ...client.Object,
) (*ClusterReconciler, *record.FakeRecorder) {
	scheme := schemeBuilder.BuildWithAllKnownScheme()
	recorder := record.NewFakeRecorder(120)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&apiv1.Cluster{}).
		WithIndex(&batchv1.Job{}, jobOwnerKey, jobOwnerIndexFunc).
		WithInterceptorFuncs(interceptorFuncs).
		WithObjects(initObjs...).
		Build()

	return &ClusterReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: recorder,
	}, recorder
}

// unrecoverablePod builds an instance pod annotated as unrecoverable in the given
// phase. A non-nil deletionTime marks the pod as Terminating with that deadline.
func unrecoverablePod(name string, phase corev1.PodPhase, deletionTime *metav1.Time) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Annotations: map[string]string{
				utils.UnrecoverableInstanceAnnotationName: "true",
			},
			DeletionTimestamp: deletionTime,
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}
}

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

	It("Collects unrecoverable instances regardless of pod readiness status", func(ctx SpecContext) {
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

	DescribeTable(
		"Protects the primary even when it is not active",
		func(ctx SpecContext, primaryField string, phase corev1.PodPhase, deletionTime *metav1.Time) {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{Instances: 2},
				Status: apiv1.ClusterStatus{
					CurrentPrimary: "cluster-example-2",
					TargetPrimary:  "cluster-example-2",
				},
			}
			if primaryField == "target" {
				cluster.Status.CurrentPrimary = "cluster-example-1"
			}

			// The protected primary is annotated unrecoverable and in a non-active state
			protectedPrimary := unrecoverablePod("cluster-example-2", phase, deletionTime)
			// A plain unrecoverable replica that must still be collected
			replica := unrecoverablePod("cluster-example-3", corev1.PodPending, nil)

			result := collectNamesOfUnrecoverableInstances(
				ctx,
				cluster,
				&managedResources{
					instances: corev1.PodList{
						Items: []corev1.Pod{protectedPrimary, replica},
					},
				},
			)

			Expect(result).To(ConsistOf("cluster-example-3"))
			Expect(result).NotTo(ContainElement("cluster-example-2"))
		},
		Entry("current primary, Pending", "current", corev1.PodPending, nil),
		Entry("current primary, Terminating", "current", corev1.PodRunning,
			&metav1.Time{Time: time.Now().Add(-time.Minute)}),
		Entry("target primary, Pending", "target", corev1.PodPending, nil),
		Entry("target primary, Terminating", "target", corev1.PodRunning,
			&metav1.Time{Time: time.Now().Add(-time.Minute)}),
	)

	It("Deletes an annotated Pending instance", func(ctx SpecContext) {
		pendingPod := unrecoverablePod("cluster-example-2", corev1.PodPending, nil)
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-example", Namespace: "default"},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "cluster-example-1",
				TargetPrimary:  "cluster-example-1",
			},
		}

		r, recorder := newUnrecoverableReconciler(interceptor.Funcs{}, &pendingPod)
		resources := &managedResources{
			instances: corev1.PodList{Items: []corev1.Pod{pendingPod}},
		}

		Expect(collectNamesOfUnrecoverableInstances(ctx, cluster, resources)).
			To(ConsistOf("cluster-example-2"))

		result, err := r.reconcileUnrecoverableInstances(ctx, cluster, resources)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(5 * time.Second))

		// The Pending pod has been deleted
		var fetched corev1.Pod
		err = r.Get(ctx, client.ObjectKeyFromObject(&pendingPod), &fetched)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		// An observability event has been recorded
		Expect(recorder.Events).To(Receive(ContainSubstring("DeleteUnrecoverableInstance")))
	})

	It("Force-removes an annotated Terminating instance past its deadline", func(ctx SpecContext) {
		const podName = "cluster-example-2"
		stuckPod := unrecoverablePod(podName, corev1.PodRunning, &metav1.Time{Time: time.Now().Add(-time.Minute)})
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-example", Namespace: "default"},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "cluster-example-1",
				TargetPrimary:  "cluster-example-1",
			},
		}

		forceDeleteCount := 0
		r, _ := newUnrecoverableReconciler(interceptor.Funcs{
			Delete: func(
				ctx context.Context,
				c client.WithWatch,
				obj client.Object,
				opts ...client.DeleteOption,
			) error {
				if pod, ok := obj.(*corev1.Pod); ok && pod.Name == podName {
					deleteOpts := client.DeleteOptions{}
					deleteOpts.ApplyOptions(opts)
					if deleteOpts.GracePeriodSeconds != nil && *deleteOpts.GracePeriodSeconds == 0 {
						forceDeleteCount++
					}
				}
				return c.Delete(ctx, obj, opts...)
			},
		})
		resources := &managedResources{
			instances: corev1.PodList{Items: []corev1.Pod{stuckPod}},
		}

		result, err := r.reconcileUnrecoverableInstances(ctx, cluster, resources)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(5 * time.Second))

		// Exactly one grace-period-zero delete has been issued for the stuck pod
		Expect(forceDeleteCount).To(Equal(1))
	})

	It("Does not force-remove a Terminating instance still within its deadline", func(ctx SpecContext) {
		const podName = "cluster-example-2"
		terminatingPod := unrecoverablePod(podName, corev1.PodRunning, &metav1.Time{Time: time.Now().Add(time.Hour)})
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-example", Namespace: "default"},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "cluster-example-1",
				TargetPrimary:  "cluster-example-1",
			},
		}

		forceDeleteCount := 0
		podDeleteCount := 0
		r, _ := newUnrecoverableReconciler(interceptor.Funcs{
			Delete: func(
				ctx context.Context,
				c client.WithWatch,
				obj client.Object,
				opts ...client.DeleteOption,
			) error {
				if pod, ok := obj.(*corev1.Pod); ok && pod.Name == podName {
					podDeleteCount++
					deleteOpts := client.DeleteOptions{}
					deleteOpts.ApplyOptions(opts)
					if deleteOpts.GracePeriodSeconds != nil && *deleteOpts.GracePeriodSeconds == 0 {
						forceDeleteCount++
					}
				}
				return c.Delete(ctx, obj, opts...)
			},
		})
		resources := &managedResources{
			instances: corev1.PodList{Items: []corev1.Pod{terminatingPod}},
		}

		result, err := r.reconcileUnrecoverableInstances(ctx, cluster, resources)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(5 * time.Second))

		// The pod is still handled (a graceful delete is issued) but never force-removed
		Expect(podDeleteCount).To(BeNumerically(">", 0))
		Expect(forceDeleteCount).To(Equal(0))
	})

	It("Deletes an annotated Pending instance even when not all instances are active", func(ctx SpecContext) {
		// This pins the gate-ordering fix: with a single Pending instance
		// allInstancesAreActive() is false, which used to make reconcileResources
		// return before the unrecoverable handling could run.
		pendingPod := unrecoverablePod("cluster-example-2", corev1.PodPending, nil)
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-example", Namespace: "default"},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:17.2",
				Instances: 1,
			},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "cluster-example-1",
				TargetPrimary:  "cluster-example-1",
			},
		}

		r, _ := newUnrecoverableReconciler(interceptor.Funcs{}, cluster, &pendingPod)
		resources := &managedResources{
			instances: corev1.PodList{Items: []corev1.Pod{pendingPod}},
		}

		// Precondition of the bug: the gate would otherwise short-circuit here.
		// The cluster is seeded so the pre-fix gate path (RegisterPhase) would run
		// cleanly and this test would fail on the substantive assertions below.
		Expect(resources.allInstancesAreActive()).To(BeFalse())

		result, err := r.reconcileResources(ctx, cluster, resources, postgres.PostgresqlStatusList{})
		Expect(err).ToNot(HaveOccurred())

		// The unrecoverable handling ran (5s requeue) rather than the active-instances
		// gate (1s requeue), and the Pending pod has been deleted.
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Second}))
		Expect(cluster.Status.Phase).ToNot(Equal(apiv1.PhaseWaitingForInstancesToBeActive))

		var fetched corev1.Pod
		err = r.Get(ctx, client.ObjectKeyFromObject(&pendingPod), &fetched)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	DescribeTable(
		"isPodStuckTerminating",
		func(deletionTime *metav1.Time, expected bool) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: deletionTime},
			}
			Expect(isPodStuckTerminating(pod)).To(Equal(expected))
		},
		Entry("no deletion timestamp", nil, false),
		Entry("deletion deadline in the past", &metav1.Time{Time: time.Now().Add(-time.Minute)}, true),
		Entry("deletion deadline in the future", &metav1.Time{Time: time.Now().Add(time.Hour)}, false),
	)
})
