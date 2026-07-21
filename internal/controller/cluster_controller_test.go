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
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("reconcilePods instance recreation while a PVC is terminating (#10985)", func() {
	var env *testingEnvironment
	var namespace string

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace = newFakeNamespace(env.client)
	})

	// Builds a 3-instance cluster with WAL storage where instance serial 1 is
	// missing while instances 2 and 3 are up and reporting ready. This is the
	// state in which reconcilePods decides to recreate serial 1.
	newRecreatingCluster := func(ctx SpecContext) (*apiv1.Cluster, *managedResources, postgres.PostgresqlStatusList) {
		cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
			c.Spec.WalStorage = &apiv1.StorageConfiguration{Size: "1G"}
		})
		cluster.Status.Instances = 2
		cluster.Status.ReadyInstances = 2
		cluster.Status.InstanceNames = []string{
			specs.GetInstanceName(cluster.Name, 2),
			specs.GetInstanceName(cluster.Name, 3),
		}

		readyPod := func(serial int) *corev1.Pod {
			pod, err := specs.NewInstance(ctx, *cluster, serial, true)
			Expect(err).ToNot(HaveOccurred())
			pod.Status = corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			}
			return pod
		}
		pod2 := readyPod(2)
		pod3 := readyPod(3)

		resources := &managedResources{
			instances: corev1.PodList{Items: []corev1.Pod{*pod2, *pod3}},
		}
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{Pod: pod2, IsPodReady: true},
				{Pod: pod3, IsPodReady: true, IsPrimary: true},
			},
		}
		return cluster, resources, statusList
	}

	It("creates the join Job when no previous PVC is terminating", func(ctx SpecContext) {
		cluster, resources, statusList := newRecreatingCluster(ctx)

		res, err := env.clusterReconciler.reconcilePods(ctx, cluster, resources, statusList)
		Expect(err).To(MatchError(ErrNextLoop))
		Expect(res.RequeueAfter).To(Equal(30 * time.Second))

		var jobs batchv1.JobList
		Expect(env.client.List(ctx, &jobs)).To(Succeed())
		Expect(jobs.Items).To(HaveLen(1), "the join Job for the recreated instance should have been created")
	})

	It("defers recreation while a previous PVC for the same serial is still terminating", func(ctx SpecContext) {
		cluster, resources, statusList := newRecreatingCluster(ctx)
		// The WAL PVC of serial 1 has not finished terminating yet.
		resources.pvcs = corev1.PersistentVolumeClaimList{Items: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:              specs.GetInstanceName(cluster.Name, 1) + apiv1.WalArchiveVolumeSuffix,
					Namespace:         namespace,
					DeletionTimestamp: ptr.To(metav1.Now()),
				},
			},
		}}

		res, err := env.clusterReconciler.reconcilePods(ctx, cluster, resources, statusList)
		Expect(err).To(MatchError(ErrNextLoop))
		Expect(res.RequeueAfter).To(Equal(time.Second))

		var jobs batchv1.JobList
		Expect(env.client.List(ctx, &jobs)).To(Succeed())
		Expect(jobs.Items).To(BeEmpty(), "no join Job should be created while the previous PVC is terminating")
	})
})

var _ = Describe("ensureInstancesAreCreated reattachment while a PVC is terminating (#10985)", func() {
	var env *testingEnvironment
	var namespace string

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace = newFakeNamespace(env.client)
	})

	It("defers reattaching a Pod while one of the instance's PVCs is still terminating", func(ctx SpecContext) {
		cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
			c.Spec.WalStorage = &apiv1.StorageConfiguration{Size: "1G"}
		})
		cluster.Status.ReadyInstances = 2

		readyPod := func(serial int) *corev1.Pod {
			pod, err := specs.NewInstance(ctx, *cluster, serial, true)
			Expect(err).ToNot(HaveOccurred())
			pod.Status = corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			}
			return pod
		}
		pod1, pod2 := readyPod(1), readyPod(2)

		// Instance 3 has a podless data PVC (so it is a candidate for
		// reattachment) while its WAL PVC is still terminating.
		thirdGroup := newFakePVC(env.client, cluster, 3, persistentvolumeclaim.StatusReady)
		walName := specs.GetInstanceName(cluster.Name, 3) + apiv1.WalArchiveVolumeSuffix
		for i := range thirdGroup {
			if thirdGroup[i].Name == walName {
				thirdGroup[i].DeletionTimestamp = ptr.To(metav1.Now())
			}
		}
		cluster.Status.UnusablePVC = []string{specs.GetInstanceName(cluster.Name, 3)}

		resources := &managedResources{
			instances: corev1.PodList{Items: []corev1.Pod{*pod1, *pod2}},
			pvcs:      corev1.PersistentVolumeClaimList{Items: thirdGroup},
		}
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{Pod: pod1, IsPodReady: true, IsPrimary: true},
				{Pod: pod2, IsPodReady: true},
			},
		}

		res, err := env.clusterReconciler.ensureInstancesAreCreated(ctx, cluster, resources, statusList)
		Expect(err).To(MatchError(ErrNextLoop))
		Expect(res.RequeueAfter).To(Equal(time.Second))

		var pods corev1.PodList
		Expect(env.client.List(ctx, &pods)).To(Succeed())
		Expect(pods.Items).To(BeEmpty(), "no Pod should be reattached while one of its PVCs is terminating")
	})
})

var _ = Describe("ensureInstancesAreCreated recovers a lost first-primary bootstrap Job (#11036)", func() {
	var env *testingEnvironment
	var namespace string

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace = newFakeNamespace(env.client)
	})

	It("recreates the initdb Job reusing serial 1 when the primary PVC is stuck initializing", func(ctx SpecContext) {
		cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
			c.Spec.Instances = 1
		})

		// The first-primary bootstrap created the data PVC for serial 1 and patched
		// the cluster status (TargetPrimary set, Instances flipped to 1) but lost the
		// optimistic lock before creating the initdb Job: no Pod and no Job exist.
		primaryName := specs.GetInstanceName(cluster.Name, 1)
		cluster.Status.Instances = 1
		cluster.Status.ReadyInstances = 0
		cluster.Status.InstanceNames = []string{primaryName}
		cluster.Status.TargetPrimary = primaryName
		cluster.Status.DanglingPVC = []string{primaryName}

		pvcGroup := newFakePVC(env.client, cluster, 1, persistentvolumeclaim.StatusInitializing)

		resources := &managedResources{
			pvcs: corev1.PersistentVolumeClaimList{Items: pvcGroup},
		}
		statusList := postgres.PostgresqlStatusList{}

		res, err := env.clusterReconciler.ensureInstancesAreCreated(ctx, cluster, resources, statusList)
		Expect(err).To(MatchError(ErrNextLoop))
		Expect(res.RequeueAfter).To(Equal(time.Second))

		var jobs batchv1.JobList
		Expect(env.client.List(ctx, &jobs)).To(Succeed())
		Expect(jobs.Items).To(HaveLen(1), "the bootstrap Job for serial 1 should have been recreated")
		Expect(jobs.Items[0].Labels[utils.InstanceNameLabelName]).To(Equal(primaryName))

		// No serial-2 instance or PVC must be created: the existing serial must be reused.
		var pvcs corev1.PersistentVolumeClaimList
		Expect(env.client.List(ctx, &pvcs)).To(Succeed())
		for _, pvc := range pvcs.Items {
			serial, serr := specs.GetNodeSerial(pvc.ObjectMeta)
			Expect(serr).ToNot(HaveOccurred())
			Expect(serial).To(Equal(1), "no PVC for a different serial should have been created")
		}

		var pods corev1.PodList
		Expect(env.client.List(ctx, &pods)).To(Succeed())
		Expect(pods.Items).To(BeEmpty(), "no Pod should be created while the PVC is still initializing")
	})

	It("waits instead of recreating the init Job when one already exists", func(ctx SpecContext) {
		cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
			c.Spec.Instances = 1
		})

		primaryName := specs.GetInstanceName(cluster.Name, 1)
		cluster.Status.Instances = 1
		cluster.Status.ReadyInstances = 0
		cluster.Status.InstanceNames = []string{primaryName}
		cluster.Status.TargetPrimary = primaryName
		cluster.Status.DanglingPVC = []string{primaryName}

		// The initializing PVC already has its bootstrap Job: the recovery path
		// must defer to the regular wait, not create a second Job.
		pvcGroup := newFakePVC(env.client, cluster, 1, persistentvolumeclaim.StatusInitializing)
		existingJob := specs.CreatePrimaryJobViaInitdb(*cluster, 1)
		cluster.SetInheritedDataAndOwnership(&existingJob.ObjectMeta)

		resources := &managedResources{
			pvcs: corev1.PersistentVolumeClaimList{Items: pvcGroup},
			jobs: batchv1.JobList{Items: []batchv1.Job{*existingJob}},
		}
		statusList := postgres.PostgresqlStatusList{}

		res, err := env.clusterReconciler.ensureInstancesAreCreated(ctx, cluster, resources, statusList)
		Expect(err).To(MatchError(ErrNextLoop))
		Expect(res.RequeueAfter).To(Equal(time.Second))

		var jobs batchv1.JobList
		Expect(env.client.List(ctx, &jobs)).To(Succeed())
		Expect(jobs.Items).To(BeEmpty(), "the pre-existing Job lives only in resources, none should be created")
	})
})

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

	// DescribeTable entries assert state (requeue vs. proceed) only. They also
	// regression-lock event absence: none of these scenarios should emit an
	// event. Event-emission assertions live in separate It blocks below — that
	// is why the "transient /pg/status failure" case (which fires + emits)
	// lives there rather than here.
	DescribeTable(
		"guards behaviour",
		func(ctx SpecContext, currentPrimary, targetPrimary string, items []postgres.PostgresqlStatus, requeue bool) {
			cluster.Status.CurrentPrimary = currentPrimary
			cluster.Status.TargetPrimary = targetPrimary

			result := env.clusterReconciler.evaluatePodReadinessGuards(
				ctx, cluster, postgres.PostgresqlStatusList{Items: items},
			)

			if requeue {
				Expect(result.RequeueAfter).To(Equal(10 * time.Second))
			} else {
				Expect(result.IsZero()).To(BeTrue())
			}

			fakeRecorder, ok := env.clusterReconciler.Recorder.(*record.FakeRecorder)
			Expect(ok).To(BeTrue())
			Expect(fakeRecorder.Events).ShouldNot(Receive(),
				"DescribeTable entries must not emit events; if a new branch needs one, add a dedicated It")
		},
		Entry("happy path: primary Ready and reporting, no guard fires",
			primaryName, primaryName,
			[]postgres.PostgresqlStatus{readyReportingPrimary, readyReportingReplica}, false),
		Entry("kubelet has not refreshed the readiness probe yet",
			primaryName, primaryName,
			[]postgres.PostgresqlStatus{kubeletStaleReporting, readyReportingReplica}, true),
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

		result := env.clusterReconciler.evaluatePodReadinessGuards(ctx, cluster, statusList)
		Expect(result.RequeueAfter).To(Equal(10 * time.Second))
	})

	It("emits a Warning event when the primary is Ready but /pg/status is failing", func(ctx SpecContext) {
		cluster.Status.CurrentPrimary = primaryName
		cluster.Status.TargetPrimary = primaryName

		result := env.clusterReconciler.evaluatePodReadinessGuards(
			ctx, cluster,
			postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				readyReportingReplica, readyErroringPrimary,
			}},
		)
		Expect(result.RequeueAfter).To(Equal(10 * time.Second))

		fakeRecorder, ok := env.clusterReconciler.Recorder.(*record.FakeRecorder)
		Expect(ok).To(BeTrue(), "test environment must wire a FakeRecorder")

		var recorded string
		Eventually(fakeRecorder.Events, "1s").Should(Receive(&recorded))
		Expect(recorded).To(HavePrefix("Warning PrimaryStatusCheckFailed"))
		Expect(recorded).To(ContainSubstring(primaryName))
		Expect(recorded).To(ContainSubstring("See operator logs"))
	})

	It("does not emit an event on the kubelet-stale branch", func(ctx SpecContext) {
		cluster.Status.CurrentPrimary = primaryName
		cluster.Status.TargetPrimary = primaryName

		result := env.clusterReconciler.evaluatePodReadinessGuards(
			ctx, cluster,
			postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{
				kubeletStaleReporting, readyReportingReplica,
			}},
		)
		Expect(result.RequeueAfter).To(Equal(10 * time.Second))

		fakeRecorder, ok := env.clusterReconciler.Recorder.(*record.FakeRecorder)
		Expect(ok).To(BeTrue())

		Expect(fakeRecorder.Events).ShouldNot(Receive(),
			"kubelet-stale branch must stay event-less to avoid noise")
	})
})

var _ = Describe("getPluginsNeededForReconcile", func() {
	ptrBool := func(b bool) *bool { return &b }

	It("returns an empty slice when no plugins or external clusters are configured", func() {
		cluster := &apiv1.Cluster{}
		Expect(getPluginsNeededForReconcile(cluster)).To(BeEmpty())
	})

	It("returns the names of enabled plugins from Spec.Plugins", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{
					{Name: "plugin-a"},
					{Name: "plugin-b", Enabled: ptrBool(true)},
				},
			},
		}
		Expect(getPluginsNeededForReconcile(cluster)).
			To(ConsistOf("plugin-a", "plugin-b"))
	})

	It("skips plugins that are explicitly disabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{
					{Name: "plugin-a"},
					{Name: "plugin-b", Enabled: ptrBool(false)},
				},
			},
		}
		Expect(getPluginsNeededForReconcile(cluster)).
			To(ConsistOf("plugin-a"))
	})

	It("includes plugins referenced by external clusters", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name:                "source",
						PluginConfiguration: &apiv1.PluginConfiguration{Name: "plugin-ext"},
					},
					{Name: "no-plugin"},
				},
			},
		}
		Expect(getPluginsNeededForReconcile(cluster)).
			To(ConsistOf("plugin-ext"))
	})

	It("merges plugins from Spec.Plugins and Spec.ExternalClusters", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{
					{Name: "plugin-a"},
					{Name: "plugin-disabled", Enabled: ptrBool(false)},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name:                "source",
						PluginConfiguration: &apiv1.PluginConfiguration{Name: "plugin-ext"},
					},
				},
			},
		}
		Expect(getPluginsNeededForReconcile(cluster)).
			To(ConsistOf("plugin-a", "plugin-ext"))
	})

	It("excludes external-cluster plugins that are explicitly disabled", func() {
		// GetExternalClustersEnabledPluginNames honours
		// PluginConfiguration.Enabled, so a disabled plugin on an external
		// cluster does not contribute its name.
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "source",
						PluginConfiguration: &apiv1.PluginConfiguration{
							Name:    "plugin-ext",
							Enabled: ptrBool(false),
						},
					},
				},
			},
		}
		Expect(getPluginsNeededForReconcile(cluster)).To(BeEmpty())
	})

	It("deduplicates plugin names that appear in both Spec.Plugins and external clusters", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{
					{Name: "plugin-shared"},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name:                "source",
						PluginConfiguration: &apiv1.PluginConfiguration{Name: "plugin-shared"},
					},
				},
			},
		}
		Expect(getPluginsNeededForReconcile(cluster)).
			To(Equal([]string{"plugin-shared"}))
	})
})

var _ = Describe("reconcileResources surfaces a permanently failed instance creation job", func() {
	var env *testingEnvironment
	var namespace string

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace = newFakeNamespace(env.client)
	})

	// recoveryJob returns a recovery Job for the cluster with the given status,
	// mimicking the bootstrap/recovery Job the operator creates.
	recoveryJob := func(cluster *apiv1.Cluster, status batchv1.JobStatus) batchv1.Job {
		return batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name + "-1-full-recovery",
				Namespace: namespace,
			},
			Status: status,
		}
	}

	It("reports the unrecoverable phase when a job reaches its backoff limit",
		func(ctx SpecContext) {
			cluster := newFakeCNPGCluster(env.client, namespace)
			failedJob := recoveryJob(cluster, batchv1.JobStatus{
				Failed: 7,
				Conditions: []batchv1.JobCondition{
					{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"},
				},
			})

			resources := &managedResources{jobs: batchv1.JobList{Items: []batchv1.Job{failedJob}}}

			res, err := env.clusterReconciler.reconcileResources(ctx, cluster, resources, postgres.PostgresqlStatusList{})
			Expect(err).ToNot(HaveOccurred())
			Expect(res.RequeueAfter).To(BeNumerically(">", 0))

			var updated apiv1.Cluster
			Expect(env.client.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: namespace}, &updated)).
				To(Succeed())
			Expect(updated.Status.Phase).To(Equal(apiv1.PhaseUnrecoverable))
			Expect(updated.Status.PhaseReason).To(ContainSubstring(failedJob.Name))
		})
})

var _ = Describe("mapClusterOwnedResourceToCluster", func() {
	const (
		ns             = "ns"
		referencedName = "referenced-cluster"
	)
	expectedRequest := reconcile.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: referencedName},
	}
	clusterRef := corev1.LocalObjectReference{Name: referencedName}

	It("maps a Database to a reconcile request for the referenced cluster", func(ctx SpecContext) {
		db := &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "db"},
			Spec:       apiv1.DatabaseSpec{ClusterRef: clusterRef},
		}
		Expect(mapClusterOwnedResourceToCluster(ctx, db)).To(ConsistOf(expectedRequest))
	})

	It("maps a Publication to a reconcile request for the referenced cluster", func(ctx SpecContext) {
		pub := &apiv1.Publication{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "pub"},
			Spec:       apiv1.PublicationSpec{ClusterRef: clusterRef},
		}
		Expect(mapClusterOwnedResourceToCluster(ctx, pub)).To(ConsistOf(expectedRequest))
	})

	It("maps a Subscription to a reconcile request for the referenced cluster", func(ctx SpecContext) {
		sub := &apiv1.Subscription{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "sub"},
			Spec:       apiv1.SubscriptionSpec{ClusterRef: clusterRef},
		}
		Expect(mapClusterOwnedResourceToCluster(ctx, sub)).To(ConsistOf(expectedRequest))
	})

	It("maps a DatabaseRole to a reconcile request for the referenced cluster", func(ctx SpecContext) {
		sub := &apiv1.DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "sub"},
			Spec:       apiv1.DatabaseRoleSpec{ClusterRef: clusterRef},
		}
		Expect(mapClusterOwnedResourceToCluster(ctx, sub)).To(ConsistOf(expectedRequest))
	})

	It("returns nil when the cluster reference is empty", func(ctx SpecContext) {
		db := &apiv1.Database{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "db"}}
		Expect(mapClusterOwnedResourceToCluster(ctx, db)).To(BeNil())
	})

	It("returns nil for an object that does not reference a cluster", func(ctx SpecContext) {
		Expect(mapClusterOwnedResourceToCluster(ctx, &corev1.Secret{})).To(BeNil())
	})
})
