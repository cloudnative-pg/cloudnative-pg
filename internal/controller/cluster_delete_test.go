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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ensures that deleteDanglingMonitoringQueries works correctly", func() {
	const cmName = apiv1.DefaultMonitoringConfigMapName
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
		configuration.Current = configuration.NewConfiguration()
		configuration.Current.MonitoringQueriesConfigmap = cmName
	})

	It("should make sure that a dangling monitoring queries config map is deleted", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		crReconciler := &ClusterReconciler{
			Client: fakeClientWithIndexAdapter{
				Client:          env.clusterReconciler.Client,
				indexerAdapters: []indexAdapter{clusterDefaultQueriesFalsePathIndexAdapter},
			},
			Scheme:          env.clusterReconciler.Scheme,
			Recorder:        env.clusterReconciler.Recorder,
			DiscoveryClient: env.clusterReconciler.DiscoveryClient,
			InstanceClient:  env.clusterReconciler.InstanceClient,
		}

		By("creating the required monitoring configmap", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: namespace,
				},
				BinaryData: map[string][]byte{},
			}
			err := crReconciler.Create(ctx, cm)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure configmap exists", func() {
			cm := &corev1.ConfigMap{}
			expectResourceExists(env.client, cmName, namespace, cm)
		})

		By("deleting the dangling monitoring configmap", func() {
			err := crReconciler.deleteDanglingMonitoringQueries(ctx, namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure it doesn't exist anymore", func() {
			expectResourceDoesntExist(env.client, cmName, namespace, &corev1.ConfigMap{})
		})
	})

	It("should make sure that the configmap is not deleted if a cluster is running", func() {
		ctx := context.Background()
		crReconciler := &ClusterReconciler{
			Client: fakeClientWithIndexAdapter{
				Client:          env.clusterReconciler.Client,
				indexerAdapters: []indexAdapter{clusterDefaultQueriesFalsePathIndexAdapter},
			},
			Scheme:          env.clusterReconciler.Scheme,
			Recorder:        env.clusterReconciler.Recorder,
			DiscoveryClient: env.clusterReconciler.DiscoveryClient,
			InstanceClient:  env.clusterReconciler.InstanceClient,
		}
		namespace := newFakeNamespace(env.client)
		var cluster *apiv1.Cluster

		By("creating the required monitoring configmap", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: namespace,
				},
				BinaryData: map[string][]byte{},
			}
			err := crReconciler.Create(ctx, cm)
			Expect(err).ToNot(HaveOccurred())
		})

		By("creating the required resources", func() {
			cluster = newFakeCNPGCluster(env.client, namespace)
			cluster.Spec.Monitoring = &apiv1.MonitoringConfiguration{
				DisableDefaultQueries: ptr.To(false),
			}
			err := crReconciler.Update(context.Background(), cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the configmap and the cluster exists", func() {
			expectResourceExists(crReconciler.Client, cmName, namespace, &corev1.ConfigMap{})
			expectResourceExists(crReconciler.Client, cluster.Name, namespace, &apiv1.Cluster{})
		})

		By("deleting the dangling monitoring configmap", func() {
			err := crReconciler.deleteDanglingMonitoringQueries(ctx, namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure it still exists", func() {
			expectResourceExists(env.client, cmName, namespace, &corev1.ConfigMap{})
		})
	})
})
