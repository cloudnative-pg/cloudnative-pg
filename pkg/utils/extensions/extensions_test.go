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

package extensions

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ResolveFromCatalog", func() {
	var (
		cluster *apiv1.Cluster
		catalog *apiv1.ImageCatalog
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
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
						Image: "postgres:16",
						Major: 16,
						Extensions: []apiv1.ExtensionConfiguration{
							{
								Name: "postgis",
								ImageVolumeSource: corev1.ImageVolumeSource{
									Reference: "postgis:3.4",
								},
								LdLibraryPath: []string{"/system"},
							},
						},
					},
				},
			},
		}
	})

	It("resolves extensions from catalog when cluster requests by name only", func() {
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{Name: "postgis"},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts).To(HaveLen(1))
		Expect(exts[0].Name).To(Equal("postgis"))
		Expect(exts[0].ImageVolumeSource.Reference).To(Equal("postgis:3.4"))
		Expect(exts[0].LdLibraryPath).To(Equal([]string{"/system"}))
	})

	It("allows cluster spec to override the image reference", func() {
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{
				Name: "postgis",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "postgis:custom",
				},
			},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts[0].ImageVolumeSource.Reference).To(Equal("postgis:custom"))
		// Catalog defaults should still be preserved for non-overridden fields
		Expect(exts[0].LdLibraryPath).To(Equal([]string{"/system"}))
	})

	It("allows cluster spec to override PullPolicy", func() {
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{
				Name: "postgis",
				ImageVolumeSource: corev1.ImageVolumeSource{
					PullPolicy: corev1.PullAlways,
				},
			},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts[0].ImageVolumeSource.PullPolicy).To(Equal(corev1.PullAlways))
		Expect(exts[0].ImageVolumeSource.Reference).To(Equal("postgis:3.4"))
	})

	It("allows cluster spec to override ExtensionControlPath", func() {
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{
				Name:                 "postgis",
				ExtensionControlPath: []string{"/custom/share"},
			},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts[0].ExtensionControlPath).To(Equal([]string{"/custom/share"}))
	})

	It("allows cluster spec to override DynamicLibraryPath", func() {
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{
				Name:               "postgis",
				DynamicLibraryPath: []string{"/custom/lib"},
			},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts[0].DynamicLibraryPath).To(Equal([]string{"/custom/lib"}))
	})

	It("allows cluster spec to override LdLibraryPath", func() {
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{
				Name:          "postgis",
				LdLibraryPath: []string{"/custom/ldpath"},
			},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts[0].LdLibraryPath).To(Equal([]string{"/custom/ldpath"}))
	})

	It("passes through extensions not in catalog when they have a reference", func() {
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{
				Name: "pgvector",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "pgvector:0.8",
				},
			},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts).To(HaveLen(1))
		Expect(exts[0].Name).To(Equal("pgvector"))
		Expect(exts[0].ImageVolumeSource.Reference).To(Equal("pgvector:0.8"))
	})

	It("returns empty when no extensions are requested", func() {
		cluster.Spec.PostgresConfiguration.Extensions = nil

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts).To(BeEmpty())
	})

	It("errors when extension not in catalog has no reference", func() {
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{Name: "pgvector"},
		}

		_, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("pgvector"))
		Expect(err.Error()).To(ContainSubstring("ImageVolumeSource.Reference"))
	})

	It("errors when catalog extension and cluster spec both have empty reference", func() {
		catalog.Spec.Images[0].Extensions[0].ImageVolumeSource.Reference = ""
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{Name: "postgis"},
		}

		_, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("postgis"))
		Expect(err.Error()).To(ContainSubstring("ImageCatalog/catalog"))
	})

	It("handles mixed extensions from catalog and cluster spec", func() {
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{Name: "postgis"},
			{
				Name: "pgvector",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "pgvector:0.8",
				},
			},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts).To(HaveLen(2))
		Expect(exts[0].Name).To(Equal("postgis"))
		Expect(exts[0].ImageVolumeSource.Reference).To(Equal("postgis:3.4"))
		Expect(exts[1].Name).To(Equal("pgvector"))
		Expect(exts[1].ImageVolumeSource.Reference).To(Equal("pgvector:0.8"))
	})

	It("uses empty catalog extensions when major version has no extensions", func() {
		catalog.Spec.Images = append(catalog.Spec.Images, apiv1.CatalogImage{
			Image: "postgres:15",
			Major: 15,
		})
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{
				Name: "pgvector",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "pgvector:0.8",
				},
			},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 15)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts).To(HaveLen(1))
		Expect(exts[0].Name).To(Equal("pgvector"))
	})
})

var _ = Describe("ValidateWithoutCatalog", func() {
	It("returns extensions when all have references", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: []apiv1.ExtensionConfiguration{
						{
							Name: "postgis",
							ImageVolumeSource: corev1.ImageVolumeSource{
								Reference: "postgis:3.4",
							},
						},
						{
							Name: "pgvector",
							ImageVolumeSource: corev1.ImageVolumeSource{
								Reference: "pgvector:0.8",
							},
						},
					},
				},
			},
		}

		exts, err := ValidateWithoutCatalog(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts).To(HaveLen(2))
	})

	It("returns empty slice when no extensions are defined", func() {
		cluster := &apiv1.Cluster{}

		exts, err := ValidateWithoutCatalog(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts).To(BeEmpty())
	})

	It("errors when an extension has no reference", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: []apiv1.ExtensionConfiguration{
						{Name: "postgis"},
					},
				},
			},
		}

		_, err := ValidateWithoutCatalog(cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("postgis"))
	})

	It("errors on the first extension missing a reference", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: []apiv1.ExtensionConfiguration{
						{
							Name: "pgvector",
							ImageVolumeSource: corev1.ImageVolumeSource{
								Reference: "pgvector:0.8",
							},
						},
						{Name: "postgis"},
					},
				},
			},
		}

		_, err := ValidateWithoutCatalog(cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("postgis"))
	})
})
