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
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func newFakeReconcilerFor(cluster *apiv1.Cluster, catalog *apiv1.ImageCatalog) *ClusterReconciler {
	fakeClient := fake.NewClientBuilder().
		WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
		WithRuntimeObjects(cluster).
		WithStatusSubresource(cluster).
		Build()

	if catalog != nil {
		_ = fakeClient.Create(context.Background(), catalog)
	}

	return &ClusterReconciler{
		Client:   fakeClient,
		Recorder: record.NewFakeRecorder(10),
	}
}

var _ = Describe("Cluster image detection", func() {
	It("gets the image from .spec.imageName", func(ctx SpecContext) {
		// This is a simple situation, having a cluster with an
		// explicit image. The image should be directly set into the
		// status and the reconciliation loop can proceed.
		// No major version upgrade have been requested.
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:15.2",
			},
		}
		r := newFakeReconcilerFor(cluster, nil)

		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(BeNil())

		Expect(cluster.Status.Image).To(Equal("postgres:15.2"))
		Expect(cluster.Status.PGDataImageInfo.Image).To(Equal("postgres:15.2"))
		Expect(cluster.Status.PGDataImageInfo.MajorVersion).To(Equal(15))
	})

	It("gets the image from an image catalog", func(ctx SpecContext) {
		// This is slightly more complex, having an image catalog reference
		// instead of an explicit image name. No major version upgrade have
		// been requested, the reconciliation loop can proceed correctly
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ImageCatalogRef: &apiv1.ImageCatalogRef{
					TypedLocalObjectReference: corev1.TypedLocalObjectReference{
						Name:     "catalog",
						Kind:     "ImageCatalog",
						APIGroup: &apiv1.SchemeGroupVersion.Group,
					},
					Major: 15,
				},
			},
		}
		catalog := &apiv1.ImageCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "catalog",
				Namespace: "default",
			},
			Spec: apiv1.ImageCatalogSpec{
				Images: []apiv1.CatalogImage{
					{
						Image: "postgres:15.2",
						Major: 15,
					},
				},
			},
		}

		r := newFakeReconcilerFor(cluster, catalog)
		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(BeNil())

		Expect(cluster.Status.Image).To(Equal("postgres:15.2"))
		Expect(cluster.Status.PGDataImageInfo.Image).To(Equal("postgres:15.2"))
		Expect(cluster.Status.PGDataImageInfo.MajorVersion).To(Equal(15))
	})

	It("gets the name from the image catalog, but the catalog is incomplete", func(ctx SpecContext) {
		// As a variant of the previous case, the catalog may be
		// incomplete and have no image for the selected major.  When
		// this happens, the reconciliation loop should be stopped and
		// the proper phase should be set.
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ImageCatalogRef: &apiv1.ImageCatalogRef{
					TypedLocalObjectReference: corev1.TypedLocalObjectReference{
						Name:     "catalog",
						Kind:     "ImageCatalog",
						APIGroup: &apiv1.SchemeGroupVersion.Group,
					},
					Major: 15,
				},
			},
		}
		catalog := &apiv1.ImageCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "catalog",
				Namespace: "default",
			},
			Spec: apiv1.ImageCatalogSpec{
				Images: []apiv1.CatalogImage{
					{
						Image: "postgres:17.4",
						Major: 17,
					},
				},
			},
		}

		r := newFakeReconcilerFor(cluster, catalog)
		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).ToNot(BeNil())

		Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseImageCatalogError))
	})

	It("skips major version downgrades", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:15.2",
			},
			Status: apiv1.ClusterStatus{
				Image: "postgres:16.2",
				PGDataImageInfo: &apiv1.ImageInfo{
					Image:        "postgres:16.2",
					MajorVersion: 16,
				},
			},
		}

		r := newFakeReconcilerFor(cluster, nil)

		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().Should(HaveOccurred())
		Expect(err).Error().Should(MatchError("cannot downgrade the PostgreSQL major version from 16 to 15"))
		Expect(result).To(BeNil())

		Expect(cluster.Status.Image).To(Equal("postgres:16.2"))
	})

	It("process major version upgrades", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:17.2",
			},
			Status: apiv1.ClusterStatus{
				Image: "postgres:16.2",
				PGDataImageInfo: &apiv1.ImageInfo{
					Image:        "postgres:16.2",
					MajorVersion: 16,
				},
			},
		}

		r := newFakeReconcilerFor(cluster, nil)

		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(BeNil())

		Expect(cluster.Status.Image).To(Equal("postgres:17.2"))
		Expect(cluster.Status.PGDataImageInfo.Image).To(Equal("postgres:16.2"))
		Expect(cluster.Status.PGDataImageInfo.MajorVersion).To(Equal(16))
	})
})
