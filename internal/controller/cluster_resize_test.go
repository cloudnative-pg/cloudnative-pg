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
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("In-place resize of an instance pod", func() {
	var cluster *apiv1.Cluster
	var pod *corev1.Pod
	var recorder *record.FakeRecorder

	BeforeEach(func(ctx SpecContext) {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-resize",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
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
				Image: "postgres:13.11",
			},
		}

		// Build the pod from a deep copy: NewInstance shares the resource
		// maps of the passed cluster, and mutating them afterwards would
		// silently change the "live" pod too
		var err error
		pod, err = specs.NewInstance(ctx, *cluster.DeepCopy(), 1, true)
		Expect(err).ToNot(HaveOccurred())

		recorder = record.NewFakeRecorder(120)
	})

	newReconciler := func(funcs interceptor.Funcs) *ClusterReconciler {
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(pod).
			WithInterceptorFuncs(funcs).
			Build()

		return &ClusterReconciler{
			Client:   fakeClient,
			Recorder: recorder,
		}
	}

	getPostgresResources := func(spec *corev1.PodSpec) corev1.ResourceRequirements {
		for i := range spec.Containers {
			if spec.Containers[i].Name == specs.PostgresContainerName {
				return spec.Containers[i].Resources
			}
		}
		return corev1.ResourceRequirements{}
	}

	It("patches the resize subresource and refreshes the annotation", func(ctx SpecContext) {
		r := newReconciler(interceptor.Funcs{})

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		Expect(r.resizeInstanceInPlace(ctx, cluster, pod, "unit test")).To(Succeed())

		var currentPod corev1.Pod
		Expect(r.Get(ctx, k8client.ObjectKeyFromObject(pod), &currentPod)).To(Succeed())

		liveResources := getPostgresResources(&currentPod.Spec)
		Expect(liveResources.Limits["cpu"]).To(Equal(resource.MustParse("2")))

		var storedPodSpec corev1.PodSpec
		Expect(json.Unmarshal(
			[]byte(currentPod.Annotations[utils.PodSpecAnnotationName]), &storedPodSpec)).To(Succeed())
		storedResources := getPostgresResources(&storedPodSpec)
		Expect(storedResources.Limits["cpu"]).To(Equal(resource.MustParse("2")))

		Expect(recorder.Events).To(Receive(ContainSubstring("InPlaceResize")))
	})

	It("only repairs the annotation when the live resources already match", func(ctx SpecContext) {
		// Make the stored annotation stale while the live spec matches the target
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

		resizeCalled := false
		r := newReconciler(interceptor.Funcs{
			SubResourcePatch: func(
				ctx context.Context,
				cl k8client.Client,
				subResourceName string,
				obj k8client.Object,
				patch k8client.Patch,
				opts ...k8client.SubResourcePatchOption,
			) error {
				resizeCalled = true
				return cl.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
			},
		})

		Expect(r.resizeInstanceInPlace(ctx, cluster, pod, "unit test")).To(Succeed())
		Expect(resizeCalled).To(BeFalse())

		var currentPod corev1.Pod
		Expect(r.Get(ctx, k8client.ObjectKeyFromObject(pod), &currentPod)).To(Succeed())
		var refreshedPodSpec corev1.PodSpec
		Expect(json.Unmarshal(
			[]byte(currentPod.Annotations[utils.PodSpecAnnotationName]), &refreshedPodSpec)).To(Succeed())
		refreshedResources := getPostgresResources(&refreshedPodSpec)
		Expect(refreshedResources.Limits["cpu"]).To(Equal(resource.MustParse("1")))
	})

	It("reports a rejected resize so the caller can fall back", func(ctx SpecContext) {
		r := newReconciler(interceptor.Funcs{
			SubResourcePatch: func(
				_ context.Context,
				_ k8client.Client,
				_ string,
				_ k8client.Object,
				_ k8client.Patch,
				_ ...k8client.SubResourcePatchOption,
			) error {
				return apierrs.NewForbidden(
					schema.GroupResource{Resource: "pods"}, pod.Name,
					errors.New("the resize would change the pod QoS class"))
			},
		})

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		err := r.resizeInstanceInPlace(ctx, cluster, pod, "unit test")
		Expect(errors.Is(err, errInPlaceResizeRejected)).To(BeTrue())
		Expect(recorder.Events).To(Receive(ContainSubstring("InPlaceResizeFailed")))
	})

	It("propagates transient errors without falling back", func(ctx SpecContext) {
		r := newReconciler(interceptor.Funcs{
			SubResourcePatch: func(
				_ context.Context,
				_ k8client.Client,
				_ string,
				_ k8client.Object,
				_ k8client.Patch,
				_ ...k8client.SubResourcePatchOption,
			) error {
				return apierrs.NewInternalError(errors.New("etcd timeout"))
			},
		})

		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		err := r.resizeInstanceInPlace(ctx, cluster, pod, "unit test")
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, errInPlaceResizeRejected)).To(BeFalse())
	})
})
