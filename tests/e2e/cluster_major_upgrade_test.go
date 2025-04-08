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
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Postgres Major Upgrade", Label(tests.LabelPostgresMajorUpgrade), func() {
	const (
		level                  = tests.Medium
		namespacePrefix        = "cluster-major-upgrade"
		postgisEntry           = "postgis"
		postgresqlEntry        = "postgresql"
		postgresqlMinimalEntry = "postgresql-minimal"
	)

	var namespace string

	type scenario struct {
		startingCluster *v1.Cluster
		startingMajor   int
		targetImage     string
		targetMajor     int
	}
	scenarios := map[string]*scenario{}

	generateBaseCluster := func(namespace string, storageClass string) *v1.Cluster {
		return &v1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg-major-upgrade",
				Namespace: namespace,
			},
			Spec: v1.ClusterSpec{
				Instances: 3,
				Bootstrap: &v1.BootstrapConfiguration{
					InitDB: &v1.BootstrapInitDB{
						DataChecksums:  ptr.To(true),
						WalSegmentSize: 32,
					},
				},
				StorageConfiguration: v1.StorageConfiguration{
					StorageClass: &storageClass,
					Size:         "1Gi",
				},
				WalStorage: &v1.StorageConfiguration{
					StorageClass: &storageClass,
					Size:         "1Gi",
				},
				PostgresConfiguration: v1.PostgresConfiguration{
					Parameters: map[string]string{
						"log_checkpoints":             "on",
						"log_lock_waits":              "on",
						"log_min_duration_statement":  "1000",
						"log_statement":               "ddl",
						"log_temp_files":              "1024",
						"log_autovacuum_min_duration": "1000",
						"log_replication_commands":    "on",
						"max_slot_wal_keep_size":      "1GB",
					},
				},
			},
		}
	}

	generatePostgreSQLCluster := func(namespace string, storageClass string, majorVersion int) *v1.Cluster {
		cluster := generateBaseCluster(namespace, storageClass)
		cluster.Spec.ImageName = "ghcr.io/cloudnative-pg/postgresql:" + strconv.Itoa(majorVersion)
		cluster.Spec.Bootstrap = &v1.BootstrapConfiguration{
			InitDB: &v1.BootstrapInitDB{
				PostInitSQL: []string{
					"CREATE EXTENSION IF NOT EXISTS pg_stat_statements;",
					"CREATE EXTENSION IF NOT EXISTS pg_trgm;",
				},
			},
		}
		cluster.Spec.PostgresConfiguration.Parameters["pg_stat_statements.track"] = "top"
		return cluster
	}
	generatePostgreSQLMinimalCluster := func(namespace string, storageClass string, majorVersion int) *v1.Cluster {
		cluster := generatePostgreSQLCluster(namespace, storageClass, majorVersion)
		cluster.Spec.ImageName = fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%d-minimal-bookworm", majorVersion)
		return cluster
	}

	generatePostGISCluster := func(namespace string, storageClass string, majorVersion int) *v1.Cluster {
		cluster := generateBaseCluster(namespace, storageClass)
		cluster.Spec.ImageName = "ghcr.io/cloudnative-pg/postgis:" + strconv.Itoa(majorVersion)
		cluster.Spec.Bootstrap = &v1.BootstrapConfiguration{
			InitDB: &v1.BootstrapInitDB{
				PostInitApplicationSQL: []string{
					"CREATE EXTENSION postgis",
					"CREATE EXTENSION postgis_raster",
					"CREATE EXTENSION postgis_sfcgal",
					"CREATE EXTENSION fuzzystrmatch",
					"CREATE EXTENSION address_standardizer",
					"CREATE EXTENSION address_standardizer_data_us",
					"CREATE EXTENSION postgis_tiger_geocoder",
					"CREATE EXTENSION postgis_topology",
					"CREATE TABLE geometries (name varchar, geom geometry)",
					"INSERT INTO geometries VALUES" +
						" ('Point', 'POINT(0 0)')," +
						" ('Linestring', 'LINESTRING(0 0, 1 1, 2 1, 2 2)')," +
						" ('Polygon', 'POLYGON((0 0, 1 0, 1 1, 0 1, 0 0))')," +
						" ('PolygonWithHole', 'POLYGON((0 0, 10 0, 10 10, 0 10, 0 0),(1 1, 1 2, 2 2, 2 1, 1 1))')," +
						" ('Collection', 'GEOMETRYCOLLECTION(POINT(2 0),POLYGON((0 0, 1 0, 1 1, 0 1, 0 0)))');",
				},
			},
		}
		return cluster
	}

	determineVersionsForTesting := func() (uint64, uint64) {
		currentImage := os.Getenv("POSTGRES_IMG")
		Expect(currentImage).ToNot(BeEmpty())

		currentVersion, err := version.FromTag(reference.New(currentImage).Tag)
		Expect(err).NotTo(HaveOccurred())
		currentMajor := currentVersion.Major()

		targetVersion, err := version.FromTag(reference.New(versions.DefaultImageName).Tag)
		Expect(err).ToNot(HaveOccurred())
		targetMajor := targetVersion.Major()

		// If same version, choose a previous one for testing
		if currentMajor == targetMajor {
			currentMajor = targetMajor - (uint64(rand.Int() % 4)) - 1
			GinkgoWriter.Printf("Using %v as the current major version instead.\n", currentMajor)
		}

		return currentMajor, targetMajor
	}

	// generateTargetImages, given a targetMajor, generates a target image for each buildScenario.
	// MAJOR_UPGRADE_IMAGE_REPO env allows to customize the target image repository.
	generateTargetImages := func(targetMajor uint64) map[string]string {
		// Default target Images
		targetImages := map[string]string{
			postgisEntry:           fmt.Sprintf("%v:%v", postgres.PostgisImageRepository, targetMajor),
			postgresqlEntry:        fmt.Sprintf("%v:%v", postgres.ImageRepository, targetMajor),
			postgresqlMinimalEntry: fmt.Sprintf("%v:%v-minimal-bookworm", postgres.ImageRepository, targetMajor),
		}
		// Set custom targets when detecting a given env variable
		if envValue := os.Getenv("MAJOR_UPGRADE_IMAGE_REPO"); envValue != "" {
			targetImages[postgisEntry] = fmt.Sprintf("%v:%v-postgis-bookworm", envValue, targetMajor)
			targetImages[postgresqlEntry] = fmt.Sprintf("%v:%v-standard-bookworm", envValue, targetMajor)
			targetImages[postgresqlMinimalEntry] = fmt.Sprintf("%v:%v-minimal-bookworm", envValue, targetMajor)
		}

		return targetImages
	}

	buildScenarios := func(
		namespace string, storageClass string, currentMajor, targetMajor uint64,
	) map[string]*scenario {
		targetImages := generateTargetImages(targetMajor)

		return map[string]*scenario{
			postgisEntry: {
				startingCluster: generatePostGISCluster(namespace, storageClass, int(currentMajor)),
				startingMajor:   int(currentMajor),
				targetImage:     targetImages[postgisEntry],
				targetMajor:     int(targetMajor),
			},
			postgresqlEntry: {
				startingCluster: generatePostgreSQLCluster(namespace, storageClass, int(currentMajor)),
				startingMajor:   int(currentMajor),
				targetImage:     targetImages[postgresqlEntry],
				targetMajor:     int(targetMajor),
			},
			postgresqlMinimalEntry: {
				startingCluster: generatePostgreSQLMinimalCluster(namespace, storageClass, int(currentMajor)),
				startingMajor:   int(currentMajor),
				targetImage:     targetImages[postgresqlMinimalEntry],
				targetMajor:     int(targetMajor),
			},
		}
	}

	verifyPodsChanged := func(
		ctx context.Context, client client.Client, cluster *v1.Cluster, oldPodsUUIDs []types.UID,
	) {
		Eventually(func(g Gomega) {
			podList, err := clusterutils.ListPods(ctx, client, cluster.Name, cluster.Namespace)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(podList.Items).To(HaveLen(len(oldPodsUUIDs)))
			for _, pod := range podList.Items {
				g.Expect(oldPodsUUIDs).NotTo(ContainElement(pod.UID))
			}
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	}

	verifyPVCsChanged := func(
		ctx context.Context, client client.Client, cluster *v1.Cluster, oldPVCsUUIDs []types.UID,
	) {
		Eventually(func(g Gomega) {
			pvcList, err := storage.GetPVCList(ctx, client, cluster.Namespace)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(pvcList.Items).To(HaveLen(len(oldPVCsUUIDs)))
			for _, pvc := range pvcList.Items {
				if pvc.Labels[utils.ClusterInstanceRoleLabelName] == specs.ClusterRoleLabelReplica {
					g.Expect(oldPVCsUUIDs).NotTo(ContainElement(pvc.UID))
				} else {
					g.Expect(oldPVCsUUIDs).To(ContainElement(pvc.UID))
				}
			}
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	}

	verifyPostgresVersion := func(
		env *environment.TestingEnvironment, primary *corev1.Pod, oldStdOut string, targetMajor int,
	) {
		Eventually(func(g Gomega) {
			stdOut, stdErr, err := exec.EventuallyExecQueryInInstancePod(env.Ctx, env.Client, env.Interface,
				env.RestClientConfig,
				exec.PodLocator{Namespace: primary.GetNamespace(), PodName: primary.GetName()}, postgres.AppDBName,
				"SELECT version();", 60, objects.PollingTime)
			g.Expect(err).ToNot(HaveOccurred(), "failed to execute version query")
			g.Expect(stdErr).To(BeEmpty(), "unexpected stderr output when checking version")
			g.Expect(stdOut).ToNot(Equal(oldStdOut), "postgres version did not change")
			g.Expect(stdOut).To(ContainSubstring(strconv.Itoa(targetMajor)),
				fmt.Sprintf("version string doesn't contain expected major version %d: %s", targetMajor, stdOut))
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	}

	verifyCleanupAfterUpgrade := func(ctx context.Context, client client.Client, primary *corev1.Pod) {
		shouldHaveBeenDeleted := []string{
			"/var/lib/postgresql/data/pgdata/pg_upgrade_output.d",
			"/var/lib/postgresql/data/pgdata-new",
			"/var/lib/postgresql/data/pgwal-new",
		}
		timeout := time.Second * 20
		for _, path := range shouldHaveBeenDeleted {
			_, stdErr, err := exec.CommandInInstancePod(ctx, client, env.Interface, env.RestClientConfig,
				exec.PodLocator{Namespace: primary.GetNamespace(), PodName: primary.GetName()}, &timeout,
				"stat", path)
			Expect(err).To(HaveOccurred(), "path: %s", path)
			Expect(stdErr).To(ContainSubstring("No such file or directory"), "path: %s", path)
		}
	}

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		currentMajor, targetMajor := determineVersionsForTesting()
		var err error
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		storageClass := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
		Expect(storageClass).ToNot(BeEmpty())

		// We cannot use generated entries in the DescribeTable, so we use the scenario key as a constant, but
		// define the actual content here.
		// See https://onsi.github.io/ginkgo/#mental-model-table-specs-are-just-syntactic-sugar
		scenarios = buildScenarios(namespace, storageClass, currentMajor, targetMajor)
	})

	DescribeTable("can upgrade a Cluster to a newer major version", func(scenarioName string) {
		By("Creating the starting cluster")
		scenario := scenarios[scenarioName]
		cluster := scenario.startingCluster
		err := env.Client.Create(env.Ctx, cluster)
		Expect(err).NotTo(HaveOccurred())
		AssertClusterIsReady(cluster.Namespace, cluster.Name, testTimeouts[timeouts.ClusterIsReady],
			env)

		By("Collecting the pods UUIDs")
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, cluster.Name, cluster.Namespace)
		Expect(err).ToNot(HaveOccurred())
		oldPodsUUIDs := make([]types.UID, len(podList.Items))
		for i, pod := range podList.Items {
			oldPodsUUIDs[i] = pod.UID
		}

		By("Collecting the PVCs UUIDs")
		pvcList, err := storage.GetPVCList(env.Ctx, env.Client, cluster.Namespace)
		Expect(err).ToNot(HaveOccurred())
		oldPVCsUUIDs := make([]types.UID, len(pvcList.Items))
		for i, pvc := range pvcList.Items {
			oldPVCsUUIDs[i] = pvc.UID
		}

		By("Checking the starting version of the cluster")
		primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, cluster.Namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())

		oldStdOut, stdErr, err := exec.EventuallyExecQueryInInstancePod(env.Ctx, env.Client, env.Interface,
			env.RestClientConfig,
			exec.PodLocator{Namespace: primary.GetNamespace(), PodName: primary.GetName()}, postgres.AppDBName,
			"SELECT version();", 60, objects.PollingTime)
		Expect(err).ToNot(HaveOccurred())
		Expect(stdErr).To(BeEmpty())
		Expect(oldStdOut).To(ContainSubstring(strconv.Itoa(scenario.startingMajor)))

		By("Updating the major")
		Eventually(func() error {
			cluster, err = clusterutils.Get(env.Ctx, env.Client, cluster.Namespace, cluster.Name)
			if err != nil {
				return err
			}
			cluster.Spec.ImageName = scenario.targetImage
			return env.Client.Update(env.Ctx, cluster)
		}).WithTimeout(1*time.Minute).WithPolling(10*time.Second).Should(
			Succeed(),
			"Failed to update cluster image from %s to %s",
			cluster.Spec.ImageName,
			scenario.targetImage,
		)

		By("Waiting for the cluster to be in the major upgrade phase")
		Eventually(func(g Gomega) {
			cluster, err = clusterutils.Get(env.Ctx, env.Client, cluster.Namespace, cluster.Name)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cluster.Status.Phase).To(Equal(v1.PhaseMajorUpgrade))
		}).WithTimeout(1 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		AssertClusterIsReady(cluster.Namespace, cluster.Name, testTimeouts[timeouts.ClusterIsReady], env)

		// The upgrade destroys all the original pods and creates new ones. We want to make sure that we have
		// the same amount of pods as before, but with different UUIDs.
		By("Verifying the pods UUIDs have changed")
		verifyPodsChanged(env.Ctx, env.Client, cluster, oldPodsUUIDs)

		// The upgrade destroys all the original PVCs and creates new ones, except for the ones associated to the
		// primary. We want to make sure that we have the same amount of PVCs as before, but with different UUIDs,
		// which should be the same instead for the primary PVCs.
		By("Verifying the replicas' PVCs have changed")
		verifyPVCsChanged(env.Ctx, env.Client, cluster, oldPVCsUUIDs)

		// Check that the version has been updated
		By("Verifying the cluster is running the target version")
		verifyPostgresVersion(env, primary, oldStdOut, scenario.targetMajor)

		// Expect temporary files to be deleted
		By("Checking no leftovers exist from the upgrade")
		verifyCleanupAfterUpgrade(env.Ctx, env.Client, primary)
	},
		Entry("PostGIS", postgisEntry),
		Entry("PostgreSQL", postgresqlEntry),
		Entry("PostgreSQL minimal", postgresqlMinimalEntry),
	)
})
