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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/minio"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/proxy"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

/*
This test includes deleting the `cnpg-system` namespace and deploying different
operator versions.
It therefore must be run isolated from the other E2E tests.

In summary: a cluster is created with an initial version of the operator.
Then the operator is upgraded to a later version.
We test four scenarios:

1 and 2) the initial operator installed is the most recent tag. We upgrade to the
  current operator build. We test this with rolling and online upgrades.
3 and 4) the initial operator installed is the current operator build. We upgrade
  to a `prime` version built from the same code, only with a different image tag
  and a different build VERSION (required to have a different binary hash).
  This `prime` version is built in the `setup-cluster.sh` script or in the
  `buildx` phase of the continuous-delivery GH workflow.
  We test with online and rolling upgrades.

To check the soundness of the upgrade, on each of the four scenarios:

* We test changing the configuration. That will induce a switchover.
* A Backup created with the initial version is still there after upgrade, and
  can be used to bootstrap a cluster.
* A ScheduledBackup created with the initial version is still scheduled
  after the upgrade.
* All the cluster pods should have their instance manager updated.

*/

var _ = Describe("Upgrade", Label(tests.LabelUpgrade, tests.LabelNoOpenshift), Ordered, Serial, func() {
	const (
		operatorNamespace       = "cnpg-system"
		configName              = "cnpg-controller-manager-config"
		currentOperatorManifest = fixturesDir + "/upgrade/current-manifest.yaml"
		primeOperatorManifest   = fixturesDir + "/upgrade/current-manifest-prime.yaml"
		rollingUpgradeNamespace = "rolling-upgrade"
		onlineUpgradeNamespace  = "online-upgrade"

		pgSecrets = fixturesDir + "/upgrade/pgsecrets.yaml" //nolint:gosec

		// This is a cluster of the previous version, created before the operator upgrade
		clusterName1 = "cluster1"
		sampleFile   = fixturesDir + "/upgrade/cluster1.yaml.template"
		minioPath1   = "minio/cluster-full-backup"

		// This is a cluster of the previous version, created after the operator upgrade
		clusterName2 = "cluster2"
		sampleFile2  = fixturesDir + "/upgrade/cluster2.yaml.template"
		minioPath2   = "minio/cluster2-full-backup"

		backupName          = "cluster-backup"
		backupFile          = fixturesDir + "/upgrade/backup1.yaml"
		restoreFile         = fixturesDir + "/upgrade/cluster-restore.yaml.template"
		scheduledBackupFile = fixturesDir + "/upgrade/scheduled-backup.yaml"

		pgBouncerSampleFile = fixturesDir + "/upgrade/pgbouncer.yaml"
		pgBouncerName       = "pgbouncer"
		level               = tests.Lowest
	)

	BeforeAll(func() {
		if os.Getenv("TEST_SKIP_UPGRADE") != "" {
			Skip("Skipping upgrade test because TEST_SKIP_UPGRADE variable is defined")
		}
		if IsOpenshift() {
			Skip("This test case is not applicable on OpenShift clusters")
		}
	})

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		// Since the 'cnpg-system' namespace is deleted after each spec is completed,
		// we should create it and then create the pull image secret

		err := namespaces.EnsureNamespace(env.Ctx, env.Client, operatorNamespace)
		Expect(err).NotTo(HaveOccurred())

		dockerServer := os.Getenv("DOCKER_SERVER")
		dockerUsername := os.Getenv("DOCKER_USERNAME")
		dockerPassword := os.Getenv("DOCKER_PASSWORD")
		if dockerServer != "" && dockerUsername != "" && dockerPassword != "" {
			_, _, err := run.Run(fmt.Sprintf(`kubectl -n %v create secret docker-registry
			cnpg-pull-secret
			--docker-server="%v"
			--docker-username="%v"
			--docker-password="%v"`,
				operatorNamespace,
				dockerServer,
				dockerUsername,
				dockerPassword,
			))
			Expect(err).NotTo(HaveOccurred())
		}
	})

	// Check that the amount of backups is increasing on minio.
	// This check relies on the fact that nothing is performing backups
	// but a single scheduled backups during the check
	AssertScheduledBackupsAreScheduled := func(serverName string) {
		By("verifying scheduled backups are still happening", func() {
			latestTar := minio.GetFilePath(serverName, "data.tar.gz")
			currentBackups, err := minio.CountFiles(minioEnv, latestTar)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() (int, error) {
				return minio.CountFiles(minioEnv, latestTar)
			}, 120).Should(BeNumerically(">", currentBackups))
		})
	}

	applyConfUpgrade := func(cluster *apiv1.Cluster) error {
		// changes some parameters in the Postgres configuration, and the `pg_hba` entries
		oldCluster := cluster.DeepCopy()
		cluster.Spec.PostgresConfiguration.Parameters["shared_buffers"] = "128MB"
		cluster.Spec.PostgresConfiguration.Parameters["work_mem"] = "8MB"
		cluster.Spec.PostgresConfiguration.Parameters["max_replication_slots"] = "16"
		cluster.Spec.PostgresConfiguration.Parameters["maintenance_work_mem"] = "256MB"
		cluster.Spec.PostgresConfiguration.PgHBA[0] = "host all all all trust"
		return env.Client.Patch(env.Ctx, cluster, ctrlclient.MergeFrom(oldCluster))
	}

	AssertConfUpgrade := func(clusterName, upgradeNamespace string) {
		databaseName := "appdb"

		By("checking basic functionality performing a configuration upgrade on the cluster", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, upgradeNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			// Gather current primary
			cluster, err := clusterutils.Get(env.Ctx, env.Client, upgradeNamespace, clusterName)
			Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))

			oldPrimary := cluster.Status.CurrentPrimary
			oldPrimaryTimestamp := cluster.Status.CurrentPrimaryTimestamp
			// Update the configuration. It may take some time after the
			// upgrade for the webhook "mcluster.cnpg.io" to work and accept
			// the `apply` command

			Eventually(func() error {
				err := applyConfUpgrade(cluster)
				return err
			}, 60).ShouldNot(HaveOccurred())

			timeout := 300
			// Check that both parameters have been modified in each pod
			for _, pod := range podList.Items {
				Eventually(func() (int, error) {
					stdout, stderr, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{
							Namespace: pod.Namespace,
							PodName:   pod.Name,
						},
						postgres.PostgresDBName,
						"show max_replication_slots")
					if err != nil {
						return 0, err
					}
					replicaSlot := strings.Trim(stdout, "\n")
					if len(replicaSlot) == 0 {
						return 0, fmt.Errorf("empty max_replication_slots value. stderr: %q", stderr)
					}
					return strconv.Atoi(replicaSlot)
				}, timeout).Should(BeEquivalentTo(16),
					"Pod %v should have updated its config", pod.Name)

				Eventually(func() (int, error, error) {
					stdout, _, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{
							Namespace: pod.Namespace,
							PodName:   pod.Name,
						},
						postgres.PostgresDBName,
						"show maintenance_work_mem")
					value, atoiErr := strconv.Atoi(strings.Trim(stdout, "MB\n"))
					return value, err, atoiErr
				}, timeout).Should(BeEquivalentTo(256),
					"Pod %v should have updated its config", pod.Name)
			}
			// Check that a switchover happened
			Eventually(func(g Gomega) bool {
				c, err := clusterutils.Get(env.Ctx, env.Client, upgradeNamespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())

				GinkgoWriter.Printf("Current Primary: %s, Current Primary timestamp: %s\n",
					c.Status.CurrentPrimary, c.Status.CurrentPrimaryTimestamp)

				if c.Status.CurrentPrimary != oldPrimary {
					return true
				} else if c.Status.CurrentPrimaryTimestamp != oldPrimaryTimestamp {
					return true
				}

				return false
			}, timeout, "1s").Should(BeTrue())
		})

		By("verifying that all the standbys streams from the primary", func() {
			// To check this we find the primary and create a table on it.
			// The table should be replicated on the standbys.
			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, upgradeNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			query := "CREATE TABLE IF NOT EXISTS postswitch(i int);"
			_, _, err = exec.EventuallyExecQueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primary.Namespace,
					PodName:   primary.Name,
				}, exec.DatabaseName(databaseName),
				query,
				RetryTimeout,
				PollingTime,
			)
			Expect(err).ToNot(HaveOccurred())

			// Get all pods in the cluster dynamically
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, upgradeNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			for _, pod := range podList.Items {
				Eventually(func() (string, error) {
					out, _, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{
							Namespace: pod.Namespace,
							PodName:   pod.Name,
						},
						exec.DatabaseName(databaseName),
						"SELECT count(*) = 0 FROM postswitch")
					return strings.TrimSpace(out), err
				}, 240).Should(BeEquivalentTo("t"),
					"Pod %v should have followed the new primary", pod.Name)
			}
		})
	}

	// getExecutableHashesFromInstances prints the manager's executable hash of each pod to a given IO writer
	getExecutableHashesFromInstances := func(upgradeNamespace, clusterName string, w io.Writer) error {
		pods, err := clusterutils.ListPods(env.Ctx, env.Client, upgradeNamespace, clusterName)
		if err != nil {
			return err
		}
		for _, pod := range pods.Items {
			status, err := proxy.RetrievePgStatusFromInstance(env.Ctx, env.Interface, pod, true)
			if err != nil {
				continue
			}
			var statusFields map[string]interface{}
			err = json.Unmarshal([]byte(status), &statusFields)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintln(w, "Pod", pod.Name, "executableHash", statusFields["executableHash"])
		}
		return nil
	}

	// assertOnlineManagerRollout checks for the presence of InstanceManagerUpgraded
	// events, which are produced on online upgrades.
	// returns a boolean indicating success
	assertOnlineManagerRollout := func() bool {
		backoffCheckingEvents := wait.Backoff{
			Duration: 10 * time.Second,
			Steps:    5,
		}
		notUpdated := errors.New("notUpdated")
		shouldRetry := func(err error) bool {
			return errors.Is(err, notUpdated)
		}
		err := retry.OnError(backoffCheckingEvents, shouldRetry, func() error {
			eventList := corev1.EventList{}
			err := env.Client.List(env.Ctx,
				&eventList,
				ctrlclient.MatchingFields{
					"involvedObject.kind": "Cluster",
					"involvedObject.name": clusterName1,
				},
			)
			if err != nil {
				return err
			}

			var count, nonUpgradeCount int
			By("checking cluster events for instance manager upgrades")
			for _, event := range eventList.Items {
				if event.Reason == "InstanceManagerUpgraded" {
					count++
					GinkgoWriter.Printf("%d: %s\n", count, event.Message)
				}
				if event.Reason == "InstanceManagerUpgradeFailed" {
					nonUpgradeCount++
					GinkgoWriter.Printf("%d: %s\n", nonUpgradeCount, event.Message)
				}
			}

			if count != 3 {
				return fmt.Errorf("expected 3 online rollouts, but %d happened: %w", count, notUpdated)
			}

			return nil
		})
		return err == nil
	}

	// assertExpectedMatchingPodUIDs checks that the UID of each pod of a Cluster matches with a given list of UIDs.
	// expectedMatches defines how many times, when comparing the elements of the 2 lists, you are expected to have
	// common values
	assertExpectedMatchingPodUIDs := func(
		namespace, clusterName string, podUIDs []types.UID, expectedMatches int,
	) error {
		backoffCheckingPodRestarts := wait.Backoff{
			Duration: 10 * time.Second,
			Steps:    30,
		}
		shouldRetry := func(_ error) bool {
			return true
		}
		err := retry.OnError(backoffCheckingPodRestarts, shouldRetry, func() error {
			var currentUIDs []types.UID
			currentPodList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			if err != nil {
				return err
			}
			for _, pod := range currentPodList.Items {
				currentUIDs = append(currentUIDs, pod.GetUID())
			}
			matchingPods := len(funk.Join(currentUIDs, podUIDs, funk.InnerJoin).([]types.UID))

			if matchingPods != expectedMatches {
				return fmt.Errorf("expected %d pods with unchanged UIDs, found %d", expectedMatches, matchingPods)
			}
			return nil
		})
		return err
	}

	cleanupOperatorAndMinio := func() error {
		GinkgoWriter.Println("cleaning up")
		if CurrentSpecReport().Failed() {
			// Dump the minio namespace when failed
			namespaces.DumpNamespaceObjects(
				env.Ctx, env.Client,
				minioEnv.Namespace, "out/"+CurrentSpecReport().LeafNodeText+"minio.log",
			)
			// Dump the operator namespace, as operator is changing too
			operator.Dump(
				env.Ctx, env.Client,
				operatorNamespace,
				"out/"+CurrentSpecReport().LeafNodeText+"operator.log")
		}

		// Delete the operator's namespace in case that the previous test make corrupted changes to
		// the operator's namespace so that affects subsequent test
		if err := namespaces.DeleteNamespaceAndWait(env.Ctx, env.Client, operatorNamespace, 60); err != nil {
			return fmt.Errorf("could not cleanup, failed to delete operator namespace: %v", err)
		}

		if _, err := minio.CleanFiles(minioEnv, minioPath1); err != nil {
			return fmt.Errorf("encountered an error while cleaning up minio: %v", err)
		}

		if _, err := minio.CleanFiles(minioEnv, minioPath2); err != nil {
			return fmt.Errorf("encountered an error while cleaning up minio: %v", err)
		}

		GinkgoWriter.Println("cleaning up done")
		return nil
	}

	assertCreateNamespace := func(namespacePrefix string) string {
		var namespace string
		By(fmt.Sprintf(
			"having a '%s' upgradeNamespace",
			namespacePrefix), func() {
			var err error
			// Create a upgradeNamespace for all the resources
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			// Creating a upgradeNamespace should be quick
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      namespace,
			}

			Eventually(func() (string, error) {
				namespaceResource := &corev1.Namespace{}
				err := env.Client.Get(env.Ctx, namespacedName, namespaceResource)
				return namespaceResource.GetName(), err
			}, 20).Should(BeEquivalentTo(namespace))
		})
		return namespace
	}

	deployOperator := func(operatorManifestFile string) {
		By(fmt.Sprintf("applying manager manifest %s", operatorManifestFile), func() {
			// Upgrade to the new version
			_, stderr, err := run.Run(
				fmt.Sprintf("kubectl apply --server-side --force-conflicts -f %v", operatorManifestFile))
			Expect(err).NotTo(HaveOccurred(), "stderr: "+stderr)
		})

		By("waiting for the deployment to be rolled out", func() {
			deployment, err := operator.GetDeployment(env.Ctx, env.Client)
			Expect(err).NotTo(HaveOccurred())

			timeout := 240
			Eventually(func() error {
				_, stderr, err := run.Run(fmt.Sprintf(
					"kubectl -n %v rollout status deployment %v -w --timeout=%vs",
					operatorNamespace,
					deployment.Name,
					timeout,
				))
				if err != nil {
					GinkgoWriter.Printf("stderr: %s\n", stderr)
				}

				return err
			}, timeout).ShouldNot(HaveOccurred())
		})
		By("getting the operator info", func() {
			pod, err := operator.GetPod(env.Ctx, env.Client)
			Expect(err).NotTo(HaveOccurred())
			GinkgoWriter.Println("image used for operator", pod.Spec.Containers[0].Image)
		})
	}

	assertClustersWorkAfterOperatorUpgrade := func(upgradeNamespace, operatorManifest string, online bool) {
		databaseName := "appdb"
		// generate random serverNames for the clusters each time
		serverName1 := fmt.Sprintf("%s-%d", clusterName1, funk.RandomInt(0, 9999))
		serverName2 := fmt.Sprintf("%s-%d", clusterName2, funk.RandomInt(0, 9999))
		// Create the secrets used by the clusters and minio
		By("creating the postgres secrets", func() {
			CreateResourceFromFile(upgradeNamespace, pgSecrets)
		})
		By("creating the cloud storage credentials", func() {
			_, err := secrets.CreateObjectStorageSecret(
				env.Ctx,
				env.Client,
				upgradeNamespace,
				"aws-creds",
				"minio",
				"minio123",
			)
			Expect(err).NotTo(HaveOccurred())
		})
		By("create the certificates for MinIO", func() {
			err := minioEnv.CreateCaSecret(env, upgradeNamespace)
			Expect(err).ToNot(HaveOccurred())
		})
		// Create the cluster. Since it will take a while, we'll do more stuff
		// in parallel and check for it to be up later.
		By(fmt.Sprintf("creating a Cluster in the '%v' upgradeNamespace",
			upgradeNamespace), func() {
			// set the serverName to a random name
			err := os.Setenv("SERVER_NAME", serverName1)
			Expect(err).ToNot(HaveOccurred())
			CreateResourceFromFile(upgradeNamespace, sampleFile)

			if online {
				// Upgrading to the new release will trigger a
				// rollout of Pods even if online upgrade is
				// enabled. This happens because of the
				// following PR:
				// https://github.com/cloudnative-pg/cloudnative-pg/pull/5503
				//
				// This E2e would correctly detect that and trigger a failure.
				// To avoid this, just for this release, we disable the pod
				// spec reconciliation.
				// By doing that we don't test that the online upgrade won't
				// trigger any Pod restart. We still test that the operator
				// is upgraded in this case too.
				_, stderr, err := run.Run(
					fmt.Sprintf("kubectl annotate -n %s cluster/%s cnpg.io/reconcilePodSpec=disabled",
						upgradeNamespace, clusterName1))
				Expect(err).NotTo(HaveOccurred(), "stderr: "+stderr)
			}
		})

		// Cluster ready happens after minio is ready
		By("having a Cluster with three instances ready", func() {
			AssertClusterIsReady(upgradeNamespace, clusterName1, testTimeouts[timeouts.ClusterIsReady], env)
		})

		By("creating a Pooler with two instances", func() {
			CreateResourceFromFile(upgradeNamespace, pgBouncerSampleFile)
		})

		// Now that everything is in place, we add a bit of data we'll use to
		// check if the backup is working
		By("creating data on the database", func() {
			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, upgradeNamespace, clusterName1)
			Expect(err).ToNot(HaveOccurred())

			query := "CREATE TABLE IF NOT EXISTS to_restore AS VALUES (1),(2);"
			_, _, err = exec.EventuallyExecQueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primary.Namespace,
					PodName:   primary.Name,
				}, exec.DatabaseName(databaseName),
				query,
				RetryTimeout,
				PollingTime,
			)
			Expect(err).ToNot(HaveOccurred())
		})

		AssertArchiveWalOnMinio(upgradeNamespace, clusterName1, serverName1)

		By("uploading a backup on minio", func() {
			// We create a Backup
			CreateResourceFromFile(upgradeNamespace, backupFile)
		})

		By("verifying that a backup has actually completed", func() {
			backupNamespacedName := types.NamespacedName{
				Namespace: upgradeNamespace,
				Name:      backupName,
			}
			Eventually(func() (apiv1.BackupPhase, error) {
				backup := &apiv1.Backup{}
				err := env.Client.Get(env.Ctx, backupNamespacedName, backup)
				return backup.Status.Phase, err
			}, 200).Should(BeEquivalentTo(apiv1.BackupPhaseCompleted))

			// A file called data.tar.gz should be available on minio
			Eventually(func() (int, error, error) {
				out, _, err := exec.CommandInContainer(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.ContainerLocator{
						Namespace:     minioEnv.Namespace,
						PodName:       minioEnv.Client.Name,
						ContainerName: "mc",
					}, nil,
					"sh", "-c", "mc find minio --name data.tar.gz | wc -l")
				value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
				return value, err, atoiErr
			}, 60).Should(BeEquivalentTo(1))
		})

		By("creating a ScheduledBackup", func() {
			// We create a ScheduledBackup
			CreateResourceFromFile(upgradeNamespace, scheduledBackupFile)
		})
		AssertScheduledBackupsAreScheduled(serverName1)

		assertPGBouncerPodsAreReady(upgradeNamespace, pgBouncerSampleFile, 2)

		podList, err := clusterutils.ListPods(env.Ctx, env.Client, upgradeNamespace, clusterName1)
		Expect(err).ToNot(HaveOccurred())
		podUIDs := make([]types.UID, 0, len(podList.Items))
		for _, pod := range podList.Items {
			podUIDs = append(podUIDs, pod.GetUID())
		}

		By("getting the executable hash from the cluster pods", func() {
			_ = getExecutableHashesFromInstances(upgradeNamespace, clusterName1, GinkgoWriter)
		})

		deployOperator(operatorManifest)

		operatorConfigMapNamespacedName := types.NamespacedName{
			Namespace: operatorNamespace,
			Name:      configName,
		}

		// We need to check here if we were able to upgrade the cluster,
		// be it rolling or online
		// We look for the setting in the operator configMap
		operatorConfigMap := &corev1.ConfigMap{}
		var testOnlineUpgrade, onlineUpgradeDone bool
		err = env.Client.Get(env.Ctx, operatorConfigMapNamespacedName, operatorConfigMap)
		if err != nil || operatorConfigMap.Data["ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES"] == "false" {
			GinkgoWriter.Printf("rolling upgrade\n")
			// Wait for rolling update. We expect all the pods to change UID
			errMatches := assertExpectedMatchingPodUIDs(upgradeNamespace, clusterName1, podUIDs, 0)
			Expect(errMatches).ShouldNot(HaveOccurred(), "No pods should have the same UID they had before the upgrade")
		} else {
			GinkgoWriter.Printf("online upgrade\n")
			testOnlineUpgrade = true
			// Pods shouldn't change and there should be an event
			onlineUpgradeDone = assertOnlineManagerRollout()
			if onlineUpgradeDone {
				GinkgoWriter.Printf("online manager rollout is done\n")
				// equivalent to waiting for 300 sec as before
				err := assertExpectedMatchingPodUIDs(upgradeNamespace, clusterName1, podUIDs, 3)
				if err != nil {
					GinkgoWriter.Printf("online manager rollout not done. Some pods were restarted\n")
					onlineUpgradeDone = false
				}
			} else {
				GinkgoWriter.Printf("online manager rollout not done. Didn't find the expected Events\n")
			}
		}
		AssertClusterIsReady(upgradeNamespace, clusterName1, 300, env)

		// the instance pods should not restart
		By("verifying that the instance pods are not restarted", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, upgradeNamespace, clusterName1)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				Expect(pod.Status.ContainerStatuses).NotTo(BeEmpty())
				Expect(pod.Status.ContainerStatuses[0].RestartCount).To(BeEquivalentTo(0))
			}
		})

		AssertConfUpgrade(clusterName1, upgradeNamespace)

		By("installing a second Cluster on the upgraded operator", func() {
			// set the serverName to a random name
			err := os.Setenv("SERVER_NAME", serverName2)
			Expect(err).ToNot(HaveOccurred())
			CreateResourceFromFile(upgradeNamespace, sampleFile2)
			AssertClusterIsReady(upgradeNamespace, clusterName2, testTimeouts[timeouts.ClusterIsReady], env)
		})

		AssertConfUpgrade(clusterName2, upgradeNamespace)

		// We verify that the backup taken before the upgrade is usable to
		// create a v1 cluster
		By("restoring the backup taken from the first Cluster in a new cluster", func() {
			restoredClusterName := "cluster-restore"
			CreateResourceFromFile(upgradeNamespace, restoreFile)
			AssertClusterIsReady(upgradeNamespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReadySlow],
				env)

			// Test data should be present on restored primary
			primary := restoredClusterName + "-1"
			out, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: upgradeNamespace,
					PodName:   primary,
				},
				exec.DatabaseName(databaseName),
				"SELECT count(*) FROM to_restore")
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))

			// Restored primary should be a timeline higher than 1, because
			// we expect a promotion. We can't enforce "2" because the timeline
			// ID will also depend on the history files existing in the cloud
			// storage and we don't know the status of that.
			out, _, err = exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: upgradeNamespace,
					PodName:   primary,
				},
				exec.DatabaseName(databaseName),
				"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)")
			Expect(err).NotTo(HaveOccurred())
			Expect(strconv.Atoi(strings.Trim(out, "\n"))).To(
				BeNumerically(">", 1))

			// Restored standbys should soon attach themselves to restored primary
			Eventually(func() (string, error) {
				out, _, err = exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: upgradeNamespace,
						PodName:   primary,
					},
					exec.DatabaseName(databaseName),
					"SELECT count(*) FROM pg_catalog.pg_stat_replication")
				return strings.Trim(out, "\n"), err
			}, 180).Should(BeEquivalentTo("2"))
		})
		AssertArchiveWalOnMinio(upgradeNamespace, clusterName1, serverName1)
		AssertScheduledBackupsAreScheduled(serverName1)

		By("scaling down the pooler to 0", func() {
			assertPGBouncerPodsAreReady(upgradeNamespace, pgBouncerSampleFile, 2)
			assertPGBouncerEndpointsContainsPodsIP(upgradeNamespace, pgBouncerSampleFile, 2)

			Eventually(func(g Gomega) {
				pooler := apiv1.Pooler{}
				err := env.Client.Get(env.Ctx,
					ctrlclient.ObjectKey{Namespace: upgradeNamespace, Name: pgBouncerName},
					&pooler)
				g.Expect(err).ToNot(HaveOccurred())

				pooler.Spec.Instances = ptr.To(int32(0))
				err = env.Client.Update(env.Ctx, &pooler)
				g.Expect(err).ToNot(HaveOccurred())
			}).Should(Succeed())

			assertPGBouncerPodsAreReady(upgradeNamespace, pgBouncerSampleFile, 0)
		})

		if testOnlineUpgrade {
			By("checking whether the pods were restarted in an online upgrade", func() {
				if !onlineUpgradeDone {
					_ = getExecutableHashesFromInstances(upgradeNamespace, clusterName1, GinkgoWriter)
					Fail("upgrade was successful, but was not an online upgrade")
				} else {
					Succeed()
				}
			})
		}
	}

	assertManifestPresent := func(path string) {
		const missingManifestsMessage = "MISSING the test operator manifest.\n" +
			"It should have been produced by calling the hack/run-e2e.sh script"
		_, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred(), missingManifestsMessage)
	}

	When("upgrading from the most recent tag to the current operator", func() {
		JustBeforeEach(func() {
			assertManifestPresent(currentOperatorManifest)
		})

		It("keeps clusters working after a rolling upgrade", func() {
			upgradeNamespacePrefix := rollingUpgradeNamespace
			By("applying environment changes for current upgrade to be performed", func() {
				operator.CreateConfigMap(env.Ctx, env.Client, operatorNamespace, configName, false)
			})
			mostRecentTag, err := operator.GetMostRecentReleaseTag("../../releases")
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("installing the recent CNPG tag %s\n", mostRecentTag)
			operator.InstallLatest(env.Client, mostRecentTag)
			DeferCleanup(cleanupOperatorAndMinio)

			upgradeNamespace := assertCreateNamespace(upgradeNamespacePrefix)
			assertClustersWorkAfterOperatorUpgrade(upgradeNamespace, currentOperatorManifest, false)
		})

		It("keeps clusters working after an online upgrade", func() {
			upgradeNamespacePrefix := onlineUpgradeNamespace
			By("applying environment changes for current upgrade to be performed", func() {
				operator.CreateConfigMap(env.Ctx, env.Client, operatorNamespace, configName, true)
			})

			mostRecentTag, err := operator.GetMostRecentReleaseTag("../../releases")
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("installing the recent CNPG tag %s\n", mostRecentTag)
			operator.InstallLatest(env.Client, mostRecentTag)
			DeferCleanup(cleanupOperatorAndMinio)

			upgradeNamespace := assertCreateNamespace(upgradeNamespacePrefix)
			assertClustersWorkAfterOperatorUpgrade(upgradeNamespace, currentOperatorManifest, true)
		})
	})

	When("upgrading from the current operator to a `prime` operator with a new hash", func() {
		JustBeforeEach(func() {
			assertManifestPresent(currentOperatorManifest)
			assertManifestPresent(primeOperatorManifest)
		})

		It("keeps clusters working after an online upgrade", func() {
			upgradeNamespacePrefix := onlineUpgradeNamespace
			By("applying environment changes for current upgrade to be performed", func() {
				operator.CreateConfigMap(env.Ctx, env.Client, operatorNamespace, configName, true)
			})

			GinkgoWriter.Printf("installing the current operator %s\n", currentOperatorManifest)
			deployOperator(currentOperatorManifest)
			DeferCleanup(cleanupOperatorAndMinio)

			upgradeNamespace := assertCreateNamespace(upgradeNamespacePrefix)
			assertClustersWorkAfterOperatorUpgrade(upgradeNamespace, primeOperatorManifest, true)
		})

		It("keeps clusters working after a rolling upgrade", func() {
			upgradeNamespacePrefix := rollingUpgradeNamespace
			By("applying environment changes for current upgrade to be performed", func() {
				operator.CreateConfigMap(env.Ctx, env.Client, operatorNamespace, configName, false)
			})
			GinkgoWriter.Printf("installing the current operator %s\n", currentOperatorManifest)
			deployOperator(currentOperatorManifest)
			DeferCleanup(cleanupOperatorAndMinio)

			upgradeNamespace := assertCreateNamespace(upgradeNamespacePrefix)
			assertClustersWorkAfterOperatorUpgrade(upgradeNamespace, primeOperatorManifest, false)
		})
	})
})
