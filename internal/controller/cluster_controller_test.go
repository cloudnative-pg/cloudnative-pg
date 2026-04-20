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
	"fmt"
	"sort"
	"time"

	cnpgTypes "github.com/cloudnative-pg/machinery/pkg/types"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

	It("Issue #9786: findInstancePodToCreate selects podless resizing PVC classified as dangling by EnrichStatus",
		func(ctx SpecContext) {
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(cluster *apiv1.Cluster) {
				cluster.Spec.Instances = 2
				cluster.Status.LatestGeneratedNode = 2
				cluster.Status.ReadyInstances = 1
			})

			By("creating cluster PVCs and marking them as resizing")
			pvcs := generateClusterPVC(env.client, cluster, persistentvolumeclaim.StatusReady)
			for i := range pvcs {
				pvcs[i].Status.Phase = corev1.ClaimBound
				pvcs[i].Status.Conditions = append(pvcs[i].Status.Conditions, corev1.PersistentVolumeClaimCondition{
					Type:   corev1.PersistentVolumeClaimResizing,
					Status: corev1.ConditionTrue,
				})
			}

			By("creating a pod for instance 1 only (instance 2's pod was deleted during rolling update)")
			pod1, err := specs.NewInstance(ctx, *cluster, 1, true)
			Expect(err).ToNot(HaveOccurred())
			cluster.SetInheritedDataAndOwnership(&pod1.ObjectMeta)
			Expect(env.client.Create(ctx, pod1)).To(Succeed())
			pod1.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
				},
			}

			By("running EnrichStatus to classify PVCs")
			persistentvolumeclaim.EnrichStatus(ctx, cluster, []corev1.Pod{*pod1}, []batchv1.Job{}, pvcs)
			instance2Name := specs.GetInstanceName(cluster.Name, 2)
			Expect(cluster.Status.ResizingPVC).Should(HaveLen(1))
			Expect(cluster.Status.DanglingPVC).Should(Equal([]string{instance2Name}))

			By("verifying findInstancePodToCreate selects the dangling instance for pod creation")
			statusList := postgres.PostgresqlStatusList{
				Items: []postgres.PostgresqlStatus{
					{
						IsPodReady: true,
						IsPrimary:  true,
						Pod:        pod1,
					},
				},
			}
			instanceToCreate, err := findInstancePodToCreate(ctx, cluster, statusList, pvcs)
			Expect(err).ToNot(HaveOccurred())
			Expect(instanceToCreate).ToNot(BeNil())
			Expect(instanceToCreate.Name).To(Equal(instance2Name))
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

var _ = Describe("evaluatePodReadinessGuards", func() {
	const (
		primaryName    = "cluster-1"
		replicaName    = "cluster-2"
		newPrimaryName = "cluster-3"
	)

	var (
		env     *testingEnvironment
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace := newFakeNamespace(env.client)
		cluster = newFakeCNPGCluster(env.client, namespace)
	})

	errStatusFailing := fmt.Errorf("status endpoint failing")

	readyReportingReplica := postgres.PostgresqlStatus{
		Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: replicaName}},
		IsPodReady: true,
	}
	readyReportingPrimary := postgres.PostgresqlStatus{
		Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: primaryName}},
		IsPodReady: true,
	}
	readyErroringPrimary := postgres.PostgresqlStatus{
		Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: primaryName}},
		IsPodReady: true,
		Error:      errStatusFailing,
	}
	unreadyErroringPrimary := postgres.PostgresqlStatus{
		Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: primaryName}},
		IsPodReady: false,
		Error:      errStatusFailing,
	}
	readyErroringReplica := postgres.PostgresqlStatus{
		Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: replicaName}},
		IsPodReady: true,
		Error:      errStatusFailing,
	}
	// kubeletStaleReporting is the "kubelet has not refreshed the probe yet"
	// shape: instance manager reports success (Error==nil, so HasHTTPStatus
	// returns true) but the pod is not yet Ready from the kubelet.
	kubeletStaleReporting := postgres.PostgresqlStatus{
		Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: primaryName}},
		IsPodReady: false,
	}

	DescribeTable(
		"guards behaviour",
		func(ctx SpecContext, currentPrimary, targetPrimary string, items []postgres.PostgresqlStatus, requeue bool) {
			cluster.Status.CurrentPrimary = currentPrimary
			cluster.Status.TargetPrimary = targetPrimary

			result, err := env.clusterReconciler.evaluatePodReadinessGuards(
				ctx, cluster, postgres.PostgresqlStatusList{Items: items},
			)
			Expect(err).NotTo(HaveOccurred())

			if requeue {
				Expect(result.RequeueAfter).To(Equal(10 * time.Second))
			} else {
				Expect(result.IsZero()).To(BeTrue())
			}
		},
		Entry("happy path: primary Ready and reporting, no guard fires",
			primaryName, primaryName,
			[]postgres.PostgresqlStatus{readyReportingPrimary, readyReportingReplica}, false),
		Entry("kubelet has not refreshed the readiness probe yet",
			primaryName, primaryName,
			[]postgres.PostgresqlStatus{kubeletStaleReporting, readyReportingReplica}, true),
		Entry("transient /pg/status failure on Ready primary requeues",
			primaryName, primaryName,
			[]postgres.PostgresqlStatus{readyReportingReplica, readyErroringPrimary}, true),
		Entry("guard is scoped to steady-state (CurrentPrimary == TargetPrimary)",
			primaryName, newPrimaryName,
			[]postgres.PostgresqlStatus{readyReportingReplica, readyErroringPrimary}, false),
		Entry("bootstrap: no primary elected yet",
			"", "",
			[]postgres.PostgresqlStatus{}, false),
		Entry("primary absent from status list (defensive)",
			primaryName, primaryName,
			[]postgres.PostgresqlStatus{readyReportingReplica}, false),
		Entry("current primary set but target primary cleared: guard does not fire",
			primaryName, "",
			[]postgres.PostgresqlStatus{readyReportingPrimary, readyErroringReplica}, false),
		Entry("erroring replica with reporting primary does not fire the guard",
			primaryName, primaryName,
			[]postgres.PostgresqlStatus{readyReportingPrimary, readyErroringReplica}, false),
		Entry("primary unready and not reporting (genuine failover case)",
			primaryName, primaryName,
			[]postgres.PostgresqlStatus{readyReportingReplica, unreadyErroringPrimary}, false),
		Entry("empty status list",
			primaryName, primaryName,
			[]postgres.PostgresqlStatus{}, false),
	)

	It("fires after the production sort pushes the erroring primary to the tail", func(ctx SpecContext) {
		// Build the list in "primary first" order as the instance manager
		// would populate it, then rely on PostgresqlStatusList.Less to push
		// the erroring primary to the tail. This locks the guard's
		// rationale against a regression in the sort.
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: primaryName}},
					IsPodReady: true,
					Error:      errStatusFailing,
				},
				{
					Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: replicaName}},
					IsPodReady: true,
				},
			},
		}
		sort.Sort(&statusList)
		Expect(statusList.Items[0].Pod.Name).To(Equal(replicaName),
			"sort must push the erroring primary to the tail")

		cluster.Status.CurrentPrimary = primaryName
		cluster.Status.TargetPrimary = primaryName

		result, err := env.clusterReconciler.evaluatePodReadinessGuards(ctx, cluster, statusList)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(10 * time.Second))
	})
})
