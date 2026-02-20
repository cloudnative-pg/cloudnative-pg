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
	"path/filepath"
	"strings"
	"time"

	"github.com/blang/semver"
	corev1 "k8s.io/api/core/v1"
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

var _ = Describe("ImageVolume Extensions", Label(tests.LabelPostgresConfiguration), func() {
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

	It("can use ImageVolume extensions", func() {
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
			database := &apiv1.Database{}
			databaseNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      databaseName,
			}

			Eventually(func(g Gomega) {
				// Updating the Cluster
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).NotTo(HaveOccurred())
				cluster.Spec.PostgresConfiguration.Extensions = append(cluster.Spec.PostgresConfiguration.Extensions,
					apiv1.ExtensionConfiguration{
						Name: "pgvector",
						ImageVolumeSource: corev1.ImageVolumeSource{
							Reference: "ghcr.io/niccolofei/pgvector:18rc1-master-trixie", // wokeignore:rule=master
						},
					})
				g.Expect(env.Client.Update(env.Ctx, cluster)).To(Succeed())

				// Updating the Database
				err = env.Client.Get(env.Ctx, databaseNamespacedName, database)
				g.Expect(err).ToNot(HaveOccurred())
				database.Spec.Extensions = append(database.Spec.Extensions, apiv1.ExtensionSpec{
					DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
						Name:   "vector",
						Ensure: apiv1.EnsurePresent,
					},
				})
				g.Expect(env.Client.Update(env.Ctx, database)).To(Succeed())
			}, 60, 5).Should(Succeed())

			AssertClusterEventuallyReachesPhase(namespace, clusterName,
				[]string{apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 30)
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)

			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			assertVolumeMounts(podList, "pgvector")
			assertVolumes(podList, "pgvector")
			assertExtensions(namespace, databaseName)
		})

		By("verifying the extension's usage ", func() {
			assertPostgis(namespace, clusterName)
			assertVector(namespace, clusterName)
		})
	})
})
