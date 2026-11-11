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
	"encoding/json"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	rolloutManager "github.com/cloudnative-pg/cloudnative-pg/internal/controller/rollout"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("In-place resize of the instance pods", func() {
	var (
		cluster  *apiv1.Cluster
		pod      *corev1.Pod
		recorder *record.FakeRecorder
		rm       *rolloutManager.Manager

		// resizeCalls counts the requests to the resize subresource, and
		// resizeErr is what the API server answers to them
		resizeCalls int
		resizeErr   error
	)

	BeforeEach(func(ctx SpecContext) {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-resize",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				Instances:               1,
				ImageName:               "postgres:13.11",
				ResourcesUpdateStrategy: apiv1.ResourcesUpdateStrategyInPlace,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"cpu":    resource.MustParse("500m"),
						"memory": resource.MustParse("1Gi"),
					},
					Limits: corev1.ResourceList{
						"cpu":    resource.MustParse("1"),
						"memory": resource.MustParse("2Gi"),
					},
				},
			},
			Status: apiv1.ClusterStatus{
				Image:     "postgres:13.11",
				Instances: 1,
			},
		}

		// Build the pod from a deep copy: NewInstance shares the resource
		// maps of the passed cluster, and mutating them afterwards would
		// silently change the "live" pod too
		var err error
		pod, err = specs.NewInstance(ctx, *cluster.DeepCopy(), 1)
		Expect(err).ToNot(HaveOccurred())
		cluster.Status.CurrentPrimary = pod.Name
		cluster.Status.TargetPrimary = pod.Name

		recorder = record.NewFakeRecorder(120)
		resizeCalls = 0
		resizeErr = nil
	})

	newReconciler := func(otherPods ...k8client.Object) *ClusterReconciler {
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(append(otherPods, pod, cluster)...).
			WithStatusSubresource(&apiv1.Cluster{}).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(
					ctx context.Context,
					cl k8client.Client,
					subResourceName string,
					obj k8client.Object,
					patch k8client.Patch,
					opts ...k8client.SubResourcePatchOption,
				) error {
					if subResourceName != "resize" {
						return cl.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
					}
					resizeCalls++
					if resizeErr != nil {
						return resizeErr
					}
					return cl.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
				},
			}).
			Build()

		rm = rolloutManager.New(time.Hour, 0)
		return &ClusterReconciler{
			Client:         fakeClient,
			Recorder:       recorder,
			rolloutManager: rm,
		}
	}

	// rollingUpdate runs the production entry point: the in-place resize
	// followed by the rollout of the instances that still need it
	rollingUpdate := func(ctx context.Context, r *ClusterReconciler, pods ...*corev1.Pod) error {
		instances := postgres.PostgresqlStatusList{}
		for _, instance := range pods {
			instances.Items = append(instances.Items, postgres.PostgresqlStatus{
				Pod: instance, IsPodReady: true, ExecutableHash: "test_hash",
			})
		}
		_, err := r.handleRollingUpdate(ctx, cluster, instances)
		return err
	}

	getPostgresResources := func(spec *corev1.PodSpec) corev1.ResourceRequirements {
		for i := range spec.Containers {
			if spec.Containers[i].Name == specs.PostgresContainerName {
				return spec.Containers[i].Resources
			}
		}
		return corev1.ResourceRequirements{}
	}

	// getCurrentPod returns the pod as known by the API server, together
	// with the pod spec stored in its annotation
	getCurrentPod := func(ctx context.Context, r *ClusterReconciler, name string) (*corev1.Pod, *corev1.PodSpec) {
		GinkgoHelper()
		var currentPod corev1.Pod
		Expect(r.Get(ctx, k8client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &currentPod)).To(Succeed())
		var storedPodSpec corev1.PodSpec
		Expect(json.Unmarshal(
			[]byte(currentPod.Annotations[utils.PodSpecAnnotationName]), &storedPodSpec)).To(Succeed())
		return &currentPod, &storedPodSpec
	}

	// expectRecreated verifies that the instance was left to the rolling
	// update, which deleted the pod
	expectRecreated := func(ctx context.Context, r *ClusterReconciler, err error) {
		GinkgoHelper()
		Expect(err).To(MatchError(ErrNextLoop))
		err = r.Get(ctx, k8client.ObjectKeyFromObject(pod), &corev1.Pod{})
		Expect(apierrs.IsNotFound(err)).To(BeTrue(), "the pod must have been deleted by the rolling update")
	}

	// expectUntouched verifies that neither the live resources nor the
	// stored ones changed
	expectUntouched := func(ctx context.Context, r *ClusterReconciler) {
		GinkgoHelper()
		Expect(resizeCalls).To(BeZero())
		currentPod, storedPodSpec := getCurrentPod(ctx, r, pod.Name)
		Expect(getPostgresResources(&currentPod.Spec).Limits["cpu"]).To(Equal(resource.MustParse("1")))
		Expect(getPostgresResources(storedPodSpec).Limits["cpu"]).To(Equal(resource.MustParse("1")))
	}

	It("resizes the pod, records the new resources and skips the rollout", func(ctx SpecContext) {
		r := newReconciler()

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("4Gi")
		Expect(rollingUpdate(ctx, r, pod)).To(Succeed())

		currentPod, storedPodSpec := getCurrentPod(ctx, r, pod.Name)
		Expect(currentPod.UID).To(Equal(pod.UID))
		for _, resources := range []corev1.ResourceRequirements{
			getPostgresResources(&currentPod.Spec),
			getPostgresResources(storedPodSpec),
			// the pod the rollout looks at is the resized one
			getPostgresResources(&pod.Spec),
		} {
			Expect(resources.Limits["cpu"]).To(Equal(resource.MustParse("2")))
			Expect(resources.Limits["memory"]).To(Equal(resource.MustParse("4Gi")))
			Expect(resources.Requests["cpu"]).To(Equal(resource.MustParse("500m")))
		}
		Expect(resizeCalls).To(Equal(1))
		Expect(recorder.Events).To(Receive(ContainSubstring("InPlaceResize")))

		// nothing is left to do
		Expect(rollingUpdate(ctx, r, currentPod)).To(Succeed())
		Expect(resizeCalls).To(Equal(1))
	})

	It("resizes every instance with no rollout slot and no switchover", func(ctx SpecContext) {
		replica, err := specs.NewInstance(ctx, *cluster.DeepCopy(), 2)
		Expect(err).ToNot(HaveOccurred())
		cluster.Spec.Instances = 2
		cluster.Spec.PrimaryUpdateMethod = apiv1.PrimaryUpdateMethodSwitchover
		cluster.Status.Instances = 2
		r := newReconciler(replica)

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		Expect(rollingUpdate(ctx, r, pod, replica)).To(Succeed())
		Expect(cluster.Status.TargetPrimary).To(Equal(pod.Name))

		for _, instance := range []*corev1.Pod{pod, replica} {
			currentPod, _ := getCurrentPod(ctx, r, instance.Name)
			Expect(getPostgresResources(&currentPod.Spec).Limits["cpu"]).To(Equal(resource.MustParse("2")))
		}

		otherCluster := k8client.ObjectKey{Namespace: cluster.Namespace, Name: "other-cluster"}
		Expect(rm.CoordinateRollout(otherCluster, "other-cluster-1").RolloutAllowed).To(BeTrue(),
			"an in-place resize must not consume the rollout slot")
	})

	It("leaves alone the containers it does not resize", func(ctx SpecContext) {
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
			Name: "istio-proxy",
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{"cpu": resource.MustParse("100m")},
			},
		})
		// the cached copy of the pod predates a resize of the injected
		// container issued by another actor
		cachedPod := pod.DeepCopy()
		pod.Spec.Containers[len(pod.Spec.Containers)-1].Resources.Limits["cpu"] = resource.MustParse("300m")
		r := newReconciler()

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		Expect(rollingUpdate(ctx, r, cachedPod)).To(Succeed())

		currentPod, _ := getCurrentPod(ctx, r, pod.Name)
		Expect(getPostgresResources(&currentPod.Spec).Limits["cpu"]).To(Equal(resource.MustParse("2")))
		injected := currentPod.Spec.Containers[len(currentPod.Spec.Containers)-1]
		Expect(injected.Name).To(Equal("istio-proxy"))
		Expect(injected.Resources.Limits["cpu"]).To(Equal(resource.MustParse("300m")))
	})

	It("only repairs the annotation when the live resources already match", func(ctx SpecContext) {
		// The resize was applied, but recording it failed
		var storedPodSpec corev1.PodSpec
		Expect(json.Unmarshal(
			[]byte(pod.Annotations[utils.PodSpecAnnotationName]), &storedPodSpec)).To(Succeed())
		for i := range storedPodSpec.Containers {
			if storedPodSpec.Containers[i].Name == specs.PostgresContainerName {
				storedPodSpec.Containers[i].Resources.Limits["cpu"] = resource.MustParse("250m")
			}
		}
		staleAnnotation, err := json.Marshal(storedPodSpec)
		Expect(err).ToNot(HaveOccurred())
		pod.Annotations[utils.PodSpecAnnotationName] = string(staleAnnotation)
		r := newReconciler()

		Expect(rollingUpdate(ctx, r, pod)).To(Succeed())
		Expect(resizeCalls).To(BeZero())

		_, refreshedPodSpec := getCurrentPod(ctx, r, pod.Name)
		Expect(getPostgresResources(refreshedPodSpec).Limits["cpu"]).To(Equal(resource.MustParse("1")))
	})

	It("records the resources of the run-once init containers too", func(ctx SpecContext) {
		// a run-once init container, as injected by a plugin, whose
		// resources follow the ones of the cluster
		var storedPodSpec corev1.PodSpec
		Expect(json.Unmarshal(
			[]byte(pod.Annotations[utils.PodSpecAnnotationName]), &storedPodSpec)).To(Succeed())
		for i := range storedPodSpec.InitContainers {
			storedPodSpec.InitContainers[i].Resources.Limits["cpu"] = resource.MustParse("250m")
		}
		staleAnnotation, err := json.Marshal(storedPodSpec)
		Expect(err).ToNot(HaveOccurred())
		pod.Annotations[utils.PodSpecAnnotationName] = string(staleAnnotation)
		r := newReconciler()

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		Expect(rollingUpdate(ctx, r, pod)).To(Succeed())

		currentPod, refreshedPodSpec := getCurrentPod(ctx, r, pod.Name)
		Expect(refreshedPodSpec.InitContainers).ToNot(BeEmpty())
		for i := range refreshedPodSpec.InitContainers {
			Expect(refreshedPodSpec.InitContainers[i].Resources.Limits["cpu"]).To(Equal(resource.MustParse("2")))
			// the init containers have already terminated: they are not resized
			Expect(currentPod.Spec.InitContainers[i].Resources.Limits["cpu"]).To(Equal(resource.MustParse("1")))
		}
	})

	DescribeTable("recreates the pod when the API server rejects the resize",
		func(ctx SpecContext, rejection error) {
			resizeErr = rejection
			r := newReconciler()

			cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
			expectRecreated(ctx, r, rollingUpdate(ctx, r, pod))
			Expect(resizeCalls).To(Equal(1))
			Expect(recorder.Events).To(Receive(ContainSubstring("InPlaceResizeFailed")))
		},
		Entry("because the change is invalid, as when it alters the pod QoS class",
			apierrs.NewInvalid(schema.GroupKind{Kind: "Pod"}, "cluster-resize-1", nil)),
		Entry("because the request is malformed",
			apierrs.NewBadRequest("bad request")),
		Entry("because the operator is not allowed to resize pods",
			apierrs.NewForbidden(schema.GroupResource{Resource: "pods"}, "cluster-resize-1", errors.New("forbidden"))),
		Entry("because the API server does not expose the resize subresource",
			apierrs.NewNotFound(schema.GroupResource{Resource: "pods"}, "cluster-resize-1")),
	)

	It("propagates transient errors without recreating the pod", func(ctx SpecContext) {
		resizeErr = apierrs.NewInternalError(errors.New("etcd timeout"))
		r := newReconciler()

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		err := rollingUpdate(ctx, r, pod)
		Expect(apierrs.IsInternalError(err)).To(BeTrue())

		resizeErr = nil
		resizeCalls = 0
		expectUntouched(ctx, r)
	})

	It("recreates the pod when the memory limit decreases", func(ctx SpecContext) {
		r := newReconciler()

		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("1Gi")
		expectRecreated(ctx, r, rollingUpdate(ctx, r, pod))
		Expect(resizeCalls).To(BeZero())
	})

	It("recreates the pod when the resources are not the only change", func(ctx SpecContext) {
		r := newReconciler()

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		cluster.Spec.SchedulerName = "custom-scheduler"
		expectRecreated(ctx, r, rollingUpdate(ctx, r, pod))
		Expect(resizeCalls).To(BeZero())
	})

	It("recreates the pod when the strategy is recreate", func(ctx SpecContext) {
		cluster.Spec.ResourcesUpdateStrategy = apiv1.ResourcesUpdateStrategyRecreate
		r := newReconciler()

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		expectRecreated(ctx, r, rollingUpdate(ctx, r, pod))
		Expect(resizeCalls).To(BeZero())
	})

	It("leaves alone the live resources changed by other actors", func(ctx SpecContext) {
		// A resize issued outside the operator, or a mutating webhook at
		// creation: the pod spec changes, the annotation stays aligned with
		// the target
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Name == specs.PostgresContainerName {
				pod.Spec.Containers[i].Resources.Limits["cpu"] = resource.MustParse("4")
			}
		}
		r := newReconciler()

		Expect(rollingUpdate(ctx, r, pod)).To(Succeed())
		Expect(resizeCalls).To(BeZero())
		currentPod, _ := getCurrentPod(ctx, r, pod.Name)
		Expect(getPostgresResources(&currentPod.Spec).Limits["cpu"]).To(Equal(resource.MustParse("4")))
	})

	It("does not touch the instances when the pod spec reconciliation is disabled", func(ctx SpecContext) {
		cluster.Annotations = map[string]string{utils.ReconcilePodSpecAnnotationName: "disabled"}
		r := newReconciler()

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		Expect(rollingUpdate(ctx, r, pod)).To(Succeed())
		expectUntouched(ctx, r)
	})

	It("does not touch a fenced instance", func(ctx SpecContext) {
		cluster.Annotations = map[string]string{utils.FencedInstanceAnnotation: `["` + pod.Name + `"]`}
		r := newReconciler()

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		Expect(rollingUpdate(ctx, r, pod)).To(Succeed())
		expectUntouched(ctx, r)
	})

	It("does not touch a pod without the pod spec annotation", func(ctx SpecContext) {
		delete(pod.Annotations, utils.PodSpecAnnotationName)
		r := newReconciler()

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		Expect(r.resizeInstancesInPlace(ctx, cluster, &postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{{Pod: pod}},
		})).To(Succeed())
		Expect(resizeCalls).To(BeZero())
	})
})
