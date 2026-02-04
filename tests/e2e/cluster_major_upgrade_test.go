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
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/minio"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
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
		postgresqlSystemEntry  = "postgresql-system"

		// custom registry envs
		customPostgresImageRegistryEnvVar       = "POSTGRES_MAJOR_UPGRADE_IMAGE_REGISTRY"
		customPostgresImageStandardSuffixEnvVar = "POSTGRES_MAJOR_UPGRADE_STANDARD_SUFFIX"
		customPostgresImageMinimalSuffixEnvVar  = "POSTGRES_MAJOR_UPGRADE_MINIMAL_SUFFIX"
		customPostgresImageSystemSuffixEnvVar   = "POSTGRES_MAJOR_UPGRADE_SYSTEM_SUFFIX"
		customPostgresImagePostGISSuffixEnvVar  = "POSTGRES_MAJOR_UPGRADE_POSTGIS_SUFFIX"

		// default suffixes used when overriding registry via env vars
		// as defined by postgres-trunk-containers tests
		defaultStandardSuffix = "-standard-trixie"
		defaultMinimalSuffix  = "-minimal-trixie"
		defaultSystemSuffix   = "-system-trixie"
		defaultPostGISSuffix  = "-postgis-trixie"
	)

	type scenario struct {
		startingCluster *apiv1.Cluster
		startingMajor   int
		targetImage     string
		targetMajor     int
	}
	scenarios := map[string]*scenario{}

	generateBaseCluster := func(namespace string, storageClass string, enableBackup bool) *apiv1.Cluster {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg-major-upgrade",
				Namespace: namespace,
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						DataChecksums:  ptr.To(true),
						WalSegmentSize: 32,
					},
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					StorageClass: &storageClass,
					Size:         "1Gi",
				},
				WalStorage: &apiv1.StorageConfiguration{
					StorageClass: &storageClass,
					Size:         "1Gi",
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"log_checkpoints":             "on",
						"log_lock_waits":              "on",
						"log_min_duration_statement":  "1000",
						"log_statement":               "ddl",
						"log_temp_files":              "1024",
						"log_autovacuum_min_duration": "1000",
						"log_replication_commands":    "on",
					},
				},
			},
		}
		if enableBackup {
			cluster.Spec.Backup = &apiv1.BackupConfiguration{
				Target: apiv1.BackupTargetPrimary,
				BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
					BarmanCredentials: apiv1.BarmanCredentials{
						AWS: &apiv1.S3Credentials{
							AccessKeyIDReference: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{
									Name: "backup-storage-creds",
								},
								Key: "ID",
							},
							SecretAccessKeyReference: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{
									Name: "backup-storage-creds",
								},
								Key: "KEY",
							},
						},
					},
					DestinationPath: "s3://pg-major-upgrade/",
					EndpointURL:     "https://minio-service.minio:9000",
					EndpointCA: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "minio-server-ca-secret",
						},
						Key: "ca.crt",
					},
					Wal: &apiv1.WalBackupConfiguration{
						Compression: "gzip",
					},
				},
				RetentionPolicy: "30d",
			}
		}
		return cluster
	}

	generatePostgreSQLCluster := func(
		namespace string, storageClass string, tagVersion string, enableBackup bool,
	) *apiv1.Cluster {
		cluster := generateBaseCluster(namespace, storageClass, enableBackup)
		cluster.Spec.ImageName = env.OfficialStandardImageName(tagVersion)
		cluster.Spec.Bootstrap.InitDB.PostInitSQL = []string{
			"CREATE EXTENSION IF NOT EXISTS pg_stat_statements;",
			"CREATE EXTENSION IF NOT EXISTS pg_trgm;",
		}
		cluster.Spec.PostgresConfiguration.Parameters["pg_stat_statements.track"] = "top"
		return cluster
	}

	generatePostgreSQLMinimalCluster := func(
		namespace string, storageClass string, tagVersion string, enableBackup bool,
	) *apiv1.Cluster {
		cluster := generatePostgreSQLCluster(namespace, storageClass, tagVersion, enableBackup)
		cluster.Spec.ImageName = env.OfficialMinimalImageName(tagVersion)
		return cluster
	}

	generatePostgreSQLSystemCluster := func(
		namespace string, storageClass string, tagVersion string, enableBackup bool,
	) *apiv1.Cluster {
		cluster := generatePostgreSQLCluster(namespace, storageClass, tagVersion, enableBackup)
		cluster.Spec.ImageName = env.OfficialSystemImageName(tagVersion)
		return cluster
	}

	generatePostGISCluster := func(
		namespace string, storageClass string, tagVersion string, enableBackup bool,
	) *apiv1.Cluster {
		cluster := generateBaseCluster(namespace, storageClass, enableBackup)
		cluster.Spec.ImageName = env.PostGISImageName(tagVersion)
		cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQL = []string{
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
		}
		return cluster
	}

	type versionInfo struct {
		currentMajor uint64
		currentTag   string
		targetMajor  uint64
		targetTag    string
	}

	determineVersionsForTesting := func() versionInfo {
		currentVersion, err := version.FromTag(env.PostgresImageTag)
		Expect(err).NotTo(HaveOccurred())
		currentMajor := currentVersion.Major()
		currentTag := strconv.FormatUint(currentMajor, 10)

		targetVersion, err := version.FromTag(reference.New(versions.DefaultImageName).Tag)
		Expect(err).ToNot(HaveOccurred())
		targetMajor := targetVersion.Major()
		targetTag := strconv.FormatUint(targetMajor, 10)

		// If same version, choose a previous one for testing
		if currentMajor == targetMajor {
			currentMajor = targetMajor - (uint64(rand.Int() % 4)) - 1
			GinkgoWriter.Printf("Using %v as the current major version instead.\n", currentMajor)
		}

		// This means we are on a beta version, so we can just invert the versions
		if currentMajor > targetMajor {
			currentMajor, targetMajor = targetMajor, currentMajor
			currentTag = targetTag
			// Beta images don't have a major version only tag yet, and
			// are most likely in the following format: "18beta1", "18rc2"
			// So, we split at the first `-` and use that prefix to build the target image.
			targetTag = strings.Split(env.PostgresImageTag, "-")[0]
			GinkgoWriter.Printf("Using %v as the current major and upgrading to %v.\n", currentMajor, targetMajor)
		}

		return versionInfo{
			currentMajor: currentMajor,
			currentTag:   currentTag,
			targetMajor:  targetMajor,
			targetTag:    targetTag,
		}
	}

	// generateTargetImages, given a targetMajor, generates a target image for each buildScenario.
	// It allows overriding the target image's repositories via env variables.
	generateTargetImages := func(targetTag string) map[string]string {
		// Default target Images
		targetImages := map[string]string{
			postgisEntry:           env.PostGISImageName(targetTag),
			postgresqlEntry:        env.StandardImageName(targetTag),
			postgresqlMinimalEntry: env.MinimalImageName(targetTag),
			postgresqlSystemEntry:  env.SystemImageName(targetTag),
		}

		// Set custom targets when detecting env variables (used by postgres-trunk-containers tests)
		if envValue := os.Getenv(customPostgresImageRegistryEnvVar); envValue != "" {
			standardSuffix := os.Getenv(customPostgresImageStandardSuffixEnvVar)
			if standardSuffix == "" {
				standardSuffix = defaultStandardSuffix
			}
			minimalSuffix := os.Getenv(customPostgresImageMinimalSuffixEnvVar)
			if minimalSuffix == "" {
				minimalSuffix = defaultMinimalSuffix
			}
			systemSuffix := os.Getenv(customPostgresImageSystemSuffixEnvVar)
			if systemSuffix == "" {
				systemSuffix = defaultSystemSuffix
			}
			postgisSuffix := os.Getenv(customPostgresImagePostGISSuffixEnvVar)
			if postgisSuffix == "" {
				postgisSuffix = defaultPostGISSuffix
			}

			targetImages[postgresqlEntry] = fmt.Sprintf("%v:%v%s", envValue, targetTag, standardSuffix)
			targetImages[postgresqlMinimalEntry] = fmt.Sprintf("%v:%v%s", envValue, targetTag, minimalSuffix)
			targetImages[postgresqlSystemEntry] = fmt.Sprintf("%v:%v%s", envValue, targetTag, systemSuffix)
			targetImages[postgisEntry] = fmt.Sprintf("%v:%v%s", envValue, targetTag, postgisSuffix)
		}

		return targetImages
	}

	buildScenarios := func(
		namespace string, storageClass string, info versionInfo,
	) map[string]*scenario {
		targetImages := generateTargetImages(info.targetTag)

		return map[string]*scenario{
			postgisEntry: {
				startingCluster: generatePostGISCluster(namespace, storageClass, strconv.FormatUint(info.currentMajor, 10), false),
				startingMajor:   int(info.currentMajor),
				targetImage:     targetImages[postgisEntry],
				targetMajor:     int(info.targetMajor),
			},
			postgresqlEntry: {
				startingCluster: generatePostgreSQLCluster(namespace, storageClass,
					strconv.FormatUint(info.currentMajor, 10), false),
				startingMajor: int(info.currentMajor),
				targetImage:   targetImages[postgresqlEntry],
				targetMajor:   int(info.targetMajor),
			},
			postgresqlMinimalEntry: {
				startingCluster: generatePostgreSQLMinimalCluster(namespace, storageClass,
					strconv.FormatUint(info.currentMajor, 10), false),
				startingMajor: int(info.currentMajor),
				targetImage:   targetImages[postgresqlMinimalEntry],
				targetMajor:   int(info.targetMajor),
			},
			postgresqlSystemEntry: {
				startingCluster: generatePostgreSQLSystemCluster(namespace, storageClass,
					strconv.FormatUint(info.currentMajor, 10), true),
				startingMajor: int(info.currentMajor),
				targetImage:   targetImages[postgresqlSystemEntry],
				targetMajor:   int(info.targetMajor),
			},
		}
	}

	verifyPodsChanged := func(
		ctx context.Context, client client.Client, cluster *apiv1.Cluster, oldPodsUUIDs []types.UID,
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
		ctx context.Context, client client.Client, cluster *apiv1.Cluster, oldPVCsUUIDs []types.UID,
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

	verifyTimelineResetAfterUpgrade := func(ctx context.Context, client client.Client, cluster *apiv1.Cluster) {
		currentCluster, err := clusterutils.Get(ctx, client, cluster.Namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentCluster.Status.TimelineID).To(Equal(1))
	}

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		versionInfo := determineVersionsForTesting()
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		storageClass := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
		Expect(storageClass).ToNot(BeEmpty())

		By("creating the certificates for MinIO", func() {
			err := minioEnv.CreateCaSecret(env, namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		By("creating the credentials for minio", func() {
			_, err = secrets.CreateObjectStorageSecret(
				env.Ctx,
				env.Client,
				namespace,
				"backup-storage-creds",
				"minio",
				"minio123",
			)
			Expect(err).ToNot(HaveOccurred())
		})

		// We cannot use generated entries in the DescribeTable, so we use the scenario key as a constant, but
		// define the actual content here.
		// See https://onsi.github.io/ginkgo/#mental-model-table-specs-are-just-syntactic-sugar
		scenarios = buildScenarios(namespace, storageClass, versionInfo)
	})

	DescribeTable("can upgrade a Cluster to a newer major version", func(scenarioName string) {
		By("Creating the starting cluster")
		scenario := scenarios[scenarioName]
		cluster := scenario.startingCluster
		err := env.Client.Create(env.Ctx, cluster)
		Expect(err).NotTo(HaveOccurred())
		AssertClusterIsReady(cluster.Namespace, cluster.Name, testTimeouts[timeouts.ClusterIsReady],
			env)

		if cluster.Spec.Backup != nil {
			By("verifying connectivity of barman to minio", func() {
				primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, cluster.Namespace, cluster.Name)
				Expect(err).ToNot(HaveOccurred())
				Eventually(func() (bool, error) {
					connectionStatus, err := minio.TestBarmanConnectivity(
						cluster.Namespace, cluster.Name, primaryPod.Name,
						"minio", "minio123", minioEnv.ServiceName)
					return connectionStatus, err
				}, 60).Should(BeTrue())
			})
		}

		By("Performing switchover to move to timeline 2")
		AssertSwitchover(cluster.Namespace, cluster.Name, env)

		By("Verifying cluster is on timeline 2 before upgrade")
		Eventually(func(g Gomega) {
			currentCluster, err := clusterutils.Get(env.Ctx, env.Client, cluster.Namespace, cluster.Name)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(currentCluster.Status.TimelineID).To(Equal(2))
		}).WithTimeout(time.Duration(testTimeouts[timeouts.NewPrimaryAfterSwitchover]) * time.Second).
			WithPolling(5 * time.Second).Should(Succeed())

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
			g.Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseMajorUpgrade))
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

		By("Verifying timeline ID is reset to 1 after major upgrade")
		verifyTimelineResetAfterUpgrade(env.Ctx, env.Client, cluster)

		// Expect temporary files to be deleted
		By("Checking no leftovers exist from the upgrade")
		verifyCleanupAfterUpgrade(env.Ctx, env.Client, primary)

		// Verify WAL archiving continues to work after the major upgrade
		if cluster.Spec.Backup != nil {
			By("Verifying WAL archiving works after the major upgrade")
			AssertArchiveWalOnMinio(cluster.Namespace, cluster.Name, cluster.Name)
		}
	},
		Entry("PostGIS", postgisEntry),
		Entry("PostgreSQL", postgresqlEntry),
		Entry("PostgreSQL minimal", postgresqlMinimalEntry),
		Entry("PostgreSQL system", postgresqlSystemEntry),
	)
})
