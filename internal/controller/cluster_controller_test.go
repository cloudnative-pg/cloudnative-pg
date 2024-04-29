/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

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

	It("selects the new target primary right away", func() {
		ctx := context.TODO()
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
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
					IsPodReady:  true,
					Pod:         &instances[1],
				},
				{
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
					IsPodReady:  true,
					Pod:         &instances[2],
				},
				{
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
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

	It("it should wait the failover delay to select the new target primary", func() {
		ctx := context.TODO()
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
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
					IsPodReady:  false,
					IsPrimary:   false,
					Pod:         &instances[0],
				},
				{
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
					IsPodReady:  false,
					IsPrimary:   true,
					Pod:         &instances[1],
				},
				{
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
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

	It("Issue #1783: ensure that the scale-down behaviour remain consistent", func() {
		ctx := context.TODO()
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
					CurrentLsn:         postgres.LSN("0/0"),
					ReceivedLsn:        postgres.LSN("0/0"),
					ReplayLsn:          postgres.LSN("0/0"),
					IsPodReady:         true,
					IsPrimary:          false,
					Pod:                &instances[0],
					MightBeUnavailable: false,
				},
				{
					CurrentLsn:         postgres.LSN("0/0"),
					ReceivedLsn:        postgres.LSN("0/0"),
					ReplayLsn:          postgres.LSN("0/0"),
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
			err := env.clusterReconciler.Client.Get(ctx, types.NamespacedName{
				Name:      instanceName,
				Namespace: cluster.Namespace,
			}, &expectedPod)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = FDescribe("evaluateShutdownCheckpointToken", func() {
	var env *testingEnvironment
	BeforeEach(func() {
		env = buildTestEnvironment()
	})
	It("can correctly set the token", func(ctx SpecContext) {
		const fakeControlData = `pg_control version number:               1002
Catalog version number:                  202201241
Database cluster state:                  in production
Database system identifier:              12345678901234567890123456789012
Latest checkpoint's TimeLineID:       3
pg_control last modified:                2024-04-30 12:00:00 UTC
Latest checkpoint location:              0/3000FF0
Prior checkpoint location:               0/2000AA0
Minimum recovery ending location:        0/3000000
Time of latest checkpoint:               2024-04-30 10:00:00 UTC
Database block size:                     8192 bytes
Latest checkpoint's REDO location:         0/3000CC0
Latest checkpoint's REDO WAL file:         000000010000000000000003
Blocks per segment of large relation:    131072
Maximum data alignment:                  8
Database disk usage:                     10240 KB
Maximum xlog ID:                         123456789
Next xlog byte position:                 0/3000010`
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace, func(cluster *apiv1.Cluster) {
			instanceName := specs.GetInstanceName(cluster.Name, 1)
			_, err := utils.AddFencedInstance(utils.FenceAllInstances, &cluster.ObjectMeta)
			Expect(err).ToNot(HaveOccurred())
			cluster.Status.TargetPrimary = instanceName
			cluster.Status.CurrentPrimary = instanceName
		})
		instances := generateFakeClusterPods(env.client, cluster, true)
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					CurrentLsn:         postgres.LSN("0/0"),
					ReceivedLsn:        postgres.LSN("0/0"),
					ReplayLsn:          postgres.LSN("0/0"),
					IsPodReady:         true,
					IsPrimary:          true,
					Pod:                &instances[0],
					MightBeUnavailable: false,
				},
			},
		}
		controlDataFunc := func(_ context.Context, _ *corev1.Pod) (string, error) {
			return fakeControlData, nil
		}
		env.clusterReconciler.InstanceClient = fakeInstanceStatusClient{getPgControlDataFromInstanceFunc: controlDataFunc}
		err := env.clusterReconciler.evaluateShutdownCheckpointToken(ctx, cluster, statusList)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Status.ShutdownCheckpointToken).ToNot(BeEmpty())
		data, err := utils.ParsePgControldataToken(cluster.Status.ShutdownCheckpointToken)
		Expect(err).ToNot(HaveOccurred())
		Expect(data.LatestCheckpointTimelineID).To(Equal("3"))
		Expect(data.TimeOfLatestCheckpoint).To(Equal("2024-04-30 10:00:00 UTC"))
		Expect(data.LatestCheckpointREDOLocation).To(Equal("0/3000CC0"))
		Expect(data.REDOWALFile).To(Equal("000000010000000000000003"))
		Expect(data.DatabaseSystemIdentifier).To(Equal("12345678901234567890123456789012"))
	})
})
