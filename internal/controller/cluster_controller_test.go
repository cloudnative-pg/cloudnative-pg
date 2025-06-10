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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	cnpgTypes "github.com/cloudnative-pg/machinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
)

var _ = Describe("reconcileResources", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	It("should delete a failed job and requeue", func(ctx SpecContext) {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)

		// Create the CA secrets that the cluster expects
		generateFakeCASecret(env.client, cluster.GetServerCASecretName(), namespace, "cluster-test")
		generateFakeCASecret(env.client, cluster.GetClientCASecretName(), namespace, "cluster-test")

		instanceName := cluster.Name + "-1"
		failedJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      instanceName + "-snapshot-recovery",
				Namespace: namespace,
				Labels: map[string]string{
					utils.ClusterLabelName:      cluster.Name,
					utils.InstanceNameLabelName: instanceName,
					utils.JobRoleLabelName:      "snapshot-recovery",
				},
			},
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{
						Type:   batchv1.JobFailed,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		// Create the failed job
		Expect(env.client.Create(ctx, failedJob)).To(Succeed())

		// Create minimal managed resources for the test
		managedResources := &managedResources{
			nodes:     make(map[string]corev1.Node),
			instances: corev1.PodList{Items: []corev1.Pod{}},
			pvcs:      corev1.PersistentVolumeClaimList{Items: []corev1.PersistentVolumeClaim{}},
			jobs:      batchv1.JobList{Items: []batchv1.Job{*failedJob}},
		}

		// Test the reconcileResources method directly to avoid architecture validation
		var instancesStatus postgres.PostgresqlStatusList
		result, err := env.clusterReconciler.reconcileResources(ctx, cluster, managedResources, instancesStatus)

		// Check the result
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Requeue).To(BeTrue())

		// Check if the job was deleted
		err = env.client.Get(ctx, client.ObjectKeyFromObject(failedJob), failedJob)
		Expect(err).To(HaveOccurred())
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
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
