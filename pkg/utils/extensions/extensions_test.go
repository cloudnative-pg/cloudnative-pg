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
	"github.com/cloudnative-pg/machinery/pkg/envmap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

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

	It("allows cluster spec to override BinPath", func() {
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{
				Name:    "postgis",
				BinPath: []string{"/custom/bin"},
			},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts[0].BinPath).To(Equal([]string{"/custom/bin"}))
	})

	It("allows cluster spec to override Env", func() {
		envVars := []apiv1.ExtensionEnvVar{
			{
				Name:  "FOO",
				Value: "bar",
			},
		}
		cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{
			{
				Name: "postgis",
				Env:  envVars,
			},
		}

		exts, err := ResolveFromCatalog(cluster, catalog, 16)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts[0].Env).To(Equal(envVars))
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

var _ = Describe("SetEnvVars", func() {
	var envMap envmap.EnvironmentMap
	BeforeEach(func() {
		envMap = envmap.EnvironmentMap{
			"BAR_ENV": "bar_value",
			"BAZ_ENV": "baz_value",
		}
	})

	It("should add the extension's env variables", func() {
		extensionsConfig := []apiv1.ExtensionConfiguration{
			{
				Name: "foo",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "foo:dev",
				},
				Env: []apiv1.ExtensionEnvVar{
					{
						Name:  "FOO_ENV",
						Value: "foo_value",
					},
				},
			},
		}

		SetEnvVars(extensionsConfig, envMap, postgres.ExtensionsBaseDirectory)
		Expect(envMap).To(HaveKeyWithValue("BAR_ENV", "bar_value"))
		Expect(envMap).To(HaveKeyWithValue("BAZ_ENV", "baz_value"))
		Expect(envMap).To(HaveKeyWithValue("FOO_ENV", "foo_value"))
	})

	It("should expand placeholders", func() {
		extensionsConfig := []apiv1.ExtensionConfiguration{
			{
				Name: "foo",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "foo:dev",
				},
				Env: []apiv1.ExtensionEnvVar{
					{
						Name:  "FOO_ENV",
						Value: "${image_root}/foo_value",
					},
				},
			},
		}

		SetEnvVars(extensionsConfig, envMap, postgres.ExtensionsBaseDirectory)
		Expect(envMap).To(HaveKeyWithValue("BAR_ENV", "bar_value"))
		Expect(envMap).To(HaveKeyWithValue("BAZ_ENV", "baz_value"))
		Expect(envMap).To(HaveKeyWithValue("FOO_ENV", "/extensions/foo/foo_value"))
	})

	It("should unescape $${...} to literal ${...}", func() {
		extensionsConfig := []apiv1.ExtensionConfiguration{
			{
				Name: "foo",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "foo:dev",
				},
				Env: []apiv1.ExtensionEnvVar{
					{
						Name:  "ESCAPED",
						Value: "$${not_expanded}",
					},
					{
						Name:  "MIXED",
						Value: "${image_root}/$${literal}",
					},
				},
			},
		}

		SetEnvVars(extensionsConfig, envMap, postgres.ExtensionsBaseDirectory)
		Expect(envMap).To(HaveKeyWithValue("ESCAPED", "${not_expanded}"))
		Expect(envMap).To(HaveKeyWithValue("MIXED", "/extensions/foo/${literal}"))
	})

	It("should skip reserved environment variables", func() {
		envMap["PATH"] = "/usr/bin"
		envMap["LD_LIBRARY_PATH"] = "/usr/lib"

		extensionsConfig := []apiv1.ExtensionConfiguration{
			{
				Name: "foo",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "foo:dev",
				},
				Env: []apiv1.ExtensionEnvVar{
					{Name: "PATH", Value: "/evil/path"},
					{Name: "LD_LIBRARY_PATH", Value: "/evil/lib"},
					{Name: "PGDATA", Value: "/evil/data"},
					{Name: "CNPG_SECRET", Value: "stolen"},
					{Name: "POD_NAME", Value: "fake"},
					{Name: "NAMESPACE", Value: "fake"},
					{Name: "CLUSTER_NAME", Value: "fake"},
					{Name: "SAFE_VAR", Value: "allowed"},
				},
			},
		}

		SetEnvVars(extensionsConfig, envMap, postgres.ExtensionsBaseDirectory)
		Expect(envMap).To(HaveKeyWithValue("PATH", "/usr/bin"))
		Expect(envMap).To(HaveKeyWithValue("LD_LIBRARY_PATH", "/usr/lib"))
		Expect(envMap).NotTo(HaveKey("PGDATA"))
		Expect(envMap).NotTo(HaveKey("CNPG_SECRET"))
		Expect(envMap).NotTo(HaveKey("POD_NAME"))
		Expect(envMap).NotTo(HaveKey("NAMESPACE"))
		Expect(envMap).NotTo(HaveKey("CLUSTER_NAME"))
		Expect(envMap).To(HaveKeyWithValue("SAFE_VAR", "allowed"))
	})

	It("should let the last extension win when multiple define the same variable", func() {
		extensionsConfig := []apiv1.ExtensionConfiguration{
			{
				Name: "ext1",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "ext1:dev",
				},
				Env: []apiv1.ExtensionEnvVar{
					{Name: "SHARED", Value: "from_ext1"},
					{Name: "ONLY_EXT1", Value: "ext1_value"},
				},
			},
			{
				Name: "ext2",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "ext2:dev",
				},
				Env: []apiv1.ExtensionEnvVar{
					{Name: "SHARED", Value: "from_ext2"},
					{Name: "ONLY_EXT2", Value: "ext2_value"},
				},
			},
		}

		SetEnvVars(extensionsConfig, envMap, postgres.ExtensionsBaseDirectory)
		Expect(envMap).To(HaveKeyWithValue("SHARED", "from_ext2"))
		Expect(envMap).To(HaveKeyWithValue("ONLY_EXT1", "ext1_value"))
		Expect(envMap).To(HaveKeyWithValue("ONLY_EXT2", "ext2_value"))
	})

	// During a major upgrade the operator calls SetEnvVars twice: once for the
	// source-version extensions mounted under ExtensionsBaseDirectory and once
	// for the target-version copies under UpgradeTargetExtensionsBaseDirectory.
	// The merge across the two calls is what the upgrade pod actually applies
	// to its process environment, so it is worth pinning here even though
	// "last writer wins" is the same rule SetEnvVars uses within a single call.
	Context("when applied across source and target extension sets (major upgrade)", func() {
		It("expands ${image_root} against the call-specific baseDir", func() {
			oldExt := []apiv1.ExtensionConfiguration{{
				Name: "plpython3",
				Env: []apiv1.ExtensionEnvVar{
					{Name: "OLD_PYPATH", Value: "${image_root}/lib"},
				},
			}}
			newExt := []apiv1.ExtensionConfiguration{{
				Name: "plpython3",
				Env: []apiv1.ExtensionEnvVar{
					{Name: "NEW_PYPATH", Value: "${image_root}/lib"},
				},
			}}

			SetEnvVars(oldExt, envMap, postgres.ExtensionsBaseDirectory)
			SetEnvVars(newExt, envMap, postgres.UpgradeTargetExtensionsBaseDirectory)

			Expect(envMap).To(HaveKeyWithValue("OLD_PYPATH", "/extensions/plpython3/lib"))
			Expect(envMap).To(HaveKeyWithValue("NEW_PYPATH", "/new-extensions/plpython3/lib"))
		})

		It("lets the target-version value override a same-named source-version value", func() {
			oldExt := []apiv1.ExtensionConfiguration{{
				Name: "plpython3",
				Env: []apiv1.ExtensionEnvVar{
					{Name: "PYTHONPATH", Value: "${image_root}/lib"},
				},
			}}
			newExt := []apiv1.ExtensionConfiguration{{
				Name: "plpython3",
				Env: []apiv1.ExtensionEnvVar{
					{Name: "PYTHONPATH", Value: "${image_root}/lib"},
				},
			}}

			SetEnvVars(oldExt, envMap, postgres.ExtensionsBaseDirectory)
			Expect(envMap).To(HaveKeyWithValue("PYTHONPATH", "/extensions/plpython3/lib"))

			SetEnvVars(newExt, envMap, postgres.UpgradeTargetExtensionsBaseDirectory)
			Expect(envMap).To(HaveKeyWithValue("PYTHONPATH", "/new-extensions/plpython3/lib"))
		})
	})
})

var _ = Describe("AppendPaths", func() {
	It("returns existing unchanged when extra is empty", func() {
		Expect(AppendPaths("/usr/lib", nil)).To(Equal("/usr/lib"))
	})

	It("returns extra joined when existing is empty", func() {
		Expect(AppendPaths("", []string{"/a", "/b"})).To(Equal("/a:/b"))
	})

	It("appends extra after existing with a single colon separator", func() {
		Expect(AppendPaths("/usr/lib", []string{"/a", "/b"})).To(Equal("/usr/lib:/a:/b"))
	})
})

var _ = Describe("CollectLibraryPaths and CollectBinPaths", func() {
	It("normalize user-supplied paths via filepath.Join under baseDir", func() {
		// "/lib", "./lib", and "lib" are all equivalent once joined; this
		// guards against accidental user-supplied absolute paths leaking
		// outside the extension mount.
		exts := []apiv1.ExtensionConfiguration{{
			Name:          "ext1",
			LdLibraryPath: []string{"/lib", "./lib", "lib"},
			BinPath:       []string{"/bin", "./bin", "bin"},
		}}

		libs := CollectLibraryPaths(exts, postgres.ExtensionsBaseDirectory)
		Expect(libs).To(HaveLen(3))
		for _, p := range libs {
			Expect(p).To(Equal("/extensions/ext1/lib"))
		}

		bins := CollectBinPaths(exts, postgres.ExtensionsBaseDirectory)
		Expect(bins).To(HaveLen(3))
		for _, p := range bins {
			Expect(p).To(Equal("/extensions/ext1/bin"))
		}
	})

	It("returns the empty slice when no extensions declare paths", func() {
		Expect(CollectLibraryPaths(nil, postgres.ExtensionsBaseDirectory)).To(BeEmpty())
		Expect(CollectBinPaths(nil, postgres.ExtensionsBaseDirectory)).To(BeEmpty())
	})

	It("uses the upgrade-target baseDir when supplied", func() {
		exts := []apiv1.ExtensionConfiguration{{
			Name:          "ext1",
			LdLibraryPath: []string{"lib"},
			BinPath:       []string{"bin"},
		}}
		Expect(CollectLibraryPaths(exts, postgres.UpgradeTargetExtensionsBaseDirectory)).
			To(ConsistOf("/new-extensions/ext1/lib"))
		Expect(CollectBinPaths(exts, postgres.UpgradeTargetExtensionsBaseDirectory)).
			To(ConsistOf("/new-extensions/ext1/bin"))
	})
})
