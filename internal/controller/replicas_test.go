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
	"errors"
	"fmt"
	"time"

	cnpgTypes "github.com/cloudnative-pg/machinery/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Sacrificial Pod detection", func() {
	car1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "car-1",
			Namespace: "default",
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: "1",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	car2 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "car-2",
			Namespace: "default",
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: "2",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	foo := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: "3",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	bar := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bar",
			Namespace: "default",
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: "4",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	// unreachable mimics a Pod whose node has stopped reporting to the API
	// server: the node lifecycle controller flips PodReady to False, but the
	// stale ContainersReady left by the kubelet remains True.
	unreachable := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unreachable",
			Namespace: "default",
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: "5",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	It("detects if the list of Pods is empty", func() {
		var podList []corev1.Pod
		Expect(findDeletableInstance(&apiv1.Cluster{}, podList)).To(BeEmpty())
	})

	It("detects if we have not a ready Pod", func() {
		podList := []corev1.Pod{foo, bar}
		Expect(findDeletableInstance(&apiv1.Cluster{}, podList)).To(BeEmpty())
	})

	It("detects it if is the first available", func() {
		podList := []corev1.Pod{foo, bar, car1, car2}
		resultName := findDeletableInstance(&apiv1.Cluster{}, podList)
		Expect(resultName).ToNot(BeEmpty())
		Expect(resultName).To(Equal("car-2"))
	})

	It("detects it if is not the first one", func() {
		podList := []corev1.Pod{car2, foo, bar, car1}
		resultName := findDeletableInstance(&apiv1.Cluster{}, podList)
		Expect(resultName).ToNot(BeEmpty())
		Expect(resultName).To(Equal("car-2"))
	})

	It("skips a Pod whose node is unreachable even if it has the highest serial", func() {
		podList := []corev1.Pod{car1, car2, unreachable}
		resultName := findDeletableInstance(&apiv1.Cluster{}, podList)
		Expect(resultName).To(Equal("car-2"))
	})
})

var _ = Describe("markOldPrimaryAsUnhealthy", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	makePod := func(name, namespace, role string) corev1.Pod {
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{},
			},
		}
		if role != "" {
			utils.SetInstanceRole(&pod.ObjectMeta, role)
		}
		return pod
	}

	It("changes the primary label from the old primary pod", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)

		primary := makePod("cluster-1", namespace, specs.ClusterRoleLabelPrimary)
		replica1 := makePod("cluster-2", namespace, specs.ClusterRoleLabelReplica)
		replica2 := makePod("cluster-3", namespace, specs.ClusterRoleLabelReplica)

		for i, pod := range []corev1.Pod{primary, replica1, replica2} {
			p := pod
			Expect(env.client.Create(ctx, &p)).To(Succeed())
			// refresh the local copy with server-assigned fields
			if i == 0 {
				primary = p
			}
		}

		pods := []corev1.Pod{primary, replica1, replica2}

		err := env.clusterReconciler.markOldPrimaryAsUnhealthy(ctx, "cluster-1", pods)
		Expect(err).ToNot(HaveOccurred())

		// Verify the old primary's label was changed to unhealthy on the API server
		var updated corev1.Pod
		Expect(env.client.Get(ctx, client.ObjectKeyFromObject(&primary), &updated)).To(Succeed())
		Expect(updated.Labels[utils.ClusterInstanceRoleLabelName]).To(Equal(specs.ClusterRoleLabelUnhealthy))
		//nolint:staticcheck
		Expect(updated.Labels[utils.ClusterRoleLabelName]).To(Equal(specs.ClusterRoleLabelUnhealthy))

		// Verify replica pods are unchanged
		var replica1Updated corev1.Pod
		Expect(env.client.Get(ctx, client.ObjectKeyFromObject(&replica1), &replica1Updated)).To(Succeed())
		Expect(replica1Updated.Labels[utils.ClusterInstanceRoleLabelName]).To(Equal(specs.ClusterRoleLabelReplica))
	})

	It("does not error when the old primary is not in the pod list", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)

		replica := makePod("cluster-2", namespace, specs.ClusterRoleLabelReplica)
		Expect(env.client.Create(ctx, &replica)).To(Succeed())

		err := env.clusterReconciler.markOldPrimaryAsUnhealthy(ctx, "cluster-1", []corev1.Pod{replica})
		Expect(err).ToNot(HaveOccurred())
	})

	It("is a no-op when the old primary already has the unhealthy label", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)

		pod := makePod("cluster-1", namespace, specs.ClusterRoleLabelUnhealthy)
		Expect(env.client.Create(ctx, &pod)).To(Succeed())

		err := env.clusterReconciler.markOldPrimaryAsUnhealthy(ctx, "cluster-1", []corev1.Pod{pod})
		Expect(err).ToNot(HaveOccurred())

		var updated corev1.Pod
		Expect(env.client.Get(ctx, client.ObjectKeyFromObject(&pod), &updated)).To(Succeed())
		Expect(updated.Labels[utils.ClusterInstanceRoleLabelName]).To(Equal(specs.ClusterRoleLabelUnhealthy))
	})

	It("surfaces the Patch error so callers can apply their best-effort or retry strategy", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)

		primary := makePod("cluster-1", namespace, specs.ClusterRoleLabelPrimary)

		failingClient := fake.NewClientBuilder().
			WithScheme(env.scheme).
			WithObjects(&primary).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, obj client.Object,
					_ client.Patch, _ ...client.PatchOption,
				) error {
					Expect(obj).To(BeAssignableToTypeOf(&corev1.Pod{}))
					Expect(obj.GetName()).To(Equal("cluster-1"))
					Expect(obj.GetNamespace()).To(Equal(namespace))
					return fmt.Errorf("simulated API server error")
				},
			}).
			Build()

		r := &ClusterReconciler{Client: failingClient, Scheme: env.scheme}

		err := r.markOldPrimaryAsUnhealthy(ctx, "cluster-1", []corev1.Pod{primary})
		Expect(err).To(MatchError(ContainSubstring("simulated API server error")))
	})
})

var _ = Describe("Check pods not on primary node", func() {
	item1 := postgres.PostgresqlStatus{
		IsPrimary: false,
		Node:      "node-1",
		Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
	}

	item2 := postgres.PostgresqlStatus{
		IsPrimary: false,
		Node:      "node-2",
		Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}},
	}
	statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{item1, item2}}

	It("if primary is nil", func() {
		Expect(GetPodsNotOnPrimaryNode(statusList, nil).Items).To(BeEmpty())
	})

	item1.IsPrimary = true
	statusList2 := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{item1, item2}}

	It("first status element is primary", func() {
		Expect(GetPodsNotOnPrimaryNode(statusList2, &statusList2.Items[0]).Items).ToNot(BeEmpty())
	})
})

var _ = Describe("maxReceivedLsnAmongUpReceivers", func() {
	makeItem := func(name, lsn string, active bool) postgres.PostgresqlStatus {
		return postgres.PostgresqlStatus{
			Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}},
			ReceivedLsn:         cnpgTypes.LSN(lsn),
			IsWalReceiverActive: active,
		}
	}

	It("reports not found when there are no up receivers", func() {
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeItem("replica-1", "0/300", false),
		}}
		_, found, unknown := maxReceivedLsnAmongUpReceivers(statusList, "primary")
		Expect(found).To(BeFalse())
		Expect(unknown).To(BeEmpty())
	})

	It("excludes the primary even if it is (spuriously) marked as an active receiver", func() {
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeItem("primary", "0/999", true),
			makeItem("replica-1", "0/100", true),
		}}
		lsn, found, unknown := maxReceivedLsnAmongUpReceivers(statusList, "primary")
		Expect(found).To(BeTrue())
		Expect(lsn).To(Equal(cnpgTypes.LSN("0/100")))
		Expect(unknown).To(BeEmpty())
	})

	It("picks the highest LSN among multiple up receivers, ignoring down ones", func() {
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeItem("replica-1", "0/100", true),
			makeItem("replica-2", "1/0", true),
			makeItem("replica-3", "F/FFFFFFFF", false),
		}}
		lsn, found, unknown := maxReceivedLsnAmongUpReceivers(statusList, "primary")
		Expect(found).To(BeTrue())
		Expect(lsn).To(Equal(cnpgTypes.LSN("1/0")))
		Expect(unknown).To(BeEmpty())
	})

	It("reports a standby that failed to report as unknown, not as a down receiver", func() {
		unreachable := makeItem("replica-2", "", false)
		unreachable.Error = errors.New("connection refused")
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeItem("replica-1", "0/100", true),
			unreachable,
		}}
		lsn, found, unknown := maxReceivedLsnAmongUpReceivers(statusList, "primary")
		Expect(found).To(BeTrue())
		Expect(lsn).To(Equal(cnpgTypes.LSN("0/100")))
		Expect(unknown).To(ConsistOf("replica-2"))
	})

	It("ignores the reported LSN of a standby that failed to report, however high", func() {
		// A failed probe can still carry populated fields: the status client
		// decodes into the same struct it reports the error on, and its
		// deferred body close can set that error after a successful decode
		// (see rawInstanceStatusRequest). Whatever those fields say, an
		// instance the operator could not probe cleanly must not raise the
		// watermark, the same way every other consumer treats a non-nil Error
		// as "do not trust this item".
		unreachable := makeItem("replica-2", "F/FFFFFFFF", true)
		unreachable.Error = errors.New("connection refused")
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeItem("replica-1", "0/100", true),
			unreachable,
		}}
		lsn, found, unknown := maxReceivedLsnAmongUpReceivers(statusList, "primary")
		Expect(found).To(BeTrue())
		Expect(lsn).To(Equal(cnpgTypes.LSN("0/100")))
		Expect(unknown).To(ConsistOf("replica-2"))
	})

	It("reports not found when every standby failed to report", func() {
		// No observation to age: the gate must keep waiting rather than bypass
		// on an absence of data.
		first := makeItem("replica-1", "0/100", true)
		first.Error = errors.New("connection refused")
		second := makeItem("replica-2", "0/200", true)
		second.Error = errors.New("timeout")
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{first, second}}
		_, found, unknown := maxReceivedLsnAmongUpReceivers(statusList, "primary")
		Expect(found).To(BeFalse())
		Expect(unknown).To(ConsistOf("replica-1", "replica-2"))
	})
})

var _ = Describe("allUpReceiversExactlyCaughtUp", func() {
	makeItem := func(name, receivedLsn, latestEndLsn string, active bool) postgres.PostgresqlStatus {
		return postgres.PostgresqlStatus{
			Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}},
			ReceivedLsn:         cnpgTypes.LSN(receivedLsn),
			LatestEndLsn:        cnpgTypes.LSN(latestEndLsn),
			IsWalReceiverActive: active,
		}
	}

	It("reports caught up when every up receiver has flushed everything its sender reported", func() {
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeItem("primary", "0/999", "0/999", true),
			makeItem("replica-1", "0/300", "0/300", true),
			makeItem("replica-2", "0/300", "0/300", true),
		}}
		Expect(allUpReceiversExactlyCaughtUp(statusList, "primary")).To(BeTrue())
	})

	It("does not fire when a standby's LatestEndLsn is empty (unavailable, e.g. older instance manager)", func() {
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeItem("replica-1", "0/300", "", true),
		}}
		Expect(allUpReceiversExactlyCaughtUp(statusList, "primary")).To(BeFalse())
	})

	It("does not fire when a standby reported an error (unknown, not caught up)", func() {
		unreachable := makeItem("replica-2", "0/300", "0/300", true)
		unreachable.Error = errors.New("connection refused")
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeItem("replica-1", "0/300", "0/300", true),
			unreachable,
		}}
		Expect(allUpReceiversExactlyCaughtUp(statusList, "primary")).To(BeFalse())
	})

	It("does not fire when a standby still has bytes in flight (LatestEndLsn ahead of ReceivedLsn)", func() {
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeItem("replica-1", "0/300", "0/300", true),
			makeItem("replica-2", "0/100", "0/300", true),
		}}
		Expect(allUpReceiversExactlyCaughtUp(statusList, "primary")).To(BeFalse())
	})

	It("does not fire when no non-primary receiver is up", func() {
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeItem("replica-1", "0/300", "0/300", false),
		}}
		Expect(allUpReceiversExactlyCaughtUp(statusList, "primary")).To(BeFalse())
	})
})

var _ = Describe("evaluateWalReceiversGate", func() {
	var env *testingEnvironment
	var namespace string
	var cluster *apiv1.Cluster

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace = newFakeNamespace(env.client)
		cluster = newFakeCNPGCluster(env.client, namespace)
	})

	makeStatusItem := func(name, lsn string, walReceiverActive bool, statusErr error) postgres.PostgresqlStatus {
		return postgres.PostgresqlStatus{
			Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
			ReceivedLsn:         cnpgTypes.LSN(lsn),
			IsWalReceiverActive: walReceiverActive,
			Error:               statusErr,
		}
	}

	pastTimestamp := func(age time.Duration) string {
		return time.Now().Add(-age).Format(metav1.RFC3339Micro)
	}

	It("proceeds immediately when every up receiver has flushed everything its sender reported, "+
		"without touching the watermark", func(ctx SpecContext) {
		cluster.Status.CurrentPrimary = "primary"
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			{
				Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "replica-1", Namespace: namespace}},
				ReceivedLsn:         "0/300",
				LatestEndLsn:        "0/300",
				IsWalReceiverActive: true,
			},
			makeStatusItem("primary", "0/0", false, fmt.Errorf("connection refused")),
		}}

		err := env.clusterReconciler.evaluateWalReceiversGate(ctx, cluster, statusList)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Status.FailoverWalReceiversWatermarkLSN).To(BeEmpty())
		Expect(cluster.Status.FailoverWalReceiversWatermarkTimestamp).To(BeEmpty())
	})

	It("falls through to the timer when a receiver's LatestEndLsn is empty even though it looks caught up",
		func(ctx SpecContext) {
			cluster.Status.CurrentPrimary = "primary"
			statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				makeStatusItem("replica-1", "0/300", true, nil),
				makeStatusItem("primary", "0/0", false, fmt.Errorf("connection refused")),
			}}

			err := env.clusterReconciler.evaluateWalReceiversGate(ctx, cluster, statusList)
			Expect(err).To(MatchError(ErrWalReceiversRunning))
			Expect(cluster.Status.FailoverWalReceiversWatermarkLSN).To(Equal("0/300"))
		})

	It("keeps waiting and raises the watermark when progress is observed", func(ctx SpecContext) {
		cluster.Status.CurrentPrimary = "primary"
		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeStatusItem("replica-1", "0/300", true, nil),
			makeStatusItem("replica-2", "0/100", true, nil),
			makeStatusItem("primary", "0/0", false, fmt.Errorf("connection refused")),
		}}

		err := env.clusterReconciler.evaluateWalReceiversGate(ctx, cluster, statusList)
		Expect(err).To(MatchError(ErrWalReceiversRunning))
		Expect(cluster.Status.FailoverWalReceiversWatermarkLSN).To(Equal("0/300"))
		Expect(cluster.Status.FailoverWalReceiversWatermarkTimestamp).ToNot(BeEmpty())
	})

	It("keeps waiting when there is no progress but the grace period has not elapsed", func(ctx SpecContext) {
		cluster.Status.CurrentPrimary = "primary"
		cluster.Status.FailoverWalReceiversWatermarkLSN = "0/300"
		cluster.Status.FailoverWalReceiversWatermarkTimestamp = pastTimestamp(10 * time.Second)
		Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())

		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeStatusItem("replica-1", "0/300", true, nil),
			makeStatusItem("primary", "0/0", false, fmt.Errorf("connection refused")),
		}}

		err := env.clusterReconciler.evaluateWalReceiversGate(ctx, cluster, statusList)
		Expect(err).To(MatchError(ErrWalReceiversRunning))
		Expect(cluster.Status.FailoverWalReceiversWatermarkLSN).To(Equal("0/300"))
	})

	It("bypasses the wait once stalled past the grace period and the primary is unreachable, "+
		"without clearing the watermark yet", func(ctx SpecContext) {
		cluster.Status.CurrentPrimary = "primary"
		cluster.Status.FailoverWalReceiversWatermarkLSN = "0/300"
		cluster.Status.FailoverWalReceiversWatermarkTimestamp = pastTimestamp(walReceiversStalledGracePeriod + 5*time.Second)
		Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())

		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeStatusItem("replica-1", "0/300", true, nil),
			makeStatusItem("primary", "0/0", false, fmt.Errorf("connection refused")),
		}}

		err := env.clusterReconciler.evaluateWalReceiversGate(ctx, cluster, statusList)
		Expect(err).ToNot(HaveOccurred())
		// The watermark is left in place on purpose: the election this
		// bypass unblocks hasn't committed yet, and clearing it here would
		// make a failed commit restart the full grace period on the next
		// pass. The caller clears it once setPrimaryInstance succeeds.
		Expect(cluster.Status.FailoverWalReceiversWatermarkLSN).To(Equal("0/300"))
		Expect(cluster.Status.FailoverWalReceiversWatermarkTimestamp).ToNot(BeEmpty())
	})

	It("never bypasses when the old primary is still reporting, no matter how stale the watermark is",
		func(ctx SpecContext) {
			cluster.Status.CurrentPrimary = "primary"
			cluster.Status.FailoverWalReceiversWatermarkLSN = "0/300"
			cluster.Status.FailoverWalReceiversWatermarkTimestamp = pastTimestamp(walReceiversStalledGracePeriod + 5*time.Second)
			Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())

			statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				makeStatusItem("replica-1", "0/300", true, nil),
				// the old primary is alive enough to answer /pg/status, even though
				// it is not one of the (non-primary) WAL receivers being evaluated
				makeStatusItem("primary", "0/400", false, nil),
			}}

			err := env.clusterReconciler.evaluateWalReceiversGate(ctx, cluster, statusList)
			Expect(err).To(MatchError(ErrWalReceiversRunning))
			// the watermark is left untouched: no progress was observed (so it isn't
			// raised) and the gate never bypassed (so it isn't cleared either)
			Expect(cluster.Status.FailoverWalReceiversWatermarkLSN).To(Equal("0/300"))
		})

	It("clears a stale watermark once the receivers are genuinely down", func(ctx SpecContext) {
		cluster.Status.CurrentPrimary = "primary"
		cluster.Status.FailoverWalReceiversWatermarkLSN = "0/300"
		cluster.Status.FailoverWalReceiversWatermarkTimestamp = pastTimestamp(5 * time.Second)
		Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())

		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeStatusItem("replica-1", "0/300", false, nil),
			makeStatusItem("primary", "0/0", false, fmt.Errorf("connection refused")),
		}}

		err := env.clusterReconciler.evaluateWalReceiversGate(ctx, cluster, statusList)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Status.FailoverWalReceiversWatermarkLSN).To(BeEmpty())
		Expect(cluster.Status.FailoverWalReceiversWatermarkTimestamp).To(BeEmpty())
	})

	It("keeps the watermark set if the election fails to commit after the gate bypasses", func(ctx SpecContext) {
		cluster.Status.CurrentPrimary = "primary"
		cluster.Status.FailoverWalReceiversWatermarkLSN = "0/300"
		cluster.Status.FailoverWalReceiversWatermarkTimestamp = pastTimestamp(walReceiversStalledGracePeriod + 5*time.Second)
		Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())

		statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeStatusItem("replica-1", "0/300", true, nil),
			makeStatusItem("primary", "0/0", false, fmt.Errorf("connection refused")),
		}}

		err := env.clusterReconciler.evaluateWalReceiversGate(ctx, cluster, statusList)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Status.FailoverWalReceiversWatermarkLSN).To(Equal("0/300"))

		// Simulate the status patch that commits the election failing, the
		// same way a transient API server error would.
		failingClient := fake.NewClientBuilder().
			WithScheme(env.scheme).
			WithStatusSubresource(&apiv1.Cluster{}).
			WithObjects(cluster).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(_ context.Context, _ client.Client, subResourceName string, _ client.Object,
					_ client.Patch, _ ...client.SubResourcePatchOption,
				) error {
					if subResourceName == "status" {
						return fmt.Errorf("simulated API server error")
					}
					return nil
				},
			}).
			Build()
		r := &ClusterReconciler{Client: failingClient, Scheme: env.scheme}

		err = r.setPrimaryInstance(ctx, cluster, "replica-1")
		Expect(err).To(MatchError(ContainSubstring("simulated API server error")))

		// The election never committed. A real reconciler retries from here
		// on the next pass without ever calling clearWalReceiversWatermark,
		// so the watermark set before the (failed) attempt must survive.
		Expect(cluster.Status.FailoverWalReceiversWatermarkLSN).To(Equal("0/300"))
	})
})
