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
	"fmt"
	"time"

	cnpgTypes "github.com/cloudnative-pg/machinery/pkg/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeTimelineHistoryClient answers GetTimelineHistoryFromInstance with a
// fixed response or error, and counts how many times it was called (to
// assert the fork-point check isn't re-run needlessly).
type fakeTimelineHistoryClient struct {
	remote.InstanceClient
	response remote.TimelineHistoryResponse
	err      error
	calls    int
}

func (f *fakeTimelineHistoryClient) GetTimelineHistoryFromInstance(
	_ context.Context,
	_ *corev1.Pod,
) (remote.TimelineHistoryResponse, error) {
	f.calls++
	return f.response, f.err
}

var _ = Describe("evaluateReplicaDivergence", func() {
	var env *testingEnvironment
	var namespace string
	var cluster *apiv1.Cluster
	var fakeClient *fakeTimelineHistoryClient

	pastTimestamp := func(age time.Duration) string {
		return time.Now().Add(-age).Format(metav1.RFC3339Micro)
	}

	makeReplica := func(
		name string, uid types.UID, tli int, receivedLsn string, statusErr error,
	) postgres.PostgresqlStatus {
		return postgres.PostgresqlStatus{
			Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid}},
			TimeLineID:  tli,
			ReceivedLsn: cnpgTypes.LSN(receivedLsn),
			Error:       statusErr,
		}
	}

	makePrimary := func(name string, ready bool) postgres.PostgresqlStatus {
		return postgres.PostgresqlStatus{
			Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
			IsPrimary:  true,
			IsPodReady: ready,
		}
	}

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace = newFakeNamespace(env.client)
		cluster = newFakeCNPGCluster(env.client, namespace)
		cluster.Status.CurrentPrimary = "primary"
		cluster.Status.TargetPrimary = "primary"
		cluster.Status.TimelineID = 2
		fakeClient = &fakeTimelineHistoryClient{
			response: remote.TimelineHistoryResponse{
				TimelineID: 2,
				Content:    "1\t0/7000110\tno recovery target specified\n",
			},
		}
		env.clusterReconciler.InstanceClient = fakeClient
	})

	It("starts a watermark for a replica behind the current timeline, once the primary is Ready", func(ctx SpecContext) {
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makePrimary("primary", true),
			makeReplica("replica-1", "uid-1", 1, "0/7500000", nil),
		}}

		env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

		Expect(cluster.Status.ReplicaWalIssues).To(BeEmpty())
		Expect(fakeClient.calls).To(Equal(0))
		watermark, ok := cluster.Status.ReplicaDivergenceWatermarks["replica-1"]
		Expect(ok).To(BeTrue())
		Expect(watermark.TimeLineID).To(Equal(2))
		Expect(watermark.ReceivedLSN).To(Equal("0/7500000"))
		Expect(watermark.Since).ToNot(BeEmpty())
	})

	It("does not start the watermark before the current primary is reported Ready (A1)", func(ctx SpecContext) {
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makePrimary("primary", false), // not yet Ready
			makeReplica("replica-1", "uid-1", 1, "0/7500000", nil),
		}}

		env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

		Expect(cluster.Status.ReplicaDivergenceWatermarks).To(BeEmpty())
		Expect(cluster.Status.ReplicaWalIssues).To(BeEmpty())
	})

	It("never evaluates a primary-role instance, however it reports (A5)", func(ctx SpecContext) {
		// Pathological: an item reporting IsPrimary=true with a lower
		// timeline should never happen, but must still be skipped outright.
		rogue := makePrimary("primary", true)
		rogue.TimeLineID = 1
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{rogue}}

		env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

		Expect(cluster.Status.ReplicaDivergenceWatermarks).To(BeEmpty())
		Expect(cluster.Status.ReplicaWalIssues).To(BeEmpty())
		Expect(fakeClient.calls).To(Equal(0))
	})

	It("does nothing at all for a replica cluster (I1)", func(ctx SpecContext) {
		cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{Enabled: ptrBool(true)}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makePrimary("primary", true),
			makeReplica("replica-1", "uid-1", 1, "0/7500000", nil),
		}}

		env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

		Expect(cluster.Status.ReplicaDivergenceWatermarks).To(BeEmpty())
		Expect(cluster.Status.ReplicaWalIssues).To(BeEmpty())
		Expect(fakeClient.calls).To(Equal(0))
	})

	It("keeps waiting without confirming anything while stalled but inside the grace period", func(ctx SpecContext) {
		since := pastTimestamp(10 * time.Second)
		cluster.Status.ReplicaDivergenceWatermarks = map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark{
			"replica-1": {TimeLineID: 2, ReceivedLSN: "0/7500000", Since: since},
		}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makePrimary("primary", true),
			makeReplica("replica-1", "uid-1", 1, "0/7500000", nil),
		}}

		env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

		Expect(cluster.Status.ReplicaWalIssues).To(BeEmpty())
		Expect(fakeClient.calls).To(Equal(0))
		Expect(cluster.Status.ReplicaDivergenceWatermarks["replica-1"].Since).To(Equal(since))
	})

	It("restarts the watermark clock when the received LSN progresses", func(ctx SpecContext) {
		cluster.Status.ReplicaDivergenceWatermarks = map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark{
			"replica-1": {
				TimeLineID:  2,
				ReceivedLSN: "0/7500000",
				Since:       pastTimestamp(replicaDivergenceStallGracePeriod + 5*time.Second),
			},
		}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makePrimary("primary", true),
			makeReplica("replica-1", "uid-1", 1, "0/7600000", nil), // advanced
		}}

		env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

		Expect(cluster.Status.ReplicaWalIssues).To(BeEmpty())
		Expect(fakeClient.calls).To(Equal(0))
		watermark := cluster.Status.ReplicaDivergenceWatermarks["replica-1"]
		Expect(watermark.ReceivedLSN).To(Equal("0/7600000"))
	})

	It("confirms a divergence once stalled past the grace period and the received LSN is strictly past the fork point",
		func(ctx SpecContext) {
			cluster.Status.ReplicaDivergenceWatermarks = map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark{
				"replica-1": {
					TimeLineID:  2,
					ReceivedLSN: "0/7500000",
					Since:       pastTimestamp(replicaDivergenceStallGracePeriod + 5*time.Second),
				},
			}
			statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				makePrimary("primary", true),
				makeReplica("replica-1", "uid-1", 1, "0/7500000", nil),
			}}

			env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

			Expect(fakeClient.calls).To(Equal(1))
			issue, ok := cluster.Status.ReplicaWalIssues["replica-1"]
			Expect(ok).To(BeTrue())
			Expect(issue.Kind).To(Equal(apiv1.ReplicaWalIssueDiverged))
			Expect(issue.ForkLSN).To(Equal("0/7000110"))
			Expect(issue.ReceivedLSN).To(Equal("0/7500000"))
			Expect(issue.DetectedTimeLineID).To(Equal(1))
			Expect(issue.Parked).To(BeFalse())
			Expect(issue.PVCUID).To(BeEmpty(), "PVCUID is only recorded once containment actually parks the instance")

			cond := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionReplicasHealthy))
			Expect(cond).ToNot(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonReplicaWalIssues)))
		})

	It("classifies as Stuck (not Diverged) a replica whose received LSN exactly equals the fork point (B6, strict >)",
		func(ctx SpecContext) {
			cluster.Status.ReplicaDivergenceWatermarks = map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark{
				"replica-1": {
					TimeLineID:  2,
					ReceivedLSN: "0/7000110",
					Since:       pastTimestamp(replicaDivergenceStallGracePeriod + 5*time.Second),
				},
			}
			statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				makePrimary("primary", true),
				// received LSN == fork LSN exactly: not strictly past it
				makeReplica("replica-1", "uid-1", 1, "0/7000110", nil),
			}}

			env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

			issue, ok := cluster.Status.ReplicaWalIssues["replica-1"]
			Expect(ok).To(BeTrue())
			Expect(issue.Kind).To(Equal(apiv1.ReplicaWalIssueStuck))
			Expect(issue.ForkLSN).To(Equal("0/7000110"))
		})

	It("classifies as Stuck a replica whose received LSN is behind the fork point", func(ctx SpecContext) {
		cluster.Status.ReplicaDivergenceWatermarks = map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark{
			"replica-1": {
				TimeLineID:  2,
				ReceivedLSN: "0/6000000",
				Since:       pastTimestamp(replicaDivergenceStallGracePeriod + 5*time.Second),
			},
		}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makePrimary("primary", true),
			makeReplica("replica-1", "uid-1", 1, "0/6000000", nil),
		}}

		env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

		issue, ok := cluster.Status.ReplicaWalIssues["replica-1"]
		Expect(ok).To(BeTrue())
		Expect(issue.Kind).To(Equal(apiv1.ReplicaWalIssueStuck))
	})

	It("classifies as Stuck when the fork-point check is inconclusive (parent timeline not in history)",
		func(ctx SpecContext) {
			// The current primary is on timeline 3, having forked from
			// timeline 2 at 0/9000000; the served history only records that
			// one hop, same as a real PostgreSQL history file would -- it
			// says nothing about timeline 1.
			cluster.Status.TimelineID = 3
			fakeClient.response = remote.TimelineHistoryResponse{
				TimelineID: 3,
				Content:    "2\t0/9000000\tno recovery target specified\n",
			}
			cluster.Status.ReplicaDivergenceWatermarks = map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark{
				"replica-1": {
					TimeLineID:  3,
					ReceivedLSN: "0/9500000",
					Since:       pastTimestamp(replicaDivergenceStallGracePeriod + 5*time.Second),
				},
			}
			statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				makePrimary("primary", true),
				// timeline 1 does not appear anywhere in the served history
				makeReplica("replica-1", "uid-1", 1, "0/9500000", nil),
			}}

			env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

			issue, ok := cluster.Status.ReplicaWalIssues["replica-1"]
			Expect(ok).To(BeTrue())
			Expect(issue.Kind).To(Equal(apiv1.ReplicaWalIssueStuck))
			Expect(issue.ForkLSN).To(BeEmpty())
		})

	It("does not re-run the fork-point check for an instance already evaluated against the current timeline",
		func(ctx SpecContext) {
			cluster.Status.ReplicaWalIssues = map[apiv1.PodName]apiv1.ReplicaWalIssueStatus{
				"replica-1": {Kind: apiv1.ReplicaWalIssueStuck, DetectedTimeLineID: 2},
			}
			statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				makePrimary("primary", true),
				makeReplica("replica-1", "uid-1", 1, "0/7500000", nil),
			}}

			env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

			Expect(fakeClient.calls).To(Equal(0))
			Expect(cluster.Status.ReplicaWalIssues["replica-1"].Kind).To(Equal(apiv1.ReplicaWalIssueStuck))
		})

	It("self-heals a latched issue once the instance reports a fresh, caught-up timeline", func(ctx SpecContext) {
		cluster.Status.ReplicaWalIssues = map[apiv1.PodName]apiv1.ReplicaWalIssueStatus{
			"replica-1": {Kind: apiv1.ReplicaWalIssueDiverged, DetectedTimeLineID: 1, PVCUID: "pvc-uid-old", Parked: true},
		}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makePrimary("primary", true),
			// rebuilt instance (new UID), fully caught up
			makeReplica("replica-1", "uid-new", 2, "0/7500000", nil),
		}}

		env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

		Expect(cluster.Status.ReplicaWalIssues).To(BeEmpty())
		Expect(fakeClient.calls).To(Equal(0))
	})

	It("leaves an unreachable instance's watermark untouched", func(ctx SpecContext) {
		cluster.Status.ReplicaDivergenceWatermarks = map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark{
			"replica-1": {TimeLineID: 2, ReceivedLSN: "0/7500000", Since: pastTimestamp(10 * time.Second)},
		}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makePrimary("primary", true),
			makeReplica("replica-1", "uid-1", 0, "", fmt.Errorf("connection refused")),
		}}

		env.clusterReconciler.evaluateReplicaDivergence(ctx, cluster, statuses)

		Expect(cluster.Status.ReplicaWalIssues).To(BeEmpty())
		Expect(cluster.Status.ReplicaDivergenceWatermarks["replica-1"].ReceivedLSN).To(Equal("0/7500000"))
		Expect(fakeClient.calls).To(Equal(0))
	})
})

var _ = Describe("reconcileDivergedReplicaContainment", func() {
	var env *testingEnvironment
	var namespace string
	var cluster *apiv1.Cluster

	makeReplica := func(name string, uid types.UID, ready bool) postgres.PostgresqlStatus {
		return postgres.PostgresqlStatus{
			Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid}},
			IsPodReady: ready,
		}
	}

	// makePgDataPVC builds the PGDATA PVC fixture for an instance, named and
	// labeled exactly as the real pgDataCalculator does (see
	// pkg/reconciler/persistentvolumeclaim/calculator.go), so findPgDataPVC
	// picks it up the same way it would a real cluster's PVC.
	makePgDataPVC := func(instanceName string, uid types.UID) corev1.PersistentVolumeClaim {
		return corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      instanceName,
				Namespace: namespace,
				UID:       uid,
				Labels:    map[string]string{utils.PvcRoleLabelName: string(utils.PVCRolePgData)},
			},
		}
	}

	// isFenced always re-reads the Cluster from the fake API server rather
	// than trusting the in-memory `cluster` variable: fencing is applied by
	// utils.FencingMetadataExecutor via its own independent Get+Patch cycle
	// on a throwaway copy, so the caller's object is not reliably updated
	// with the change in place.
	isFenced := func(instance string) bool {
		var fresh apiv1.Cluster
		Expect(env.client.Get(context.Background(), client.ObjectKeyFromObject(cluster), &fresh)).To(Succeed())
		fenced, err := utils.GetFencedInstances(fresh.Annotations)
		Expect(err).ToNot(HaveOccurred())
		return fenced.Has(instance)
	}

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace = newFakeNamespace(env.client)
		cluster = newFakeCNPGCluster(env.client, namespace)
		cluster.Status.CurrentPrimary = "primary"
		cluster.Status.TargetPrimary = "primary"
		cluster.Status.ReplicaWalIssues = map[apiv1.PodName]apiv1.ReplicaWalIssueStatus{
			"replica-1": {Kind: apiv1.ReplicaWalIssueDiverged, DetectedTimeLineID: 1},
		}
		Expect(env.client.Status().Update(context.Background(), cluster)).To(Succeed())
	})

	It("fences a confirmed-diverged replica by default (auto mode), recording its PGDATA PVC UID",
		func(ctx SpecContext) {
			statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				makeReplica("replica-1", "uid-1", true),
			}}
			pvcs := []corev1.PersistentVolumeClaim{makePgDataPVC("replica-1", "pvc-uid-1")}

			err := env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, statuses, pvcs)
			Expect(err).ToNot(HaveOccurred())

			Expect(isFenced("replica-1")).To(BeTrue())
			Expect(cluster.Status.ReplicaWalIssues["replica-1"].Parked).To(BeTrue())
			Expect(cluster.Status.ReplicaWalIssues["replica-1"].PVCUID).To(Equal("pvc-uid-1"))
		})

	It("only surfaces, never fences, when the kill-switch annotation requests detectOnly", func(ctx SpecContext) {
		if cluster.Annotations == nil {
			cluster.Annotations = map[string]string{}
		}
		cluster.Annotations[utils.DivergedReplicaHandlingAnnotationName] = apiv1.DivergedReplicaHandlingDetectOnly
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeReplica("replica-1", "uid-1", true),
		}}
		pvcs := []corev1.PersistentVolumeClaim{makePgDataPVC("replica-1", "pvc-uid-1")}

		err := env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, statuses, pvcs)
		Expect(err).ToNot(HaveOccurred())

		Expect(isFenced("replica-1")).To(BeFalse())
		Expect(cluster.Status.ReplicaWalIssues["replica-1"].Parked).To(BeFalse())
	})

	It("is idempotent once an instance is already parked", func(ctx SpecContext) {
		cluster.Status.ReplicaWalIssues["replica-1"] = apiv1.ReplicaWalIssueStatus{
			Kind: apiv1.ReplicaWalIssueDiverged, DetectedTimeLineID: 1, Parked: true, PVCUID: "pvc-uid-1",
		}
		Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeReplica("replica-1", "uid-1", false), // not ready: already fenced
		}}
		// Same PVC UID as recorded: no rebuild, must stay parked untouched.
		pvcs := []corev1.PersistentVolumeClaim{makePgDataPVC("replica-1", "pvc-uid-1")}

		err := env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, statuses, pvcs)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Status.ReplicaWalIssues["replica-1"].Parked).To(BeTrue())
		Expect(cluster.Status.ReplicaWalIssues).To(HaveKey(apiv1.PodName("replica-1")))
	})

	It("never fences when doing so would break the synchronous replication quorum", func(ctx SpecContext) {
		cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
			Method: apiv1.SynchronousReplicaConfigurationMethodAny,
			Number: 2,
		}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeReplica("replica-1", "uid-1", true),
			makeReplica("replica-2", "uid-2", true),
			// only 2 ready replicas total, and Number requires 2 to remain
			// available even after excluding the one being considered for
			// fencing -- fencing replica-1 would leave only replica-2 (1 < 2).
		}}
		pvcs := []corev1.PersistentVolumeClaim{makePgDataPVC("replica-1", "pvc-uid-1")}

		err := env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, statuses, pvcs)
		Expect(err).ToNot(HaveOccurred())

		Expect(isFenced("replica-1")).To(BeFalse())
		Expect(cluster.Status.ReplicaWalIssues["replica-1"].Parked).To(BeFalse())
	})

	It("refuses to fence an entry that names the current or target primary (defensive backstop)", func(ctx SpecContext) {
		cluster.Status.ReplicaWalIssues = map[apiv1.PodName]apiv1.ReplicaWalIssueStatus{
			"primary": {Kind: apiv1.ReplicaWalIssueDiverged, DetectedTimeLineID: 1},
		}
		Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: namespace, UID: "uid-primary"}}},
		}}
		pvcs := []corev1.PersistentVolumeClaim{makePgDataPVC("primary", "pvc-uid-primary")}

		err := env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, statuses, pvcs)
		Expect(err).ToNot(HaveOccurred())
		Expect(isFenced("primary")).To(BeFalse())
	})

	It("defers containment when the diverged instance's PGDATA PVC is not found this pass", func(ctx SpecContext) {
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeReplica("replica-1", "uid-1", true),
		}}

		err := env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, statuses, nil)
		Expect(err).ToNot(HaveOccurred())

		Expect(isFenced("replica-1")).To(BeFalse())
		Expect(cluster.Status.ReplicaWalIssues["replica-1"].Parked).To(BeFalse())
	})

	It("lifts containment and forgets the entry once a parked instance's PGDATA PVC is replaced by a fresh clone",
		func(ctx SpecContext) {
			cluster.Status.ReplicaWalIssues["replica-1"] = apiv1.ReplicaWalIssueStatus{
				Kind: apiv1.ReplicaWalIssueDiverged, DetectedTimeLineID: 1, Parked: true, PVCUID: "pvc-uid-old",
			}
			cluster.Status.ReplicaDivergenceWatermarks = map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark{
				"replica-1": {TimeLineID: 1, ReceivedLSN: "0/7500000", Since: "some-time"},
			}
			Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())
			// Fence it first, mirroring the state left by an earlier pass.
			Expect(env.clusterReconciler.setInstanceFencing(ctx, cluster, "replica-1", true)).To(Succeed())
			Expect(isFenced("replica-1")).To(BeTrue())

			statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				makeReplica("replica-1", "uid-1-rebuilt", false),
			}}
			// A genuine `kubectl cnpg destroy`: the PVC was deleted and
			// recreated from a fresh clone, so its UID differs from the one
			// recorded at parking time.
			pvcs := []corev1.PersistentVolumeClaim{makePgDataPVC("replica-1", "pvc-uid-new")}

			err := env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, statuses, pvcs)
			Expect(err).ToNot(HaveOccurred())

			Expect(isFenced("replica-1")).To(BeFalse())
			Expect(cluster.Status.ReplicaWalIssues).ToNot(HaveKey(apiv1.PodName("replica-1")))
			Expect(cluster.Status.ReplicaDivergenceWatermarks).ToNot(HaveKey(apiv1.PodName("replica-1")))
		})

	It("keeps a parked instance fenced when only its Pod is recreated over the SAME, still-diverged PVC",
		func(ctx SpecContext) {
			cluster.Status.ReplicaWalIssues["replica-1"] = apiv1.ReplicaWalIssueStatus{
				Kind: apiv1.ReplicaWalIssueDiverged, DetectedTimeLineID: 1, Parked: true, PVCUID: "pvc-uid-1",
			}
			cluster.Status.ReplicaDivergenceWatermarks = map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark{
				"replica-1": {TimeLineID: 1, ReceivedLSN: "0/7500000", Since: "some-time"},
			}
			Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())
			Expect(env.clusterReconciler.setInstanceFencing(ctx, cluster, "replica-1", true)).To(Succeed())
			Expect(isFenced("replica-1")).To(BeTrue())

			// A new Pod identity -- e.g. a node failure, eviction, or a
			// rollout recreated it, none of which is `kubectl cnpg destroy`
			// -- bound to the SAME PGDATA PVC (same UID): the data is still
			// the diverged data, so this must NOT lift containment.
			statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				makeReplica("replica-1", "uid-1-new-pod-same-pvc", false),
			}}
			pvcs := []corev1.PersistentVolumeClaim{makePgDataPVC("replica-1", "pvc-uid-1")}

			err := env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, statuses, pvcs)
			Expect(err).ToNot(HaveOccurred())

			Expect(isFenced("replica-1")).To(BeTrue())
			issue, ok := cluster.Status.ReplicaWalIssues["replica-1"]
			Expect(ok).To(BeTrue())
			Expect(issue.Parked).To(BeTrue())
			Expect(issue.PVCUID).To(Equal("pvc-uid-1"))
			Expect(cluster.Status.ReplicaDivergenceWatermarks).To(HaveKey(apiv1.PodName("replica-1")))
		})

	It("never touches a Stuck (not Diverged) entry", func(ctx SpecContext) {
		cluster.Status.ReplicaWalIssues = map[apiv1.PodName]apiv1.ReplicaWalIssueStatus{
			"replica-1": {Kind: apiv1.ReplicaWalIssueStuck, DetectedTimeLineID: 1},
		}
		Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeReplica("replica-1", "uid-1", true),
		}}
		pvcs := []corev1.PersistentVolumeClaim{makePgDataPVC("replica-1", "pvc-uid-1")}

		err := env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, statuses, pvcs)
		Expect(err).ToNot(HaveOccurred())

		Expect(isFenced("replica-1")).To(BeFalse())
		Expect(cluster.Status.ReplicaWalIssues["replica-1"].Parked).To(BeFalse())
	})
})

var _ = Describe("divergedReplicaParkWouldBreakSyncQuorum", func() {
	makeStatus := func(name string, ready bool) postgres.PostgresqlStatus {
		return postgres.PostgresqlStatus{
			Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}},
			IsPodReady: ready,
		}
	}

	It("never blocks containment when synchronous replication is not configured", func() {
		cluster := &apiv1.Cluster{}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{makeStatus("replica-1", true)}}
		Expect(divergedReplicaParkWouldBreakSyncQuorum(cluster, statuses, "replica-1")).To(BeFalse())
	})

	It("blocks containment when excluding the instance would drop below the required number", func() {
		cluster := &apiv1.Cluster{Spec: apiv1.ClusterSpec{
			PostgresConfiguration: apiv1.PostgresConfiguration{
				Synchronous: &apiv1.SynchronousReplicaConfiguration{
					Method: apiv1.SynchronousReplicaConfigurationMethodAny, Number: 1,
				},
			},
		}}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeStatus("replica-1", true),
		}}
		Expect(divergedReplicaParkWouldBreakSyncQuorum(cluster, statuses, "replica-1")).To(BeTrue())
	})

	It("allows containment when enough other ready replicas remain", func() {
		cluster := &apiv1.Cluster{Spec: apiv1.ClusterSpec{
			PostgresConfiguration: apiv1.PostgresConfiguration{
				Synchronous: &apiv1.SynchronousReplicaConfiguration{
					Method: apiv1.SynchronousReplicaConfigurationMethodAny, Number: 1,
				},
			},
		}}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeStatus("replica-1", true),
			makeStatus("replica-2", true),
		}}
		Expect(divergedReplicaParkWouldBreakSyncQuorum(cluster, statuses, "replica-1")).To(BeFalse())
	})

	It("does not count a not-Ready replica towards the available quorum", func() {
		cluster := &apiv1.Cluster{Spec: apiv1.ClusterSpec{
			PostgresConfiguration: apiv1.PostgresConfiguration{
				Synchronous: &apiv1.SynchronousReplicaConfiguration{
					Method: apiv1.SynchronousReplicaConfigurationMethodAny, Number: 1,
				},
			},
		}}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			makeStatus("replica-1", true),
			makeStatus("replica-2", false),
		}}
		Expect(divergedReplicaParkWouldBreakSyncQuorum(cluster, statuses, "replica-1")).To(BeTrue())
	})
})

var _ = Describe("updateReplicasHealthyCondition", func() {
	It("reports True/AllReplicasHealthy when there are no issues", func() {
		cluster := &apiv1.Cluster{}
		updateReplicasHealthyCondition(cluster)

		cond := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionReplicasHealthy))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonAllReplicasHealthy)))
	})

	It("reports False/ReplicaWalIssues, naming the affected instances, when issues are present", func() {
		cluster := &apiv1.Cluster{Status: apiv1.ClusterStatus{
			ReplicaWalIssues: map[apiv1.PodName]apiv1.ReplicaWalIssueStatus{
				"replica-2": {Kind: apiv1.ReplicaWalIssueStuck},
				"replica-1": {Kind: apiv1.ReplicaWalIssueDiverged},
			},
		}}
		updateReplicasHealthyCondition(cluster)

		cond := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionReplicasHealthy))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonReplicaWalIssues)))
		Expect(cond.Message).To(ContainSubstring("replica-1"))
		Expect(cond.Message).To(ContainSubstring("replica-2"))
	})
})

func ptrBool(b bool) *bool {
	return &b
}
