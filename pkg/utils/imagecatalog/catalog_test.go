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

package imagecatalog

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Get", func() {
	var (
		ctx     context.Context
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		ctx = context.Background()
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ImageCatalogRef: &apiv1.ImageCatalogRef{
					TypedLocalObjectReference: corev1.TypedLocalObjectReference{
						Name:     "my-catalog",
						Kind:     "ImageCatalog",
						APIGroup: &apiv1.SchemeGroupVersion.Group,
					},
					Major: 16,
				},
			},
		}
	})

	It("retrieves an ImageCatalog", func() {
		catalog := &apiv1.ImageCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-catalog",
				Namespace: "default",
			},
			Spec: apiv1.ImageCatalogSpec{
				Images: []apiv1.CatalogImage{
					{Image: "postgres:16", Major: 16},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(catalog).
			Build()

		result, err := Get(ctx, fakeClient, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.GetName()).To(Equal("my-catalog"))
		Expect(result.GetSpec().Images).To(HaveLen(1))
	})

	It("retrieves a ClusterImageCatalog", func() {
		cluster.Spec.ImageCatalogRef.Kind = "ClusterImageCatalog"
		catalog := &apiv1.ClusterImageCatalog{
			ObjectMeta: metav1.ObjectMeta{
				// The fake client requires the namespace to match the lookup key,
				// even for cluster-scoped resources. The real API server ignores it.
				Namespace: "default",
				Name:      "my-catalog",
			},
			Spec: apiv1.ImageCatalogSpec{
				Images: []apiv1.CatalogImage{
					{Image: "postgres:16", Major: 16},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(catalog).
			Build()

		result, err := Get(ctx, fakeClient, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.GetName()).To(Equal("my-catalog"))
	})

	It("errors on unknown catalog kind", func() {
		cluster.Spec.ImageCatalogRef.Kind = "UnknownKind"

		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			Build()

		_, err := Get(ctx, fakeClient, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid image catalog type"))
	})

	It("errors on invalid API group", func() {
		badGroup := "wrong.group.io"
		cluster.Spec.ImageCatalogRef.APIGroup = &badGroup

		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			Build()

		_, err := Get(ctx, fakeClient, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid image catalog group"))
	})

	It("errors on nil API group", func() {
		cluster.Spec.ImageCatalogRef.APIGroup = nil

		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			Build()

		_, err := Get(ctx, fakeClient, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid image catalog group"))
	})

	It("errors when catalog is not found", func() {
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			Build()

		_, err := Get(ctx, fakeClient, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not found"))
	})
})
