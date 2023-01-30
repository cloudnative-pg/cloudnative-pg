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

package controllers

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

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
	It("selects the new target primary right away", func() {
		withManager(func(ctx context.Context, crReconciler *ClusterReconciler, poolerReconciler *PoolerReconciler,
			manager manager.Manager,
		) {
			var err error

			namespace := newFakeNamespace()
			cluster := newFakeCNPGCluster(namespace)

			By("creating the cluster resources", func() {
				generateFakeInitDBJobs(crReconciler.Client, cluster)
				generateFakeClusterPods(crReconciler.Client, cluster, true)
				generateFakePVC(crReconciler.Client, cluster)

				assertRefreshManagerCache(ctx, manager)
			})

			var managedResources *managedResources
			var statusList postgres.PostgresqlStatusList
			By("creating the status list from the cluster pods", func() {
				managedResources, statusList, err = getManagedResourcesAndStatusList(ctx, crReconciler, cluster)
				Expect(err).To(BeNil())

				cluster.Status.TargetPrimary = managedResources.instances.Items[0].Name
			})

			By("updating target primary pods for the cluster", func() {
				selectedPrimary, err := crReconciler.updateTargetPrimaryFromPods(
					ctx,
					cluster,
					statusList,
					managedResources,
				)

				Expect(err).To(BeNil())
				Expect(selectedPrimary).To(Equal(statusList.Items[0].Pod.Name))
			})
		})
	})

	It("it should wait the failover delay to select the new target primary", func() {
		withManager(func(ctx context.Context, crReconciler *ClusterReconciler, poolerReconciler *PoolerReconciler,
			manager manager.Manager,
		) {
			var err error
			namespace := newFakeNamespace()
			cluster := newFakeCNPGCluster(namespace)

			By("setting failover delay to 1 sec on the cluster", func() {
				cluster.Spec.FailoverDelay = 2
				err := crReconciler.Client.Update(ctx, cluster)
				Expect(err).To(BeNil())
				assertRefreshManagerCache(ctx, manager)
			})

			By("creating the cluster resources", func() {
				generateFakeInitDBJobs(crReconciler.Client, cluster)
				generateFakeClusterPods(crReconciler.Client, cluster, true)
				generateFakePVC(crReconciler.Client, cluster)

				assertRefreshManagerCache(ctx, manager)
			})

			var managedResources *managedResources
			var statusList postgres.PostgresqlStatusList

			By("creating the status list from the cluster pods", func() {
				managedResources, statusList, err = getManagedResourcesAndStatusList(ctx, crReconciler, cluster)
				Expect(err).To(BeNil())

				cluster.Status.TargetPrimary = managedResources.instances.Items[0].Name
			})

			By("updating target primary pods for the cluster three times with a 1 second interval", func() {
				selectedPrimary, err := crReconciler.updateTargetPrimaryFromPods(
					ctx,
					cluster,
					statusList,
					managedResources,
				)

				Expect(err).NotTo(BeNil())
				Expect(err).To(Equal(ErrWaitingOnFailOverDelay))
				Expect(selectedPrimary).To(Equal(""))

				time.Sleep(time.Second)

				selectedPrimary, err = crReconciler.updateTargetPrimaryFromPods(
					ctx,
					cluster,
					statusList,
					managedResources,
				)

				Expect(err).NotTo(BeNil())
				Expect(err).To(Equal(ErrWaitingOnFailOverDelay))
				Expect(selectedPrimary).To(Equal(""))

				time.Sleep(time.Second)

				selectedPrimary, err = crReconciler.updateTargetPrimaryFromPods(
					ctx,
					cluster,
					statusList,
					managedResources,
				)

				Expect(err).To(BeNil())
				Expect(selectedPrimary).To(Equal(statusList.Items[0].Pod.Name))
			})
		})
	})
})

func getManagedResourcesAndStatusList(
	ctx context.Context,
	crReconciler *ClusterReconciler,
	cluster *apiv1.Cluster,
) (*managedResources, postgres.PostgresqlStatusList, error) {
	managedResources, err := crReconciler.getManagedResources(ctx, cluster)
	if err != nil {
		return nil, postgres.PostgresqlStatusList{}, err
	}

	statusList := postgres.PostgresqlStatusList{
		Items: []postgres.PostgresqlStatus{
			{
				CurrentLsn:  postgres.LSN("0/0"),
				ReceivedLsn: postgres.LSN("0/0"),
				ReplayLsn:   postgres.LSN("0/0"),
				IsPodReady:  true,
				Pod:         managedResources.instances.Items[1],
			},
			{
				CurrentLsn:  postgres.LSN("0/0"),
				ReceivedLsn: postgres.LSN("0/0"),
				ReplayLsn:   postgres.LSN("0/0"),
				IsPodReady:  true,
				Pod:         managedResources.instances.Items[2],
			},
			{
				CurrentLsn:  postgres.LSN("0/0"),
				ReceivedLsn: postgres.LSN("0/0"),
				ReplayLsn:   postgres.LSN("0/0"),
				IsPodReady:  false,
				Pod:         managedResources.instances.Items[0],
			},
		},
	}

	return managedResources, statusList, err
}
