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
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	rolloutManager "github.com/cloudnative-pg/cloudnative-pg/internal/controller/rollout"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// countingPluginClient counts the evaluations of the instance pods, each
// one being a round trip to every plugin
type countingPluginClient struct {
	pluginClient.Client
	evaluations int
}

func (c *countingPluginClient) LifecycleHook(
	_ context.Context,
	_ plugin.OperationVerb,
	_ k8client.Object,
	object k8client.Object,
) (k8client.Object, error) {
	c.evaluations++
	return object, nil
}

var _ = Describe("In-place resize of the instance pods", func() {
	var (
		cluster          *apiv1.Cluster
		primary, replica *corev1.Pod
		podList          *postgres.PostgresqlStatusList
		recorder         *record.FakeRecorder
		rm               *rolloutManager.Manager
		plugins          *countingPluginClient
		r                *ClusterReconciler

		// the patches the API server gets, and how it answers
		resizes, podPatches    int
		resizeBody, patchBody  []byte
		resizeErr, podPatchErr error
	)

	BeforeEach(func(ctx SpecContext) {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-resize",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				Instances:               2,
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
				Image:          "postgres:13.11",
				CurrentPrimary: "cluster-resize-1",
				TargetPrimary:  "cluster-resize-1",
			},
		}

		// NewInstance shares the resource maps of the cluster it gets:
		// a deep copy keeps the pods as they are when the cluster changes
		var err error
		primary, err = specs.NewInstance(ctx, *cluster.DeepCopy(), 1)
		Expect(err).ToNot(HaveOccurred())
		replica, err = specs.NewInstance(ctx, *cluster.DeepCopy(), 2)
		Expect(err).ToNot(HaveOccurred())
		podList = &postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			{Pod: primary, IsPodReady: true, ExecutableHash: "test_hash"},
			{Pod: replica, IsPodReady: true, ExecutableHash: "test_hash"},
		}}

		resizes, podPatches = 0, 0
		resizeErr, podPatchErr = nil, nil
		recorder = record.NewFakeRecorder(120)
		rm = rolloutManager.New(time.Hour, 0)
		plugins = &countingPluginClient{}
	})

	// The reconciler is built by the specs, after they prepared the pods
	// as the API server has them
	JustBeforeEach(func(ctx SpecContext) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster, primary.DeepCopy(), replica.DeepCopy()).
			WithStatusSubresource(&apiv1.Cluster{}).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(
					ctx context.Context, cl k8client.WithWatch,
					obj k8client.Object, patch k8client.Patch, opts ...k8client.PatchOption,
				) error {
					podPatches++
					patchBody, _ = patch.Data(obj)
					if err := podPatchErr; err != nil {
						podPatchErr = nil
						return err
					}
					return cl.Patch(ctx, obj, patch, opts...)
				},
				SubResourcePatch: func(
					ctx context.Context, cl k8client.Client, subResourceName string,
					obj k8client.Object, patch k8client.Patch, opts ...k8client.SubResourcePatchOption,
				) error {
					if subResourceName == "resize" {
						resizes++
						resizeBody, _ = patch.Data(obj)
						if resizeErr != nil {
							return resizeErr
						}
					}
					return cl.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
				},
			}).
			Build()

		r = &ClusterReconciler{
			Client:         fakeClient,
			Recorder:       recorder,
			rolloutManager: rm,
		}
		// the operator works on the pods as the API server returned them
		for i := range podList.Items {
			Expect(r.Get(ctx, k8client.ObjectKeyFromObject(podList.Items[i].Pod), podList.Items[i].Pod)).To(Succeed())
		}
	})

	rollout := func(ctx context.Context) (bool, error) {
		return r.rolloutRequiredInstances(pluginClient.SetPluginClientInContext(ctx, plugins), cluster, podList)
	}

	// getPod reads a pod as the API server has it, and its postgres
	// container as the pod spec annotation records it
	getPod := func(ctx context.Context, name string) (*corev1.Pod, corev1.Container) {
		var pod corev1.Pod
		Expect(r.Get(ctx, k8client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &pod)).To(Succeed())
		var storedPodSpec corev1.PodSpec
		Expect(json.Unmarshal([]byte(pod.Annotations[utils.PodSpecAnnotationName]), &storedPodSpec)).To(Succeed())
		return &pod, storedPodSpec.Containers[0]
	}

	events := func() []string {
		var result []string
		for len(recorder.Events) > 0 {
			result = append(result, <-recorder.Events)
		}
		return result
	}

	DescribeTable("costs nothing when no resource changed",
		func(ctx SpecContext, strategy apiv1.ResourcesUpdateStrategy) {
			cluster.Spec.ResourcesUpdateStrategy = strategy
			// any API call panics
			r.Client = nil

			Expect(rollout(ctx)).To(BeFalse())
			Expect(plugins.evaluations).To(Equal(2), "one evaluation per pod")
			Expect(events()).To(BeEmpty())
		},
		Entry("with the recreate strategy", apiv1.ResourcesUpdateStrategyRecreate),
		Entry("with the inPlace strategy", apiv1.ResourcesUpdateStrategyInPlace),
	)

	It("resizes every instance once, with no rollout slot, switchover or user action", func(ctx SpecContext) {
		cluster.Spec.PrimaryUpdateStrategy = apiv1.PrimaryUpdateStrategySupervised
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")

		// the second pass finds nothing left to do
		for range 2 {
			Expect(rollout(ctx)).To(BeFalse())
			Expect(resizes).To(Equal(2))
			Expect(podPatches).To(Equal(2))
		}
		Expect(plugins.evaluations).To(Equal(4), "one evaluation per pod and pass")
		Expect(events()).To(ConsistOf(
			ContainSubstring("Requested the in-place resize of instance pod cluster-resize-1"),
			ContainSubstring("Requested the in-place resize of instance pod cluster-resize-2"),
		))

		for _, name := range []string{primary.Name, replica.Name} {
			pod, stored := getPod(ctx, name)
			Expect(pod.Spec.Containers[0].Resources.Limits["cpu"]).To(Equal(resource.MustParse("2")))
			Expect(stored.Resources.Limits["cpu"]).To(Equal(resource.MustParse("2")))
		}

		Expect(cluster.Status.TargetPrimary).To(Equal(primary.Name))
		Expect(cluster.Status.Phase).ToNot(Equal(apiv1.PhaseWaitingForUser))
		otherCluster := k8client.ObjectKey{Namespace: cluster.Namespace, Name: "other-cluster"}
		Expect(rm.CoordinateRollout(otherCluster, "other-cluster-1").RolloutAllowed).To(BeTrue(),
			"an in-place resize must not take the rollout slot")
	})

	It("sends only the entries that changed", func(ctx SpecContext) {
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		podList.Items = podList.Items[:1]

		Expect(rollout(ctx)).To(BeFalse())
		Expect(resizeBody).To(MatchJSON(`{
			"metadata": {"resourceVersion": "999"},
			"spec": {
				"$setElementOrder/containers": [{"name": "postgres"}],
				"containers": [{"name": "postgres", "resources": {"limits": {"cpu": "2"}}}]
			}
		}`))
		// the annotation goes on the pod the resize returned: no lock
		Expect(string(patchBody)).ToNot(ContainSubstring(`"resourceVersion"`))
	})

	It("ends the pass quietly when its read of the pod is stale, and converges on a fresh one", func(ctx SpecContext) {
		stale := replica.DeepCopy()
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		Expect(rollout(ctx)).To(BeFalse())
		Expect(events()).To(HaveLen(2))

		// the cluster changes again before the cache has the resized pod:
		// against the stale pod only the memory request looks different
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("1")
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1500Mi")
		fresh := podList.Items[1].Pod
		podList.Items[1].Pod = stale
		_, err := rollout(ctx)
		Expect(err).To(MatchError(ErrNextLoop))
		Expect(events()).To(BeEmpty())

		podList.Items[1].Pod = fresh
		Expect(rollout(ctx)).To(BeFalse())
		pod, stored := getPod(ctx, replica.Name)
		Expect(pod.Spec.Containers[0].Resources).To(BeComparableTo(cluster.Spec.Resources))
		Expect(stored.Resources).To(BeComparableTo(cluster.Spec.Resources))
	})

	DescribeTable("does not try to resize",
		func(ctx SpecContext, prepare func()) {
			prepare()
			cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
			podList.Items = podList.Items[:1]

			Expect(rollout(ctx)).To(BeFalse())
			Expect(resizes).To(BeZero())
		},
		Entry("when the reconciliation of the pod spec is disabled", func() {
			cluster.Annotations = map[string]string{utils.ReconcilePodSpecAnnotationName: "disabled"}
		}),
		Entry("when the primary is about to be restarted by the instance manager", func() {
			podList.Items[0].PendingRestartForDecrease = true
		}),
	)

	DescribeTable("recreates the pod in the same pass when the API server rejects the resize",
		func(ctx SpecContext, rejection error) {
			resizeErr = rejection
			cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")

			Expect(rollout(ctx)).To(BeTrue())
			Expect(resizes).To(Equal(1))
			Expect(podPatches).To(BeZero())
			Expect(events()).To(ConsistOf(
				ContainSubstring("Warning InPlaceResizeFailed"),
				ContainSubstring("Upgrading instance cluster-resize-2"),
			))
			Expect(apierrs.IsNotFound(r.Get(ctx, k8client.ObjectKeyFromObject(replica), &corev1.Pod{}))).To(BeTrue())
		},
		Entry("API server without the resize subresource",
			apierrs.NewNotFound(schema.GroupResource{Resource: "pods"}, "cluster-resize-2")),
		Entry("node without support for resize",
			apierrs.NewForbidden(schema.GroupResource{Resource: "pods"}, "cluster-resize-2",
				errors.New("Pod running on node without support for resize"))),
		Entry("invalid resize",
			apierrs.NewInvalid(schema.GroupKind{Kind: "Pod"}, "cluster-resize-2", nil)),
		Entry("malformed resize",
			apierrs.NewBadRequest("malformed patch")),
	)

	It("applies the rules of the primary when the API server rejects its resize", func(ctx SpecContext) {
		resizeErr = apierrs.NewInvalid(schema.GroupKind{Kind: "Pod"}, primary.Name, nil)
		cluster.Spec.PrimaryUpdateStrategy = apiv1.PrimaryUpdateStrategySupervised
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		podList.Items = podList.Items[:1]

		Expect(rollout(ctx)).To(BeTrue())
		Expect(resizes).To(Equal(1))
		Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseWaitingForUser))
		Expect(r.Get(ctx, k8client.ObjectKeyFromObject(primary), &corev1.Pod{})).To(Succeed())
	})

	DescribeTable("returns an unexpected resize error, recreating nothing",
		func(ctx SpecContext, unexpected error) {
			resizeErr = unexpected
			cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")

			_, err := rollout(ctx)
			Expect(err).To(MatchError(resizeErr))
			Expect(events()).To(BeEmpty())
			Expect(r.Get(ctx, k8client.ObjectKeyFromObject(replica), &corev1.Pod{})).To(Succeed())
		},
		Entry("server error", apierrs.NewInternalError(errors.New("etcd timeout"))),
		// no pod event follows it: ending the pass quietly would leave the pod as it is
		Entry("conflict on something else than the pod",
			apierrs.NewConflict(schema.GroupResource{Resource: "resourcequotas"}, "quota", errors.New("modified"))),
	)

	It("retries a failed refresh of the annotation without resizing again", func(ctx SpecContext) {
		podPatchErr = apierrs.NewInternalError(errors.New("etcd timeout"))
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")

		_, err := rollout(ctx)
		Expect(err).To(MatchError(ContainSubstring("etcd timeout")))
		Expect(resizes).To(Equal(1))

		// the next reconciliation reads the pod that was resized: with no
		// resize to send, the lock on the annotation catches a stale read
		resizedReplica, _ := getPod(ctx, replica.Name)
		podList.Items[1].Pod = resizedReplica.DeepCopy()
		podList.Items[1].Pod.ResourceVersion = "1"
		_, err = rollout(ctx)
		Expect(err).To(MatchError(ErrNextLoop))
		Expect(string(patchBody)).To(ContainSubstring(`"resourceVersion":"1"`))

		podList.Items[1].Pod = resizedReplica
		Expect(rollout(ctx)).To(BeFalse())
		Expect(resizes).To(Equal(2), "only the primary is resized in this pass")

		_, stored := getPod(ctx, replica.Name)
		Expect(stored.Resources.Limits["cpu"]).To(Equal(resource.MustParse("2")))
	})

	DescribeTable("recreates the pod without trying to resize it",
		func(ctx SpecContext, change func(), reason string) {
			change()

			Expect(rollout(ctx)).To(BeTrue())
			Expect(resizes).To(BeZero())
			Expect(cluster.Status.PhaseReason).To(HaveSuffix(reason))
			Expect(apierrs.IsNotFound(r.Get(ctx, k8client.ObjectKeyFromObject(replica), &corev1.Pod{}))).To(BeTrue())
		},
		Entry("with the recreate strategy", func() {
			cluster.Spec.ResourcesUpdateStrategy = apiv1.ResourcesUpdateStrategyRecreate
			cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		}, "container postgres differs in resources"),
		Entry("when the memory limit decreases", func() {
			cluster.Spec.Resources.Limits["memory"] = resource.MustParse("1Gi")
		}, "not resizable in place: container postgres: the memory limit cannot be decreased in place"),
		Entry("when more than the resources changed", func() {
			cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
			cluster.Spec.SchedulerName = "custom-scheduler"
		}, "container postgres differs in resources"),
	)
})
