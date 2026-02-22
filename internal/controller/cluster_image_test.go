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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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

var _ = Describe("Cluster image detection with errors", func() {
	It("emits an event when ImageCatalog retrieval fails", func(ctx SpecContext) {
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

		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithRuntimeObjects(cluster).
			WithStatusSubresource(cluster).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object,
					opts ...client.GetOption,
				) error {
					if _, ok := obj.(*apiv1.ImageCatalog); ok {
						return fmt.Errorf("simulated error: no kind match")
					}
					return cl.Get(ctx, key, obj, opts...)
				},
			}).
			Build()

		recorder := record.NewFakeRecorder(10)
		r := &ClusterReconciler{
			Client:   fakeClient,
			Recorder: recorder,
		}

		result, err := r.reconcileImage(ctx, cluster)

		// reconcileImage handles the error by updating the status, so it returns nil error
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(&ctrl.Result{}))

		// Check if the event was recorded
		// The event string format is "Type Reason Message"
		// We expect: Warning DiscoverImage Error getting ImageCatalog/catalog: ...
		Eventually(recorder.Events).Should(Receive(ContainSubstring(
			"Warning DiscoverImage Error getting ImageCatalog/catalog")))
	})
})

var _ = Describe("Cluster with container image extensions", func() {
	var extensionsConfig []apiv1.ExtensionConfiguration
	var catalog *apiv1.ImageCatalog

	BeforeEach(func() {
		extensionsConfig = []apiv1.ExtensionConfiguration{
			{
				Name: "foo",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "foo:dev",
				},
			},
		}
		catalog = &apiv1.ImageCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "catalog",
				Namespace: "default",
			},
			Spec: apiv1.ImageCatalogSpec{
				Images: []apiv1.CatalogImage{
					{
						Image:      "postgres:15.2",
						Major:      15,
						Extensions: extensionsConfig,
					},
				},
			},
		}
	})

	It("when extensions are not defined", func(ctx SpecContext) {
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
		Expect(cluster.Status.PGDataImageInfo.Extensions).To(BeEmpty())
	})

	It("when extensions are defined in the Cluster spec", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:15.2",
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: extensionsConfig,
				},
			},
		}
		r := newFakeReconcilerFor(cluster, nil)
		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(BeNil())
		Expect(cluster.Status.PGDataImageInfo.Extensions).To(Equal(extensionsConfig))
	})

	It("when extensions are defined in the Catalog", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: []apiv1.ExtensionConfiguration{
						{
							Name: "foo",
						},
					},
				},
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
		r := newFakeReconcilerFor(cluster, catalog)
		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(BeNil())
		Expect(cluster.Status.PGDataImageInfo.Extensions).To(Equal(extensionsConfig))
	})

	It("when extensions are defined in both places", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: []apiv1.ExtensionConfiguration{
						{
							Name: "foo",
						},
						{
							Name: "bar",
							ImageVolumeSource: corev1.ImageVolumeSource{
								Reference: "bar:dev",
							},
						},
					},
				},
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
		r := newFakeReconcilerFor(cluster, catalog)
		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(BeNil())
		Expect(cluster.Status.PGDataImageInfo.Extensions[0].Name).To(Equal("foo"))
		Expect(cluster.Status.PGDataImageInfo.Extensions[0].ImageVolumeSource.Reference).To(Equal("foo:dev"))
		Expect(cluster.Status.PGDataImageInfo.Extensions[1].Name).To(Equal("bar"))
		Expect(cluster.Status.PGDataImageInfo.Extensions[1].ImageVolumeSource.Reference).To(Equal("bar:dev"))
	})

	It("when an extension defined in the catalog is overridden in the Cluster Spec", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: []apiv1.ExtensionConfiguration{
						{
							Name: "foo",
							ImageVolumeSource: corev1.ImageVolumeSource{
								Reference: "foo:testing",
							},
							ExtensionControlPath: []string{"/custom/path/"},
						},
					},
				},
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
		r := newFakeReconcilerFor(cluster, catalog)
		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(BeNil())
		Expect(cluster.Status.PGDataImageInfo.Extensions[0].Name).To(Equal("foo"))
		Expect(cluster.Status.PGDataImageInfo.Extensions[0].ImageVolumeSource.Reference).To(Equal("foo:testing"))
		Expect(cluster.Status.PGDataImageInfo.Extensions[0].ExtensionControlPath).To(Equal([]string{"/custom/path/"}))
	})

	It("when an extension is defined in the catalog but not requested", func(ctx SpecContext) {
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
		r := newFakeReconcilerFor(cluster, catalog)
		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(BeNil())
		Expect(cluster.Status.PGDataImageInfo.Extensions).To(BeEmpty())
	})

	It("when an extension is defined in the catalog and requested but it's incomplete", func(ctx SpecContext) {
		catalog.Spec.Images[0].Extensions[0].ImageVolumeSource.Reference = ""
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: []apiv1.ExtensionConfiguration{
						{
							Name: "foo",
						},
					},
				},
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
		r := newFakeReconcilerFor(cluster, catalog)
		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(Not(BeNil()))
		Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseImageCatalogError))
		Expect(cluster.Status.PhaseReason).To(Not(BeEmpty()))
	})

	It("when an extension requested doesn't exist in the catalog", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: []apiv1.ExtensionConfiguration{
						{
							Name: "bar",
						},
					},
				},
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
		r := newFakeReconcilerFor(cluster, catalog)
		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(Not(BeNil()))
		Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseImageCatalogError))
		Expect(cluster.Status.PhaseReason).To(Not(BeEmpty()))
	})

	It("when an extension gets removed from a catalog but it's still requested by a Cluster", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: []apiv1.ExtensionConfiguration{
						{
							Name: "foo",
						},
					},
				},
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
		r := newFakeReconcilerFor(cluster, catalog)
		result, err := r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(BeNil())
		Expect(cluster.Status.PGDataImageInfo.Extensions[0].Name).To(Equal("foo"))
		Expect(cluster.Status.PGDataImageInfo.Extensions[0].ImageVolumeSource.Reference).To(Equal("foo:dev"))

		// Remove the extension from the catalog
		catalog.Spec.Images[0].Extensions = nil
		r = newFakeReconcilerFor(cluster, catalog)
		result, err = r.reconcileImage(ctx, cluster)
		Expect(err).Error().ShouldNot(HaveOccurred())
		Expect(result).To(Not(BeNil()))
		Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseImageCatalogError))
		Expect(cluster.Status.PhaseReason).To(Not(BeEmpty()))
	})
})

var _ = Describe("extensionsEqual", func() {
	It("returns true for two nil slices", func() {
		Expect(extensionsEqual(nil, nil)).To(BeTrue())
	})

	It("returns true for two empty slices", func() {
		Expect(extensionsEqual(
			[]apiv1.ExtensionConfiguration{},
			[]apiv1.ExtensionConfiguration{},
		)).To(BeTrue())
	})

	It("returns true for nil and empty slice", func() {
		Expect(extensionsEqual(nil, []apiv1.ExtensionConfiguration{})).To(BeTrue())
	})

	It("returns false when lengths differ", func() {
		a := []apiv1.ExtensionConfiguration{
			{Name: "foo", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "foo:1"}},
		}
		Expect(extensionsEqual(a, nil)).To(BeFalse())
	})

	It("returns true for identical extensions in same order", func() {
		a := []apiv1.ExtensionConfiguration{
			{Name: "alpha", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "alpha:1"}},
			{Name: "beta", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "beta:1"}},
		}
		b := []apiv1.ExtensionConfiguration{
			{Name: "alpha", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "alpha:1"}},
			{Name: "beta", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "beta:1"}},
		}
		Expect(extensionsEqual(a, b)).To(BeTrue())
	})

	It("returns true for identical extensions in different order", func() {
		a := []apiv1.ExtensionConfiguration{
			{Name: "beta", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "beta:1"}},
			{Name: "alpha", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "alpha:1"}},
		}
		b := []apiv1.ExtensionConfiguration{
			{Name: "alpha", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "alpha:1"}},
			{Name: "beta", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "beta:1"}},
		}
		Expect(extensionsEqual(a, b)).To(BeTrue())
	})

	It("returns false when image references differ", func() {
		a := []apiv1.ExtensionConfiguration{
			{Name: "foo", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "foo:1"}},
		}
		b := []apiv1.ExtensionConfiguration{
			{Name: "foo", ImageVolumeSource: corev1.ImageVolumeSource{Reference: "foo:2"}},
		}
		Expect(extensionsEqual(a, b)).To(BeFalse())
	})

	It("returns false when pull policies differ", func() {
		a := []apiv1.ExtensionConfiguration{
			{Name: "foo", ImageVolumeSource: corev1.ImageVolumeSource{
				Reference:  "foo:1",
				PullPolicy: "Always",
			}},
		}
		b := []apiv1.ExtensionConfiguration{
			{Name: "foo", ImageVolumeSource: corev1.ImageVolumeSource{
				Reference:  "foo:1",
				PullPolicy: "IfNotPresent",
			}},
		}
		Expect(extensionsEqual(a, b)).To(BeFalse())
	})

	It("returns false when extension control paths differ", func() {
		a := []apiv1.ExtensionConfiguration{
			{
				Name:                 "foo",
				ImageVolumeSource:    corev1.ImageVolumeSource{Reference: "foo:1"},
				ExtensionControlPath: []string{"/share"},
			},
		}
		b := []apiv1.ExtensionConfiguration{
			{
				Name:                 "foo",
				ImageVolumeSource:    corev1.ImageVolumeSource{Reference: "foo:1"},
				ExtensionControlPath: []string{"/custom"},
			},
		}
		Expect(extensionsEqual(a, b)).To(BeFalse())
	})

	It("returns false when dynamic library paths differ", func() {
		a := []apiv1.ExtensionConfiguration{
			{
				Name:               "foo",
				ImageVolumeSource:  corev1.ImageVolumeSource{Reference: "foo:1"},
				DynamicLibraryPath: []string{"/lib"},
			},
		}
		b := []apiv1.ExtensionConfiguration{
			{
				Name:               "foo",
				ImageVolumeSource:  corev1.ImageVolumeSource{Reference: "foo:1"},
				DynamicLibraryPath: []string{"/lib", "/extra"},
			},
		}
		Expect(extensionsEqual(a, b)).To(BeFalse())
	})

	It("returns false when ld library paths differ", func() {
		a := []apiv1.ExtensionConfiguration{
			{
				Name:              "foo",
				ImageVolumeSource: corev1.ImageVolumeSource{Reference: "foo:1"},
				LdLibraryPath:     []string{"/lib"},
			},
		}
		b := []apiv1.ExtensionConfiguration{
			{
				Name:              "foo",
				ImageVolumeSource: corev1.ImageVolumeSource{Reference: "foo:1"},
			},
		}
		Expect(extensionsEqual(a, b)).To(BeFalse())
	})
})
