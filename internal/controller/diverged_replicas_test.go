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

	DescribeTable("a parked entry",
		func(status func() postgres.PostgresqlStatus) {
			parked := apiv1.DivergedInstanceStatus{TimeLineID: 1, PrimaryTimeLineID: 2, Parked: true, PVCUID: "uid"}
			cluster.Status.DivergedInstances = map[apiv1.PodName]apiv1.DivergedInstanceStatus{"replica-1": parked}

			env.clusterReconciler.evaluateReplicaDivergence(cluster,
				postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{status()}})

			Expect(cluster.Status.DivergedInstances).To(HaveKeyWithValue(apiv1.PodName("replica-1"), parked))
		},
		// the data stays diverged until its PVC is replaced
		Entry("is kept when the instance reports no divergence", func() postgres.PostgresqlStatus {
			return replica(nil)
		}),
		// PostgreSQL is starting after the fence was lifted by hand, and
		// reports nothing yet
		Entry("is kept while the instance might be unavailable", func() postgres.PostgresqlStatus {
			starting := replica(nil)
			starting.MightBeUnavailable = true
			return starting
		}),
	)

	It("keeps an unparked entry while the instance might be unavailable", func() {
		cluster.Status.DivergedInstances = map[apiv1.PodName]apiv1.DivergedInstanceStatus{
			"replica-1": {TimeLineID: 1, PrimaryTimeLineID: 2},
		}
		starting := replica(nil)
		starting.MightBeUnavailable = true

		env.clusterReconciler.evaluateReplicaDivergence(cluster,
			postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{starting}})

		Expect(cluster.Status.DivergedInstances).To(HaveKey(apiv1.PodName("replica-1")))
	})

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
	unfence := func(ctx context.Context, instance string) {
		Expect(utils.NewFencingMetadataExecutor(env.client).RemoveFencing().ForInstance(instance).
			Execute(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
	}
	annotation := func() string {
		var fresh apiv1.Cluster
		Expect(env.client.Get(context.Background(), client.ObjectKeyFromObject(cluster), &fresh)).To(Succeed())
		return fresh.Annotations[utils.FencedInstanceAnnotation]
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
			[]corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-1")}, nil)).To(Succeed())

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
			[]corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-1")}, nil)).To(Succeed())

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
				claims, nil)).To(Succeed())

			Expect(isFenced(name)).To(BeFalse())
			Expect(cluster.Status.DivergedInstances[apiv1.PodName(name)].Parked).To(BeFalse())
		},
		Entry("before its PGDATA PVC is found", func(context.Context) {}, "replica-1", nil),
		Entry("an entry naming the primary", func(ctx context.Context) {
			setEntry(ctx, "primary", apiv1.DivergedInstanceStatus{TimeLineID: 1, PrimaryTimeLineID: 2})
		}, "primary", []string{"primary"}),
	)

	It("parks a replica covered by a fence on the whole cluster, leaving the annotation alone", func(ctx SpecContext) {
		fence(ctx, utils.FenceAllInstances)
		Expect(env.client.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())

		Expect(env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster,
			[]corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-1")}, nil)).To(Succeed())

		Expect(annotation()).To(Equal(`["*"]`))
		Expect(cluster.Status.DivergedInstances["replica-1"].Parked).To(BeTrue())
		Expect(cluster.Status.DivergedInstances["replica-1"].PVCUID).To(Equal("pvc-uid-1"))
		Expect(events()).ToNot(Receive())
	})

	It("changes nothing on a second pass", func(ctx SpecContext) {
		pvcs := []corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-1")}
		Expect(env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, pvcs, nil)).To(Succeed())
		Expect(events()).To(Receive())
		Expect(env.client.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
		entry := cluster.Status.DivergedInstances["replica-1"]
		resourceVersion := cluster.ResourceVersion

		Expect(env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, pvcs, nil)).To(Succeed())

		Expect(cluster.ResourceVersion).To(Equal(resourceVersion))
		Expect(isFenced("replica-1")).To(BeTrue())
		Expect(cluster.Status.DivergedInstances).To(HaveKeyWithValue(apiv1.PodName("replica-1"), entry))
		Expect(events()).ToNot(Receive())
	})

	DescribeTable("a parked instance",
		func(ctx SpecContext, setup func(context.Context), pvcs func() []corev1.PersistentVolumeClaim,
			podExists bool, released bool, annotationAfter string,
		) {
			var pods []corev1.Pod
			if podExists {
				pods = []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "replica-1"}}}
			}
			setEntry(ctx, "replica-1", apiv1.DivergedInstanceStatus{
				TimeLineID: 1, PrimaryTimeLineID: 2, Parked: true, PVCUID: "pvc-uid-old",
			})
			setup(ctx)
			// a reconcile starts from a fresh read of the cluster
			Expect(env.client.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())

			Expect(env.clusterReconciler.reconcileDivergedReplicaContainment(ctx, cluster, pvcs(), pods)).To(Succeed())

			Expect(annotation()).To(Equal(annotationAfter))
			if released {
				Expect(cluster.Status.DivergedInstances).To(BeEmpty())
				Expect(meta.IsStatusConditionTrue(cluster.Status.Conditions,
					string(apiv1.ConditionReplicasHealthy))).To(BeTrue())
			} else {
				Expect(cluster.Status.DivergedInstances["replica-1"].Parked).To(BeTrue())
			}
		},
		Entry("is released once its PGDATA PVC is replaced, as kubectl cnpg destroy does",
			func(ctx context.Context) { fence(ctx, "replica-1") },
			func() []corev1.PersistentVolumeClaim {
				return []corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-new")}
			}, false, true, ""),
		Entry("is released once its PGDATA PVC is gone",
			func(ctx context.Context) { fence(ctx, "replica-1") },
			func() []corev1.PersistentVolumeClaim { return nil }, false, true, ""),
		Entry("is released, keeping a fence on the whole cluster, once its PGDATA PVC is replaced",
			func(ctx context.Context) { fence(ctx, utils.FenceAllInstances) },
			func() []corev1.PersistentVolumeClaim {
				return []corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-new")}
			}, false, true, `["*"]`),
		Entry("is released, keeping a fence on the whole cluster, once its PGDATA PVC is gone",
			func(ctx context.Context) { fence(ctx, utils.FenceAllInstances) },
			func() []corev1.PersistentVolumeClaim { return nil }, false, true, `["*"]`),
		Entry("stays fenced when only its Pod is recreated over the same PVC",
			func(ctx context.Context) { fence(ctx, "replica-1") },
			func() []corev1.PersistentVolumeClaim {
				return []corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-old")}
			}, false, false, `["replica-1"]`),
		Entry("is fenced again after its fence was lifted by hand",
			func(ctx context.Context) {
				fence(ctx, "replica-1")
				unfence(ctx, "replica-1")
			},
			func() []corev1.PersistentVolumeClaim {
				return []corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-old")}
			}, false, false, `["replica-1"]`),
		Entry("is fenced again after a fence on the whole cluster was set and lifted",
			func(ctx context.Context) {
				fence(ctx, "replica-1")
				fence(ctx, utils.FenceAllInstances)
				unfence(ctx, utils.FenceAllInstances)
			},
			func() []corev1.PersistentVolumeClaim {
				return []corev1.PersistentVolumeClaim{pgDataPVC("replica-1", "pvc-uid-old")}
			}, false, false, `["replica-1"]`),
		// kubectl cnpg destroy --keep-pvc detaches the PVC before it deletes the Pod
		Entry("stays fenced while its PVC is missing from the list and its Pod still exists",
			func(ctx context.Context) { fence(ctx, "replica-1") },
			func() []corev1.PersistentVolumeClaim { return nil }, true, false, `["replica-1"]`),
		Entry("is fenced again while its PVC is missing from the list and its Pod still exists",
			func(ctx context.Context) {
				fence(ctx, "replica-1")
				unfence(ctx, "replica-1")
			},
			func() []corev1.PersistentVolumeClaim { return nil }, true, false, `["replica-1"]`),
	)
})

var _ = Describe("updateNoPromotableInstanceCondition", func() {
	active := func(names ...string) postgres.PostgresqlStatusList {
		var list postgres.PostgresqlStatusList
		for _, name := range names {
			list.Items = append(list.Items, postgres.PostgresqlStatus{
				Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}},
			})
		}
		return list
	}
	existing := metav1.Condition{
		Type:    string(apiv1.ConditionNoPromotableInstance),
		Status:  metav1.ConditionTrue,
		Reason:  string(apiv1.ConditionReasonOnlyDivergedInstances),
		Message: "set by an earlier pass",
	}

	DescribeTable("with instance-1 as primary",
		func(diverged []string, statuses postgres.PostgresqlStatusList, conditions []metav1.Condition, message string) {
			cluster := &apiv1.Cluster{Status: apiv1.ClusterStatus{
				CurrentPrimary: "instance-1",
				Conditions:     conditions,
			}}
			for _, name := range diverged {
				if cluster.Status.DivergedInstances == nil {
					cluster.Status.DivergedInstances = make(map[apiv1.PodName]apiv1.DivergedInstanceStatus)
				}
				cluster.Status.DivergedInstances[apiv1.PodName(name)] = apiv1.DivergedInstanceStatus{}
			}

			updateNoPromotableInstanceCondition(cluster, statuses)

			cond := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionNoPromotableInstance))
			if message == "" {
				Expect(cond).To(BeNil())
				return
			}
			Expect(cond).ToNot(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonOnlyDivergedInstances)))
			Expect(cond.Message).To(Equal(message))
		},
		Entry("sets the condition while only diverged instances are active",
			[]string{"instance-3", "instance-2"}, active("instance-2"), nil,
			"No instance can replace the primary instance-1: the only active instances are diverged, "+
				"and a diverged instance is never promoted: instance-2, instance-3"),
		Entry("removes the condition when another instance is active",
			[]string{"instance-2"}, active("instance-2", "instance-3"), []metav1.Condition{existing}, ""),
		Entry("removes the condition when no instance is recorded as diverged",
			nil, active("instance-2"), []metav1.Condition{existing}, ""),
		Entry("leaves the condition as it is when no instance is active",
			[]string{"instance-2"}, active(), []metav1.Condition{existing}, existing.Message),
	)
})
