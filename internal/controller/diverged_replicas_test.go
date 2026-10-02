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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("evaluateReplicaDivergence", func() {
	var env *testingEnvironment
	var cluster *apiv1.Cluster

	divergence := &postgres.TimelineDivergence{
		TimeLineID: 1, PrimaryTimeLineID: 2, ForkLSN: "0/4000000", ReplayLSN: "0/46210C8",
	}
	replica := func(reported *postgres.TimelineDivergence) postgres.PostgresqlStatus {
		return postgres.PostgresqlStatus{
			Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "replica-1"}},
			Divergence: reported,
		}
	}
	replicasHealthy := func() metav1.Condition {
		cond := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionReplicasHealthy))
		Expect(cond).ToNot(BeNil())
		return *cond
	}

	BeforeEach(func() {
		env = buildTestEnvironment()
		cluster = newFakeCNPGCluster(env.client, newFakeNamespace(env.client))
	})

	It("records the divergence a replica reports, with an event and the condition", func() {
		env.clusterReconciler.evaluateReplicaDivergence(cluster,
			postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{replica(divergence)}})

		diverged := cluster.Status.DivergedInstances["replica-1"]
		Expect(diverged.DetectedAt).ToNot(BeEmpty())
		diverged.DetectedAt = ""
		Expect(diverged).To(Equal(apiv1.DivergedInstanceStatus{
			TimeLineID: 1, PrimaryTimeLineID: 2, ForkLSN: "0/4000000", ReplayLSN: "0/46210C8",
		}))
		Expect(env.clusterReconciler.Recorder.(*record.FakeRecorder).Events).To(Receive(ContainSubstring("ReplicaDiverged")))
		Expect(replicasHealthy().Status).To(Equal(metav1.ConditionFalse))
		Expect(replicasHealthy().Reason).To(Equal(string(apiv1.ConditionReasonReplicasDiverged)))
	})

	It("keeps an existing entry as first reported while the replica keeps reporting a divergence", func() {
		recorded := apiv1.DivergedInstanceStatus{TimeLineID: 1, PrimaryTimeLineID: 2, DetectedAt: "earlier"}
		cluster.Status.DivergedInstances = map[apiv1.PodName]apiv1.DivergedInstanceStatus{"replica-1": recorded}

		env.clusterReconciler.evaluateReplicaDivergence(cluster, postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{replica(&postgres.TimelineDivergence{
				TimeLineID: 1, PrimaryTimeLineID: 3, ReplayLSN: "0/5000000",
			})},
		})

		Expect(cluster.Status.DivergedInstances).To(HaveKeyWithValue(apiv1.PodName("replica-1"), recorded))
		Expect(env.clusterReconciler.Recorder.(*record.FakeRecorder).Events).ToNot(Receive())
	})

	DescribeTable("the entry of an instance",
		func(status func() postgres.PostgresqlStatus, kept bool) {
			cluster.Status.DivergedInstances = map[apiv1.PodName]apiv1.DivergedInstanceStatus{
				"replica-1": {TimeLineID: 1, PrimaryTimeLineID: 2},
			}

			env.clusterReconciler.evaluateReplicaDivergence(cluster,
				postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{status()}})

			if kept {
				Expect(cluster.Status.DivergedInstances).To(HaveKey(apiv1.PodName("replica-1")))
				return
			}
			Expect(cluster.Status.DivergedInstances).To(BeEmpty())
			Expect(replicasHealthy().Status).To(Equal(metav1.ConditionTrue))
			Expect(replicasHealthy().Reason).To(Equal(string(apiv1.ConditionReasonAllReplicasHealthy)))
		},
		Entry("is removed once it no longer reports a divergence", func() postgres.PostgresqlStatus {
			return replica(nil)
		}, false),
		// kubectl cnpg promote can still pick a diverged replica
		Entry("is removed once it runs as primary", func() postgres.PostgresqlStatus {
			promoted := replica(nil)
			promoted.IsPrimary = true
			return promoted
		}, false),
		// a fenced instance does not run PostgreSQL, so its report carries no
		// divergence
		Entry("is kept while it is fenced", func() postgres.PostgresqlStatus {
			fenced := replica(nil)
			fenced.IsFenced = true
			return fenced
		}, true),
		Entry("is kept while it is not reporting", func() postgres.PostgresqlStatus {
			unreachable := replica(nil)
			unreachable.Error = fmt.Errorf("connection refused")
			return unreachable
		}, true),
	)

	It("does nothing for a replica cluster", func() {
		cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{Enabled: ptr.To(true)}

		env.clusterReconciler.evaluateReplicaDivergence(cluster,
			postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{replica(divergence)}})

		Expect(cluster.Status.DivergedInstances).To(BeEmpty())
	})
})

var _ = Describe("reconcileDivergedReplicaContainment", func() {
	var env *testingEnvironment
	var cluster *apiv1.Cluster

	readyReplica := func(name string) postgres.PostgresqlStatus {
		return postgres.PostgresqlStatus{
			Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cluster.Namespace}},
			IsPodReady: true,
		}
	}
	pgDataPVC := func(instanceName string, uid types.UID) corev1.PersistentVolumeClaim {
		return corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: cluster.Namespace,
			UID:       uid,
			Labels:    map[string]string{utils.PvcRoleLabelName: string(utils.PVCRolePgData)},
		}}
	}
	// fencing goes through its own Get and Patch, so only a fresh read shows it
	isFenced := func(instance string) bool {
		var fresh apiv1.Cluster
		Expect(env.client.Get(context.Background(), client.ObjectKeyFromObject(cluster), &fresh)).To(Succeed())
		fenced, err := utils.GetFencedInstances(fresh.Annotations)
		Expect(err).ToNot(HaveOccurred())
		return fenced.Has(instance)
	}
	fence := func(ctx context.Context, instance string) {
		Expect(utils.NewFencingMetadataExecutor(env.client).AddFencing().ForInstance(instance).
			Execute(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
	}
	setEntry := func(ctx context.Context, name apiv1.PodName, entry apiv1.DivergedInstanceStatus) {
		cluster.Status.DivergedInstances = map[apiv1.PodName]apiv1.DivergedInstanceStatus{name: entry}
		Expect(env.client.Status().Update(ctx, cluster)).To(Succeed())
	}
	events := func() chan string {
		return env.clusterReconciler.Recorder.(*record.FakeRecorder).Events
	}

	BeforeEach(func(ctx SpecContext) {
		env = buildTestEnvironment()
		cluster = newFakeCNPGCluster(env.client, newFakeNamespace(env.client))
		cluster.Status.CurrentPrimary = "primary"
		cluster.Status.TargetPrimary = "primary"
		setEntry(ctx, "replica-1", apiv1.DivergedInstanceStatus{TimeLineID: 1, PrimaryTimeLineID: 2})
	})

	It("fences a diverged replica, recording its PGDATA PVC UID", func(ctx SpecContext) {
		Expect(env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster,
			postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{readyReplica("replica-1")}},
			[]corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-1")})).To(Succeed())

		Expect(isFenced("replica-1")).To(BeTrue())
		Expect(cluster.Status.DivergedInstances["replica-1"].Parked).To(BeTrue())
		Expect(cluster.Status.DivergedInstances["replica-1"].PVCUID).To(Equal("pvc-uid-1"))
		Expect(events()).To(Receive(ContainSubstring("Fenced instance replica-1")))
	})

	It("records the parking without a second event when the fence is already in place", func(ctx SpecContext) {
		// a pass reading a cache that has the previous pass's fence but not
		// yet its parked entry
		fence(ctx, "replica-1")
		Expect(env.client.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())

		Expect(env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster,
			postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{readyReplica("replica-1")}},
			[]corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-1")})).To(Succeed())

		Expect(cluster.Status.DivergedInstances["replica-1"].Parked).To(BeTrue())
		Expect(events()).ToNot(Receive())
	})

	DescribeTable("does not fence",
		func(ctx SpecContext, setup func(ctx context.Context), name string, pvcs []string) {
			setup(ctx)
			claims := make([]corev1.PersistentVolumeClaim, 0, len(pvcs))
			for _, pvc := range pvcs {
				claims = append(claims, pgDataPVC(pvc, types.UID("uid-"+pvc)))
			}

			Expect(env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster,
				postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{readyReplica(name)}},
				claims)).To(Succeed())

			Expect(isFenced(name)).To(BeFalse())
			Expect(cluster.Status.DivergedInstances[apiv1.PodName(name)].Parked).To(BeFalse())
		},
		Entry("when that would break the synchronous quorum", func(context.Context) {
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				Method: apiv1.SynchronousReplicaConfigurationMethodAny, Number: 1,
			}
		}, "replica-1", []string{"replica-1"}),
		Entry("before its PGDATA PVC is found", func(context.Context) {}, "replica-1", nil),
		Entry("an entry naming the primary", func(ctx context.Context) {
			setEntry(ctx, "primary", apiv1.DivergedInstanceStatus{TimeLineID: 1, PrimaryTimeLineID: 2})
		}, "primary", []string{"primary"}),
	)

	DescribeTable("a parked instance",
		func(ctx SpecContext, currentPVCUID types.UID, released bool) {
			parked := apiv1.DivergedInstanceStatus{
				TimeLineID: 1, PrimaryTimeLineID: 2, Parked: true, PVCUID: "pvc-uid-old",
			}
			setEntry(ctx, "replica-1", parked)
			fence(ctx, "replica-1")

			Expect(env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster,
				postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{readyReplica("replica-1")}},
				[]corev1.PersistentVolumeClaim{pgDataPVC("replica-1", currentPVCUID)})).To(Succeed())

			Expect(isFenced("replica-1")).To(Equal(!released))
			if released {
				Expect(cluster.Status.DivergedInstances).To(BeEmpty())
			} else {
				Expect(cluster.Status.DivergedInstances).To(HaveKeyWithValue(apiv1.PodName("replica-1"), parked))
			}
		},
		Entry("is released once its PGDATA PVC is replaced, as kubectl cnpg destroy does",
			types.UID("pvc-uid-new"), true),
		Entry("stays fenced when only its Pod is recreated over the same PVC",
			types.UID("pvc-uid-old"), false),
	)
})

var _ = DescribeTable("divergedReplicaParkWouldBreakSyncQuorum",
	func(number int, otherReplicaReady bool, expected bool) {
		cluster := &apiv1.Cluster{}
		if number > 0 {
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				Method: apiv1.SynchronousReplicaConfigurationMethodAny, Number: number,
			}
		}
		statuses := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
			{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "replica-1"}}, IsPodReady: true},
			{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "replica-2"}}, IsPodReady: otherReplicaReady},
		}}
		Expect(divergedReplicaParkWouldBreakSyncQuorum(cluster, statuses, "replica-1")).To(Equal(expected))
	},
	Entry("not without synchronous replication", 0, false, false),
	Entry("not while enough other replicas are ready", 1, true, false),
	Entry("when the other replica is not ready", 1, false, true),
)
