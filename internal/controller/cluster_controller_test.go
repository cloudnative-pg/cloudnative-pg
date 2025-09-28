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

	"github.com/cloudnative-pg/machinery/pkg/log"
	cnpgTypes "github.com/cloudnative-pg/machinery/pkg/types"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Filtering cluster", func() {
	metrics := make(map[string]string, 1)
	metrics["a-secret"] = "test-version"

	cluster := apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			ImageName: "postgres:13.0",
		},
		Status: apiv1.ClusterStatus{
			SecretsResourceVersion:   apiv1.SecretsResourceVersion{Metrics: metrics},
			ConfigMapResourceVersion: apiv1.ConfigMapResourceVersion{Metrics: metrics},
		},
	}

	items := []apiv1.Cluster{cluster}
	clusterList := apiv1.ClusterList{Items: items}

	It("using a secret", func() {
		secret := corev1.Secret{}
		secret.Name = "a-secret"
		req := filterClustersUsingSecret(clusterList, &secret)
		Expect(req).ToNot(BeNil())
	})

	It("using a config map", func() {
		configMap := corev1.ConfigMap{}
		configMap.Name = "a-secret"
		req := filterClustersUsingConfigMap(clusterList, &configMap)
		Expect(req).ToNot(BeNil())
	})
})

var _ = Describe("Updating target primary", func() {
	var env *testingEnvironment
	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	It("selects the new target primary right away", func(ctx SpecContext) {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)

		By("creating the cluster resources")
		jobs := generateFakeInitDBJobs(env.client, cluster)
		instances := generateFakeClusterPods(env.client, cluster, true)
		pvc := generateClusterPVC(env.client, cluster, persistentvolumeclaim.StatusReady)

		managedResources := &managedResources{
			nodes:     nil,
			instances: corev1.PodList{Items: instances},
			pvcs:      corev1.PersistentVolumeClaimList{Items: pvc},
			jobs:      batchv1.JobList{Items: jobs},
		}
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					CurrentLsn:  cnpgTypes.LSN("0/0"),
					ReceivedLsn: cnpgTypes.LSN("0/0"),
					ReplayLsn:   cnpgTypes.LSN("0/0"),
					IsPodReady:  true,
					Pod:         &instances[1],
				},
				{
					CurrentLsn:  cnpgTypes.LSN("0/0"),
					ReceivedLsn: cnpgTypes.LSN("0/0"),
					ReplayLsn:   cnpgTypes.LSN("0/0"),
					IsPodReady:  true,
					Pod:         &instances[2],
				},
				{
					CurrentLsn:  cnpgTypes.LSN("0/0"),
					ReceivedLsn: cnpgTypes.LSN("0/0"),
					ReplayLsn:   cnpgTypes.LSN("0/0"),
					IsPodReady:  false,
					Pod:         &instances[0],
				},
			},
		}

		By("creating the status list from the cluster pods", func() {
			cluster.Status.TargetPrimary = instances[0].Name
		})

		By("updating target primary pods for the cluster", func() {
			selectedPrimary, err := env.clusterReconciler.reconcileTargetPrimaryFromPods(
				ctx,
				cluster,
				statusList,
				managedResources,
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(selectedPrimary).To(Equal(statusList.Items[0].Pod.Name))
		})
	})

	It("it should wait the failover delay to select the new target primary", func(ctx SpecContext) {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace, func(cluster *apiv1.Cluster) {
			cluster.Spec.FailoverDelay = 2
		})

		By("creating the cluster resources")
		jobs := generateFakeInitDBJobs(env.client, cluster)
		instances := generateFakeClusterPods(env.client, cluster, true)
		pvc := generateClusterPVC(env.client, cluster, persistentvolumeclaim.StatusReady)

		managedResources := &managedResources{
			nodes:     nil,
			instances: corev1.PodList{Items: instances},
			pvcs:      corev1.PersistentVolumeClaimList{Items: pvc},
			jobs:      batchv1.JobList{Items: jobs},
		}
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					CurrentLsn:  cnpgTypes.LSN("0/0"),
					ReceivedLsn: cnpgTypes.LSN("0/0"),
					ReplayLsn:   cnpgTypes.LSN("0/0"),
					IsPodReady:  false,
					IsPrimary:   false,
					Pod:         &instances[0],
				},
				{
					CurrentLsn:  cnpgTypes.LSN("0/0"),
					ReceivedLsn: cnpgTypes.LSN("0/0"),
					ReplayLsn:   cnpgTypes.LSN("0/0"),
					IsPodReady:  false,
					IsPrimary:   true,
					Pod:         &instances[1],
				},
				{
					CurrentLsn:  cnpgTypes.LSN("0/0"),
					ReceivedLsn: cnpgTypes.LSN("0/0"),
					ReplayLsn:   cnpgTypes.LSN("0/0"),
					IsPodReady:  true,
					Pod:         &instances[2],
				},
			},
		}

		By("creating the status list from the cluster pods", func() {
			cluster.Status.TargetPrimary = instances[1].Name
			cluster.Status.CurrentPrimary = instances[1].Name
		})

		By("returning the ErrWaitingOnFailOverDelay when first detecting the failure", func() {
			selectedPrimary, err := env.clusterReconciler.reconcileTargetPrimaryForNonReplicaCluster(
				ctx,
				cluster,
				statusList,
				managedResources,
			)

			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(ErrWaitingOnFailOverDelay))
			Expect(selectedPrimary).To(Equal(""))
		})

		By("eventually updating the primary pod once the delay is elapsed", func() {
			Eventually(func(g Gomega) {
				selectedPrimary, err := env.clusterReconciler.reconcileTargetPrimaryForNonReplicaCluster(
					ctx,
					cluster,
					statusList,
					managedResources,
				)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(selectedPrimary).To(Equal(statusList.Items[0].Pod.Name))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})
	})

	It("Issue #1783: ensure that the scale-down behaviour remain consistent", func(ctx SpecContext) {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace, func(cluster *apiv1.Cluster) {
			cluster.Spec.Instances = 2
			cluster.Status.LatestGeneratedNode = 2
			cluster.Status.ReadyInstances = 2
		})

		By("creating the cluster resources")
		jobs := generateFakeInitDBJobs(env.client, cluster)
		instances := generateFakeClusterPods(env.client, cluster, true)
		pvcs := generateClusterPVC(env.client, cluster, persistentvolumeclaim.StatusReady)
		thirdInstancePVCGroup := newFakePVC(env.client, cluster, 3, persistentvolumeclaim.StatusReady)
		pvcs = append(pvcs, thirdInstancePVCGroup...)

		cluster.Status.DanglingPVC = append(cluster.Status.DanglingPVC, thirdInstancePVCGroup[0].Name)

		managedResources := &managedResources{
			nodes:     nil,
			instances: corev1.PodList{Items: instances},
			pvcs:      corev1.PersistentVolumeClaimList{Items: pvcs},
			jobs:      batchv1.JobList{Items: jobs},
		}
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					CurrentLsn:         cnpgTypes.LSN("0/0"),
					ReceivedLsn:        cnpgTypes.LSN("0/0"),
					ReplayLsn:          cnpgTypes.LSN("0/0"),
					IsPodReady:         true,
					IsPrimary:          false,
					Pod:                &instances[0],
					MightBeUnavailable: false,
				},
				{
					CurrentLsn:         cnpgTypes.LSN("0/0"),
					ReceivedLsn:        cnpgTypes.LSN("0/0"),
					ReplayLsn:          cnpgTypes.LSN("0/0"),
					IsPodReady:         true,
					IsPrimary:          true,
					Pod:                &instances[1],
					MightBeUnavailable: false,
				},
			},
		}

		By("triggering ensureInstancesAreCreated", func() {
			res, err := env.clusterReconciler.ensureInstancesAreCreated(ctx, cluster, managedResources, statusList)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{RequeueAfter: time.Second}))
		})

		By("checking that the third instance exists even if the cluster has two instances", func() {
			var expectedPod corev1.Pod
			instanceName := specs.GetInstanceName(cluster.Name, 3)
			err := env.clusterReconciler.Get(ctx, types.NamespacedName{
				Name:      instanceName,
				Namespace: cluster.Namespace,
			}, &expectedPod)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("isNodeUnschedulableOrBeingDrained", func() {
	node := &corev1.Node{}
	nodeUnschedulable := &corev1.Node{
		Spec: corev1.NodeSpec{
			Unschedulable: true,
		},
	}
	nodeTainted := &corev1.Node{
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{
					Key:    "karpenter.sh/disrupted",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
	}
	nodeWithUnknownTaint := &corev1.Node{
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{
					Key:    "unknown.io/taint",
					Effect: corev1.TaintEffectPreferNoSchedule,
				},
			},
		},
	}

	DescribeTable(
		"it detects nodes that are unschedulable or being drained",
		func(node *corev1.Node, expected bool) {
			Expect(isNodeUnschedulableOrBeingDrained(node, configuration.DefaultDrainTaints)).To(Equal(expected))
		},
		Entry("plain node", node, false),
		Entry("node is unschedulable", nodeUnschedulable, true),
		Entry("node is tainted", nodeTainted, true),
		Entry("node has an unknown taint", nodeWithUnknownTaint, false),
	)
})

func (r *ClusterReconciler) pausePoolersDuringSwitchoverTest(ctx context.Context, cluster *apiv1.Cluster, poolers []apiv1.Pooler) error {
	contextLogger := log.FromContext(ctx)

	poolerList := apiv1.PoolerList{Items: poolers}

	if len(poolerList.Items) == 0 {
		contextLogger.Debug("No poolers found for cluster, skipping pause operation")
		return nil
	}

	contextLogger.Info("Pausing poolers during switchover", "poolerCount", len(poolerList.Items))

	for i := range poolerList.Items {
		pooler := &poolerList.Items[i]

		if pooler.Spec.Cluster.Name != cluster.Name {
			continue
		}

		if pooler.Spec.PgBouncer != nil && pooler.Spec.PgBouncer.IsPaused() {
			contextLogger.Debug("Pooler is already paused, skipping", "pooler", pooler.Name)
			continue
		}

		if !pooler.IsAutomatedIntegration() {
			contextLogger.Debug("Pooler has manual auth configuration, skipping pause", "pooler", pooler.Name)
			continue
		}

		poolerCopy := pooler.DeepCopy()
		if poolerCopy.Spec.PgBouncer == nil {
			poolerCopy.Spec.PgBouncer = &apiv1.PgBouncerSpec{}
		}

		poolerCopy.Spec.PgBouncer.Paused = &[]bool{true}[0]
		if poolerCopy.Annotations == nil {
			poolerCopy.Annotations = make(map[string]string)
		}
		poolerCopy.Annotations["cnpg.io/pausedDuringSwitchover"] = "true"

		if err := r.Update(ctx, poolerCopy); err != nil {
			contextLogger.Error(err, "Failed to pause pooler during switchover", "pooler", pooler.Name)
			continue
		}

		contextLogger.Info("Paused pooler during switchover", "pooler", pooler.Name)
	}

	return nil
}

func (r *ClusterReconciler) resumePoolersAfterSwitchoverTest(ctx context.Context, cluster *apiv1.Cluster, poolers []apiv1.Pooler) error {
	contextLogger := log.FromContext(ctx)

	poolerList := apiv1.PoolerList{Items: poolers}

	if len(poolerList.Items) == 0 {
		contextLogger.Debug("No poolers found for cluster, skipping resume operation")
		return nil
	}

	contextsResumed := 0
	for i := range poolerList.Items {
		pooler := &poolerList.Items[i]

		if pooler.Spec.Cluster.Name != cluster.Name {
			continue
		}

		if pooler.Annotations == nil || pooler.Annotations["cnpg.io/pausedDuringSwitchover"] != "true" {
			continue
		}

		poolerCopy := pooler.DeepCopy()
		if poolerCopy.Spec.PgBouncer == nil {
			continue 
		}

		poolerCopy.Spec.PgBouncer.Paused = &[]bool{false}[0]
		delete(poolerCopy.Annotations, "cnpg.io/pausedDuringSwitchover")

		if err := r.Update(ctx, poolerCopy); err != nil {
			contextLogger.Error(err, "Failed to resume pooler after switchover", "pooler", pooler.Name)
			continue
		}

		contextsResumed++
		contextLogger.Info("Resumed pooler after switchover", "pooler", pooler.Name)
	}

	if contextsResumed > 0 {
		contextLogger.Info("Resumed poolers after switchover", "poolerCount", contextsResumed)
	}

	return nil
}

var _ = Describe("Pooler switchover integration", func() {
	var env *testingEnvironment
	var ctx context.Context
	var namespace string
	var cluster *apiv1.Cluster
	var pooler *apiv1.Pooler

	BeforeEach(func() {
		env = buildTestEnvironment()
		ctx = context.Background()
		namespace = newFakeNamespace(env.client)
		cluster = newFakeCNPGCluster(env.client, namespace)
		pooler = newFakePooler(env.client, cluster)
	})

	Context("pausePoolersDuringSwitchover", func() {
		It("should pause poolers with automated integration during switchover", func() {
			pooler.Spec.PgBouncer = &apiv1.PgBouncerSpec{
				PoolMode: apiv1.PgBouncerPoolModeSession,
			}
			err := env.client.Update(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			err = env.clusterReconciler.pausePoolersDuringSwitchoverTest(ctx, cluster, []apiv1.Pooler{*pooler})
			Expect(err).ToNot(HaveOccurred())

			var updatedPooler apiv1.Pooler
			err = env.client.Get(ctx, client.ObjectKeyFromObject(pooler), &updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeTrue())
			Expect(updatedPooler.Annotations["cnpg.io/pausedDuringSwitchover"]).To(Equal("true"))
		})

		It("should skip poolers that are already paused", func() {
			pooler.Spec.PgBouncer = &apiv1.PgBouncerSpec{
				PoolMode: apiv1.PgBouncerPoolModeSession,
				Paused:   ptr.To(true),
			}
			err := env.client.Update(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			err = env.clusterReconciler.pausePoolersDuringSwitchoverTest(ctx, cluster, []apiv1.Pooler{*pooler})
			Expect(err).ToNot(HaveOccurred())

			var updatedPooler apiv1.Pooler
			err = env.client.Get(ctx, client.ObjectKeyFromObject(pooler), &updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeTrue())
			Expect(updatedPooler.Annotations).ToNot(HaveKey("cnpg.io/pausedDuringSwitchover"))
		})

		It("should skip poolers with manual auth configuration", func() {
			pooler.Spec.PgBouncer = &apiv1.PgBouncerSpec{
				PoolMode:  apiv1.PgBouncerPoolModeSession,
				AuthQuery: "SELECT username, password FROM users WHERE username = $1",
			}
			err := env.client.Update(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			err = env.clusterReconciler.pausePoolersDuringSwitchoverTest(ctx, cluster, []apiv1.Pooler{*pooler})
			Expect(err).ToNot(HaveOccurred())

			var updatedPooler apiv1.Pooler
			err = env.client.Get(ctx, client.ObjectKeyFromObject(pooler), &updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
			Expect(updatedPooler.Annotations).ToNot(HaveKey("cnpg.io/pausedDuringSwitchover"))
		})

		It("should handle clusters with no poolers gracefully", func() {
			err := env.clusterReconciler.pausePoolersDuringSwitchoverTest(ctx, cluster, []apiv1.Pooler{})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("resumePoolersAfterSwitchover", func() {
		It("should resume poolers that were paused during switchover", func() {
			pooler.Spec.PgBouncer = &apiv1.PgBouncerSpec{
				PoolMode: apiv1.PgBouncerPoolModeSession,
				Paused:   ptr.To(true),
			}
			if pooler.Annotations == nil {
				pooler.Annotations = make(map[string]string)
			}
			pooler.Annotations["cnpg.io/pausedDuringSwitchover"] = "true"
			err := env.client.Update(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			err = env.clusterReconciler.resumePoolersAfterSwitchoverTest(ctx, cluster, []apiv1.Pooler{*pooler})
			Expect(err).ToNot(HaveOccurred())

			var updatedPooler apiv1.Pooler
			err = env.client.Get(ctx, client.ObjectKeyFromObject(pooler), &updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
			Expect(updatedPooler.Annotations).ToNot(HaveKey("cnpg.io/pausedDuringSwitchover"))
		})

		It("should skip poolers that were not paused during switchover", func() {
			pooler.Spec.PgBouncer = &apiv1.PgBouncerSpec{
				PoolMode: apiv1.PgBouncerPoolModeSession,
				Paused:   ptr.To(true),
			}
			err := env.client.Update(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			err = env.clusterReconciler.resumePoolersAfterSwitchoverTest(ctx, cluster, []apiv1.Pooler{*pooler})
			Expect(err).ToNot(HaveOccurred())

			var updatedPooler apiv1.Pooler
			err = env.client.Get(ctx, client.ObjectKeyFromObject(pooler), &updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeTrue())
		})

		It("should handle clusters with no poolers gracefully", func() {
			err := env.clusterReconciler.resumePoolersAfterSwitchoverTest(ctx, cluster, []apiv1.Pooler{})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("full switchover integration test", func() {
		It("should pause poolers during switchover and resume them after completion", func() {
			pooler.Spec.PgBouncer = &apiv1.PgBouncerSpec{
				PoolMode: apiv1.PgBouncerPoolModeSession,
			}
			err := env.client.Update(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			var initialPooler apiv1.Pooler
			err = env.client.Get(ctx, client.ObjectKeyFromObject(pooler), &initialPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(initialPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())

			err = env.clusterReconciler.pausePoolersDuringSwitchoverTest(ctx, cluster, []apiv1.Pooler{*pooler})
			Expect(err).ToNot(HaveOccurred())

			var pausedPooler apiv1.Pooler
			err = env.client.Get(ctx, client.ObjectKeyFromObject(pooler), &pausedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(pausedPooler.Spec.PgBouncer.IsPaused()).To(BeTrue())
			Expect(pausedPooler.Annotations["cnpg.io/pausedDuringSwitchover"]).To(Equal("true"))

			err = env.clusterReconciler.resumePoolersAfterSwitchoverTest(ctx, cluster, []apiv1.Pooler{pausedPooler})
			Expect(err).ToNot(HaveOccurred())

			var resumedPooler apiv1.Pooler
			err = env.client.Get(ctx, client.ObjectKeyFromObject(pooler), &resumedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(resumedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
			Expect(resumedPooler.Annotations).ToNot(HaveKey("cnpg.io/pausedDuringSwitchover"))
		})
	})
})
