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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blang/semver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ImageVolume Extensions", Label(tests.LabelImageVolumeExtensions), func() {
	const (
		clusterManifest  = fixturesDir + "/imagevolume_extensions/cluster-with-extensions.yaml.template"
		databaseManifest = fixturesDir + "/imagevolume_extensions/database.yaml.template"
		namespacePrefix  = "cluster-imagevolume-extensions"
		level            = tests.Low
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if !IsLocal() {
			Skip("This test is only run on local cluster")
		}
		if env.PostgresVersion < 18 {
			Skip("This test is only run on PostgreSQL v18 or greater")
		}
		// Require K8S 1.33 or greater
		versionInfo, err := env.Interface.Discovery().ServerVersion()
		Expect(err).NotTo(HaveOccurred())
		currentVersion, err := semver.Parse(strings.TrimPrefix(versionInfo.String(), "v"))
		Expect(err).NotTo(HaveOccurred())
		k8s133, err := semver.Parse("1.33.0")
		Expect(err).NotTo(HaveOccurred())
		if currentVersion.LT(k8s133) {
			Skip("This test runs only on Kubernetes 1.33 or greater")
		}
	})
	var namespace, clusterName, databaseName string
	var err error

	assertVolumeMounts := func(podList *corev1.PodList, imageVolumeExtension string) {
		found := false
		volumeName := specs.SanitizeExtensionNameForVolume(imageVolumeExtension)
		mountPath := filepath.Join(postgres.ExtensionsBaseDirectory, imageVolumeExtension)
		for _, pod := range podList.Items {
			for _, volumeMount := range pod.Spec.Containers[0].VolumeMounts {
				if volumeMount.Name == volumeName && volumeMount.MountPath == mountPath {
					found = true
				}
			}
		}
		Expect(found).To(BeTrue())
	}

	assertVolumes := func(podList *corev1.PodList, imageVolumeExtension string) {
		found := false
		volumeName := specs.SanitizeExtensionNameForVolume(imageVolumeExtension)
		for _, pod := range podList.Items {
			for _, volume := range pod.Spec.Volumes {
				if volume.Name == volumeName && volume.Image.Reference != "" {
					found = true
				}
			}
		}
		Expect(found).To(BeTrue())
	}

	assertExtensions := func(namespace, databaseName string) {
		database := &apiv1.Database{}
		databaseNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      databaseName,
		}
		Eventually(func(g Gomega) {
			err := env.Client.Get(env.Ctx, databaseNamespacedName, database)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(database.Status.Applied).Should(HaveValue(BeTrue()))
			g.Expect(database.Status.Message).Should(BeEmpty())
			for _, extension := range database.Status.Extensions {
				Expect(extension.Applied).Should(HaveValue(BeTrue()))
				Expect(extension.Message).Should(BeEmpty())
			}
		}, 60).WithPolling(10 * time.Second).Should(Succeed())
	}

	assertPostgis := func(namespace, clusterName string) {
		row, err := postgresutils.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace, clusterName, postgresutils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"SELECT ST_AsText(geom) AS wkt, ST_Area(geom) AS area"+
				" FROM (SELECT ST_GeomFromText('POLYGON((0 0, 0 10, 10 10, 10 0, 0 0))', 4326) AS geom) AS subquery;")
		Expect(err).ToNot(HaveOccurred())

		var wkt, area string
		err = row.Scan(&wkt, &area)
		Expect(err).ToNot(HaveOccurred())
		Expect(wkt).To(BeEquivalentTo("POLYGON((0 0,0 10,10 10,10 0,0 0))"))
		Expect(area).To(BeEquivalentTo("100"))
	}

	assertVector := func(namespace, clusterName string) {
		row, err := postgresutils.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace, clusterName, postgresutils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"SELECT"+
				" '[1, 2, 3]'::vector AS vec1,"+
				" '[4, 5, 6]'::vector AS vec2,"+
				" cosine_distance('[1, 2, 3]'::vector, '[4, 5, 6]'::vector) AS cosine_sim,"+
				" l2_distance('[1, 2, 3]'::vector, '[4, 5, 6]'::vector) AS l2_dist,"+
				" inner_product('[1, 2, 3]'::vector, '[4, 5, 6]'::vector) AS dot_product;")
		Expect(err).ToNot(HaveOccurred())

		var vec1, vec2, cosineDist, distance, dotProduct string
		err = row.Scan(&vec1, &vec2, &cosineDist, &distance, &dotProduct)
		Expect(err).ToNot(HaveOccurred())
		Expect(vec1).To(BeEquivalentTo("[1,2,3]"))
		Expect(vec2).To(BeEquivalentTo("[4,5,6]"))
		Expect(cosineDist).To(BeEquivalentTo("0.025368153802923787"))
		Expect(distance).To(BeEquivalentTo("5.196152422706632"))
		Expect(dotProduct).To(BeEquivalentTo("32"))
	}

	addExtensionToCluster := func(
		namespace,
		clusterName,
		databaseName string,
		extensionConfig apiv1.ExtensionConfiguration,
		extensionSqlName string,
		additionalGucParams map[string]string,
	) {
		database := &apiv1.Database{}
		databaseNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      databaseName,
		}
		Eventually(func(g Gomega) {
			// Updating the Cluster
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).NotTo(HaveOccurred())
			cluster.Spec.PostgresConfiguration.Extensions = append(
				cluster.Spec.PostgresConfiguration.Extensions, extensionConfig)
			for key, value := range additionalGucParams {
				cluster.Spec.PostgresConfiguration.Parameters[key] = value
			}
			g.Expect(env.Client.Update(env.Ctx, cluster)).To(Succeed())

			// Updating the Database
			err = env.Client.Get(env.Ctx, databaseNamespacedName, database)
			g.Expect(err).ToNot(HaveOccurred())
			database.Spec.Extensions = append(database.Spec.Extensions, apiv1.ExtensionSpec{
				DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
					Name:   extensionSqlName,
					Ensure: apiv1.EnsurePresent,
				},
			})
			g.Expect(env.Client.Update(env.Ctx, database)).To(Succeed())
		}, 60, 5).Should(Succeed())

		AssertClusterEventuallyReachesPhase(namespace, clusterName,
			[]string{apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 300)
		AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)

		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		assertVolumeMounts(podList, extensionConfig.Name)
		assertVolumes(podList, extensionConfig.Name)
		assertExtensions(namespace, databaseName)
	}

	It("via Cluster Spec", func() {
		By("creating the cluster", func() {
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())
			databaseName, err = yaml.GetResourceNameFromYAML(env.Scheme, databaseManifest)
			Expect(err).NotTo(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			CreateResourceFromFile(namespace, databaseManifest)
		})

		By("checking volumes and volumeMounts", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			assertVolumeMounts(podList, "postgis")
			assertVolumes(podList, "postgis")
		})

		By("checking extensions have been created", func() {
			assertExtensions(namespace, databaseName)
		})

		By("adding a new extension to an existing Cluster", func() {
			extensionConfig := apiv1.ExtensionConfiguration{
				Name: "pgvector",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: fmt.Sprintf("ghcr.io/cloudnative-pg/pgvector:0.8.1-%d-trixie", env.PostgresVersion),
				},
			}
			additionalGucParams := map[string]string{
				"hnsw.iterative_scan":    "on",
				"ivfflat.iterative_scan": "on",
			}
			addExtensionToCluster(namespace, clusterName, databaseName, extensionConfig,
				"vector", additionalGucParams)
		})

		By("verifying GUCS have been updated", func() {
			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			QueryMatchExpectationPredicate(primary, postgresutils.PostgresDBName, "SHOW hnsw.iterative_scan", "on")
			QueryMatchExpectationPredicate(primary, postgresutils.PostgresDBName, "SHOW ivfflat.iterative_scan", "on")
		})

		By("verifying the extension's usage ", func() {
			assertPostgis(namespace, clusterName)
			assertVector(namespace, clusterName)
		})
	})

	It("via ImageCatalog", func() {
		storageClass := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
		clusterName = "postgresql-with-extensions"
		catalogName := "catalog-with-extensions"

		By("creating Catalog, Cluster and Database", func() {
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			databaseName, err = yaml.GetResourceNameFromYAML(env.Scheme, databaseManifest)
			Expect(err).NotTo(HaveOccurred())

			postgresImage := fmt.Sprintf("%s:%s", env.PostgresImageName, env.PostgresImageTag)
			catalog := &apiv1.ImageCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      catalogName,
				},
				Spec: apiv1.ImageCatalogSpec{
					Images: []apiv1.CatalogImage{
						{
							Image: postgresImage,
							Major: int(env.PostgresVersion),
							Extensions: []apiv1.ExtensionConfiguration{
								{
									Name: "postgis",
									ImageVolumeSource: corev1.ImageVolumeSource{
										Reference: fmt.Sprintf("ghcr.io/cloudnative-pg/postgis-extension:3.6.1-%d-trixie", env.PostgresVersion),
									},
									LdLibraryPath: []string{"/system"},
								},
							},
						},
					},
				},
			}
			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      clusterName,
				},
				Spec: apiv1.ClusterSpec{
					Instances: 3,
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							APIGroup: &apiv1.SchemeGroupVersion.Group,
							Name:     catalogName,
							Kind:     "ImageCatalog",
						},
						Major: int(env.PostgresVersion),
					},
					PostgresConfiguration: apiv1.PostgresConfiguration{
						Extensions: []apiv1.ExtensionConfiguration{
							{
								Name: "postgis",
							},
						},
						Parameters: map[string]string{
							"log_checkpoints":             "on",
							"log_lock_waits":              "on",
							"log_min_duration_statement":  "1000",
							"log_statement":               "ddl",
							"log_temp_files":              "1024",
							"log_autovacuum_min_duration": "1s",
							"log_replication_commands":    "on",
						},
					},
					Bootstrap: &apiv1.BootstrapConfiguration{InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
					}},
					StorageConfiguration: apiv1.StorageConfiguration{
						Size:         "1Gi",
						StorageClass: &storageClass,
					},
					WalStorage: &apiv1.StorageConfiguration{
						Size:         "1Gi",
						StorageClass: &storageClass,
					},
				},
			}
			err := env.Client.Create(env.Ctx, catalog)
			Expect(err).ToNot(HaveOccurred())
			err = env.Client.Create(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
			CreateResourceFromFile(namespace, databaseManifest)
		})

		By("checking volumes and volumeMounts", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			assertVolumeMounts(podList, "postgis")
			assertVolumes(podList, "postgis")
		})

		By("checking extensions have been created", func() {
			assertExtensions(namespace, databaseName)
		})

		By("adding a new extension by updating the ImageCatalog", func() {
			// Add a new extension to the catalog
			catalog := &apiv1.ImageCatalog{}
			catalogNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      catalogName,
			}
			err := env.Client.Get(env.Ctx, catalogNamespacedName, catalog)
			Expect(err).ToNot(HaveOccurred())
			catalog.Spec.Images[0].Extensions = append(catalog.Spec.Images[0].Extensions, apiv1.ExtensionConfiguration{
				Name: "pgvector",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: fmt.Sprintf("ghcr.io/cloudnative-pg/pgvector:0.8.1-%d-trixie", env.PostgresVersion),
				},
			})
			err = env.Client.Update(env.Ctx, catalog)
			Expect(err).ToNot(HaveOccurred())

			extensionConfig := apiv1.ExtensionConfiguration{Name: "pgvector"}
			additionalGucParams := map[string]string{
				"hnsw.iterative_scan":    "on",
				"ivfflat.iterative_scan": "on",
			}
			addExtensionToCluster(namespace, clusterName, databaseName, extensionConfig,
				"vector", additionalGucParams)
		})

		By("verifying GUCS have been updated", func() {
			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			QueryMatchExpectationPredicate(primary, postgresutils.PostgresDBName, "SHOW hnsw.iterative_scan", "on")
			QueryMatchExpectationPredicate(primary, postgresutils.PostgresDBName, "SHOW ivfflat.iterative_scan", "on")
		})

		By("verifying the extension's usage ", func() {
			assertPostgis(namespace, clusterName)
			assertVector(namespace, clusterName)
		})
	})
})
