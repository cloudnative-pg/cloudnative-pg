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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("resolvePoolerImage", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
		configuration.Current = configuration.NewConfiguration()
		configuration.Current.PgbouncerImageName = "operator-default:1"
	})

	AfterEach(func() {
		configuration.Current = configuration.NewConfiguration()
	})

	newPooler := func() *apiv1.Pooler {
		return &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"},
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{},
			},
		}
	}

	It("falls back to the operator default when nothing is configured", func() {
		image, err := env.poolerReconciler.resolvePoolerImage(context.Background(), newPooler())
		Expect(err).ToNot(HaveOccurred())
		Expect(image).To(Equal("operator-default:1"))
	})

	It("uses spec.pgbouncer.image over the operator default", func() {
		pooler := newPooler()
		pooler.Spec.PgBouncer.Image = "explicit:9"

		image, err := env.poolerReconciler.resolvePoolerImage(context.Background(), pooler)
		Expect(err).ToNot(HaveOccurred())
		Expect(image).To(Equal("explicit:9"))
	})

	It("lets the pod template override spec.pgbouncer.image", func() {
		pooler := newPooler()
		pooler.Spec.PgBouncer.Image = "explicit:9"
		pooler.Spec.Template = &apiv1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "pgbouncer", Image: "from-template:42"},
				},
			},
		}

		image, err := env.poolerReconciler.resolvePoolerImage(context.Background(), pooler)
		Expect(err).ToNot(HaveOccurred())
		Expect(image).To(Equal("from-template:42"))
	})

	It("ignores pod-template containers that are not pgbouncer", func() {
		pooler := newPooler()
		pooler.Spec.Template = &apiv1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "sidecar", Image: "sidecar:1"},
				},
			},
		}

		image, err := env.poolerReconciler.resolvePoolerImage(context.Background(), pooler)
		Expect(err).ToNot(HaveOccurred())
		Expect(image).To(Equal("operator-default:1"))
	})

	It("ignores a pgbouncer container in the template that has no image", func() {
		pooler := newPooler()
		pooler.Spec.PgBouncer.Image = "explicit:9"
		pooler.Spec.Template = &apiv1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "pgbouncer"},
				},
			},
		}

		image, err := env.poolerReconciler.resolvePoolerImage(context.Background(), pooler)
		Expect(err).ToNot(HaveOccurred())
		Expect(image).To(Equal("explicit:9"))
	})

	Context("with imageCatalogRef", func() {
		const (
			imageCatalogName        = "pgbouncer-catalog"
			clusterImageCatalogName = "global-pgbouncer-catalog"
			catalogKey              = "pgbouncer"
			resolvedImage           = "ghcr.io/example/pgbouncer:1.2.3"
		)

		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		newRef := func(kind, name string) *apiv1.ImageCatalogExtraRef {
			return &apiv1.ImageCatalogExtraRef{
				TypedLocalObjectReference: corev1.TypedLocalObjectReference{
					Kind: kind,
					Name: name,
				},
				Key: catalogKey,
			}
		}

		It("resolves through a namespaced ImageCatalog", func() {
			Expect(env.client.Create(ctx, &apiv1.ImageCatalog{
				ObjectMeta: metav1.ObjectMeta{Name: imageCatalogName, Namespace: "ns"},
				Spec: apiv1.ImageCatalogSpec{
					ExtraImages: []apiv1.CatalogExtraImage{{Key: catalogKey, Image: resolvedImage}},
				},
			})).To(Succeed())

			pooler := newPooler()
			pooler.Spec.PgBouncer.ImageCatalogRef = newRef(apiv1.ImageCatalogKind, imageCatalogName)

			image, err := env.poolerReconciler.resolvePoolerImage(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(image).To(Equal(resolvedImage))
		})

		It("resolves through a cluster-scoped ClusterImageCatalog", func() {
			Expect(env.client.Create(ctx, &apiv1.ClusterImageCatalog{
				ObjectMeta: metav1.ObjectMeta{Name: clusterImageCatalogName},
				Spec: apiv1.ImageCatalogSpec{
					ExtraImages: []apiv1.CatalogExtraImage{{Key: catalogKey, Image: resolvedImage}},
				},
			})).To(Succeed())

			pooler := newPooler()
			pooler.Spec.PgBouncer.ImageCatalogRef = newRef(apiv1.ClusterImageCatalogKind, clusterImageCatalogName)

			image, err := env.poolerReconciler.resolvePoolerImage(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(image).To(Equal(resolvedImage))
		})

		It("returns an error when the catalog cannot be found", func() {
			pooler := newPooler()
			pooler.Spec.PgBouncer.ImageCatalogRef = newRef(apiv1.ImageCatalogKind, "missing")

			_, err := env.poolerReconciler.resolvePoolerImage(ctx, pooler)
			Expect(err).To(MatchError(ContainSubstring(`ImageCatalog "missing" not found`)))
		})

		It("returns an error when the key is missing in the catalog", func() {
			Expect(env.client.Create(ctx, &apiv1.ImageCatalog{
				ObjectMeta: metav1.ObjectMeta{Name: imageCatalogName, Namespace: "ns"},
				Spec: apiv1.ImageCatalogSpec{
					ExtraImages: []apiv1.CatalogExtraImage{{Key: "other-key", Image: resolvedImage}},
				},
			})).To(Succeed())

			pooler := newPooler()
			pooler.Spec.PgBouncer.ImageCatalogRef = newRef(apiv1.ImageCatalogKind, imageCatalogName)

			_, err := env.poolerReconciler.resolvePoolerImage(ctx, pooler)
			Expect(err).To(MatchError(ContainSubstring(`key "pgbouncer" not found`)))
		})

		It("returns an error for an unknown catalog Kind", func() {
			pooler := newPooler()
			pooler.Spec.PgBouncer.ImageCatalogRef = newRef("WeirdKind", imageCatalogName)

			_, err := env.poolerReconciler.resolvePoolerImage(ctx, pooler)
			Expect(err).To(MatchError(ContainSubstring("invalid image catalog kind")))
		})
	})
})
