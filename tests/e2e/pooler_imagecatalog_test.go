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

package e2e

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler ImageCatalog", Label(tests.LabelBasic), func() {
	const (
		clusterFile     = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml.template"
		namespacePrefix = "pooler-imagecatalog-e2e"
		catalogKey      = "pgbouncer"
		poolerName      = "pooler-imgcat"
		level           = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	newPooler := func(namespace, clusterName string, ref *apiv1.ImageCatalogExtraRef) *apiv1.Pooler {
		return &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      poolerName,
				Namespace: namespace,
			},
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: clusterName},
				Type:      apiv1.PoolerTypeRW,
				Instances: ptr.To(int32(1)),
				PgBouncer: &apiv1.PgBouncerSpec{
					PoolMode:        apiv1.PgBouncerPoolModeSession,
					ImageCatalogRef: ref,
				},
			},
		}
	}

	pgbouncerContainerImage := func(deployment appsv1.Deployment) string {
		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == "pgbouncer" {
				return c.Image
			}
		}
		return ""
	}

	Context("ImageCatalog", func() {
		It("resolves and updates the pgbouncer image from a namespaced catalog", func() {
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterFile)
			Expect(err).ToNot(HaveOccurred())

			By("creating a cluster for the pooler", func() {
				AssertCreateCluster(namespace, clusterName, clusterFile, env)
			})

			pgbouncerImage := versions.DefaultPgbouncerImage
			catalogName := "imgcat-ns"

			By("creating an ImageCatalog with an extra pgbouncer image", func() {
				catalog := &apiv1.ImageCatalog{
					ObjectMeta: metav1.ObjectMeta{
						Name:      catalogName,
						Namespace: namespace,
					},
					Spec: apiv1.ImageCatalogSpec{
						Images: []apiv1.CatalogImage{
							{Image: pgbouncerImage, Major: 17},
						},
						ExtraImages: []apiv1.CatalogExtraImage{
							{Key: catalogKey, Image: pgbouncerImage},
						},
					},
				}
				Expect(env.Client.Create(env.Ctx, catalog)).To(Succeed())
			})

			By("creating a pooler referencing the ImageCatalog", func() {
				pooler := newPooler(namespace, clusterName, &apiv1.ImageCatalogExtraRef{
					TypedLocalObjectReference: corev1.TypedLocalObjectReference{
						APIGroup: &apiv1.SchemeGroupVersion.Group,
						Kind:     apiv1.ImageCatalogKind,
						Name:     catalogName,
					},
					Key: catalogKey,
				})
				Expect(env.Client.Create(env.Ctx, pooler)).To(Succeed())
			})

			By("verifying the pooler status reflects the catalog image", func() {
				Eventually(func(g Gomega) {
					var pooler apiv1.Pooler
					g.Expect(env.Client.Get(env.Ctx,
						client.ObjectKey{Namespace: namespace, Name: poolerName},
						&pooler)).To(Succeed())
					g.Expect(pooler.Status.Image).To(Equal(pgbouncerImage))
					g.Expect(pooler.Status.Phase).To(Equal(apiv1.PoolerPhaseActive))
				}, 60).Should(Succeed())
			})

			By("verifying the deployment uses the catalog image", func() {
				Eventually(func(g Gomega) {
					var deployment appsv1.Deployment
					g.Expect(env.Client.Get(env.Ctx,
						client.ObjectKey{Namespace: namespace, Name: poolerName},
						&deployment)).To(Succeed())
					g.Expect(pgbouncerContainerImage(deployment)).To(Equal(pgbouncerImage))
				}, 60).Should(Succeed())
			})

			updatedImage := pgbouncerImage + "-updated"

			By("updating the catalog extra image", func() {
				var catalog apiv1.ImageCatalog
				Expect(env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: catalogName},
					&catalog)).To(Succeed())
				catalog.Spec.ExtraImages[0].Image = updatedImage
				Expect(env.Client.Update(env.Ctx, &catalog)).To(Succeed())
			})

			By("verifying the pooler status image is updated after the catalog change", func() {
				Eventually(func(g Gomega) {
					var pooler apiv1.Pooler
					g.Expect(env.Client.Get(env.Ctx,
						client.ObjectKey{Namespace: namespace, Name: poolerName},
						&pooler)).To(Succeed())
					g.Expect(pooler.Status.Image).To(Equal(updatedImage))
				}, 60).Should(Succeed())
			})

			By("verifying the deployment spec is updated after the catalog change", func() {
				Eventually(func(g Gomega) {
					var deployment appsv1.Deployment
					g.Expect(env.Client.Get(env.Ctx,
						client.ObjectKey{Namespace: namespace, Name: poolerName},
						&deployment)).To(Succeed())
					g.Expect(pgbouncerContainerImage(deployment)).To(Equal(updatedImage))
				}, 60).Should(Succeed())
			})
		})
	})

	Context("ClusterImageCatalog", Serial, func() {
		It("resolves the pgbouncer image from a cluster-scoped catalog", func() {
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterFile)
			Expect(err).ToNot(HaveOccurred())

			By("creating a cluster for the pooler", func() {
				AssertCreateCluster(namespace, clusterName, clusterFile, env)
			})

			pgbouncerImage := versions.DefaultPgbouncerImage
			clusterCatalogName := "imgcat-cluster-" + namespace

			By("creating a ClusterImageCatalog with an extra pgbouncer image", func() {
				catalog := &apiv1.ClusterImageCatalog{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterCatalogName,
					},
					Spec: apiv1.ImageCatalogSpec{
						Images: []apiv1.CatalogImage{
							{Image: pgbouncerImage, Major: 17},
						},
						ExtraImages: []apiv1.CatalogExtraImage{
							{Key: catalogKey, Image: pgbouncerImage},
						},
					},
				}
				Expect(env.Client.Create(env.Ctx, catalog)).To(Succeed())
				DeferCleanup(func() error {
					return client.IgnoreNotFound(env.Client.Delete(env.Ctx, catalog))
				})
			})

			By("creating a pooler referencing the ClusterImageCatalog", func() {
				pooler := newPooler(namespace, clusterName, &apiv1.ImageCatalogExtraRef{
					TypedLocalObjectReference: corev1.TypedLocalObjectReference{
						APIGroup: &apiv1.SchemeGroupVersion.Group,
						Kind:     apiv1.ClusterImageCatalogKind,
						Name:     clusterCatalogName,
					},
					Key: catalogKey,
				})
				Expect(env.Client.Create(env.Ctx, pooler)).To(Succeed())
			})

			By("verifying the pooler status reflects the cluster catalog image", func() {
				Eventually(func(g Gomega) {
					var pooler apiv1.Pooler
					g.Expect(env.Client.Get(env.Ctx,
						client.ObjectKey{Namespace: namespace, Name: poolerName},
						&pooler)).To(Succeed())
					g.Expect(pooler.Status.Image).To(Equal(pgbouncerImage))
					g.Expect(pooler.Status.Phase).To(Equal(apiv1.PoolerPhaseActive))
				}, 60).Should(Succeed())
			})

			By("verifying the deployment uses the cluster catalog image", func() {
				Eventually(func(g Gomega) {
					var deployment appsv1.Deployment
					g.Expect(env.Client.Get(env.Ctx,
						client.ObjectKey{Namespace: namespace, Name: poolerName},
						&deployment)).To(Succeed())
					g.Expect(pgbouncerContainerImage(deployment)).To(Equal(pgbouncerImage))
				}, 60).Should(Succeed())
			})
		})
	})

	Context("error handling", func() {
		It("sets phase to failed when the catalog key does not exist", func() {
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterFile)
			Expect(err).ToNot(HaveOccurred())

			By("creating a cluster for the pooler", func() {
				AssertCreateCluster(namespace, clusterName, clusterFile, env)
			})

			pgbouncerImage := versions.DefaultPgbouncerImage
			catalogName := "imgcat-missing-key"

			By("creating an ImageCatalog without the expected key", func() {
				catalog := &apiv1.ImageCatalog{
					ObjectMeta: metav1.ObjectMeta{
						Name:      catalogName,
						Namespace: namespace,
					},
					Spec: apiv1.ImageCatalogSpec{
						Images: []apiv1.CatalogImage{
							{Image: pgbouncerImage, Major: 17},
						},
					},
				}
				Expect(env.Client.Create(env.Ctx, catalog)).To(Succeed())
			})

			By("creating a pooler referencing a non-existent key in the catalog", func() {
				pooler := newPooler(namespace, clusterName, &apiv1.ImageCatalogExtraRef{
					TypedLocalObjectReference: corev1.TypedLocalObjectReference{
						APIGroup: &apiv1.SchemeGroupVersion.Group,
						Kind:     apiv1.ImageCatalogKind,
						Name:     catalogName,
					},
					Key: "nonexistent-key",
				})
				Expect(env.Client.Create(env.Ctx, pooler)).To(Succeed())
			})

			By("verifying the pooler phase is set to failed", func() {
				Eventually(func(g Gomega) {
					var pooler apiv1.Pooler
					g.Expect(env.Client.Get(env.Ctx,
						client.ObjectKey{Namespace: namespace, Name: poolerName},
						&pooler)).To(Succeed())
					g.Expect(pooler.Status.Phase).To(Equal(apiv1.PoolerPhaseFailed))
				}, 60).Should(Succeed())
			})

			By("verifying the deployment was not created while the catalog key was missing", func() {
				deployment := &appsv1.Deployment{}
				err := env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: poolerName},
					deployment)
				Expect(err).To(HaveOccurred())
			})

			By("adding the missing key to the catalog", func() {
				var catalog apiv1.ImageCatalog
				Expect(env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: catalogName},
					&catalog)).To(Succeed())
				catalog.Spec.ExtraImages = []apiv1.CatalogExtraImage{
					{Key: "nonexistent-key", Image: pgbouncerImage},
				}
				Expect(env.Client.Update(env.Ctx, &catalog)).To(Succeed())
			})

			By("verifying the pooler recovers to active and the deployment is created", func() {
				Eventually(func(g Gomega) {
					var pooler apiv1.Pooler
					g.Expect(env.Client.Get(env.Ctx,
						client.ObjectKey{Namespace: namespace, Name: poolerName},
						&pooler)).To(Succeed())
					g.Expect(pooler.Status.Phase).To(Equal(apiv1.PoolerPhaseActive))
					g.Expect(pooler.Status.Image).To(Equal(pgbouncerImage))

					var deployment appsv1.Deployment
					g.Expect(env.Client.Get(env.Ctx,
						client.ObjectKey{Namespace: namespace, Name: poolerName},
						&deployment)).To(Succeed())
					g.Expect(pgbouncerContainerImage(deployment)).To(Equal(pgbouncerImage))
				}, 90).Should(Succeed())
			})
		})
	})
})
