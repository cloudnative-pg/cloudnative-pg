/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/thoas/go-funk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

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

		// This is a cluster of the previous version, created after the operator upgrade
		clusterName2 = "cluster2"
		sampleFile2  = fixturesDir + "/upgrade/cluster2.yaml.template"

		backupName          = "cluster-backup"
		backupFile          = fixturesDir + "/upgrade/backup1.yaml"
		restoreFile         = fixturesDir + "/upgrade/cluster-restore.yaml.template"
		scheduledBackupFile = fixturesDir + "/upgrade/scheduled-backup.yaml"
		countBackupsScript  = "sh -c 'mc find minio --name data.tar.gz | wc -l'"

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
		misingManifestsMessage := "MISSING the test operator manifests.\n" +
			"They should have been produced by calling the hack/run-e2e.sh script"
		_, err := os.Stat(currentOperatorManifest)
		Expect(err).NotTo(HaveOccurred(), misingManifestsMessage)
		_, err = os.Stat(primeOperatorManifest)
		Expect(err).NotTo(HaveOccurred(), misingManifestsMessage)
	})

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		// Since the 'cnpg-system' namespace is deleted after each spec is completed,
		// we should create it and then create the pull image secret

		err := env.EnsureNamespace(operatorNamespace)
		Expect(err).NotTo(HaveOccurred())

		dockerServer := os.Getenv("DOCKER_SERVER")
		dockerUsername := os.Getenv("DOCKER_USERNAME")
		dockerPassword := os.Getenv("DOCKER_PASSWORD")
		if dockerServer != "" && dockerUsername != "" && dockerPassword != "" {
			_, _, err := testsUtils.Run(fmt.Sprintf(`kubectl -n %v create secret docker-registry
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
	AssertScheduledBackupsAreScheduled := func(upgradeNamespace string) {
		By("verifying scheduled backups are still happening", func() {
			out, _, err := env.ExecCommandInContainer(
				testsUtils.ContainerLocator{
					Namespace:     upgradeNamespace,
					PodName:       minioClientName,
					ContainerName: "mc",
				}, nil,
				"sh", "-c", "mc find minio --name data.tar.gz | wc -l")
			Expect(err).ToNot(HaveOccurred())
			currentBackups, err := strconv.Atoi(strings.Trim(out, "\n"))
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() (int, error) {
				out, _, err := env.ExecCommandInContainer(
					testsUtils.ContainerLocator{
						Namespace:     upgradeNamespace,
						PodName:       minioClientName,
						ContainerName: "mc",
					}, nil,
					"sh", "-c", "mc find minio --name data.tar.gz | wc -l")
				if err != nil {
					return 0, err
				}
				return strconv.Atoi(strings.Trim(out, "\n"))
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
		By("checking basic functionality performing a configuration upgrade on the cluster", func() {
			podList, err := env.GetClusterPodList(upgradeNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			// Gather current primary
			cluster, err := env.GetCluster(upgradeNamespace, clusterName)
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
			commandTimeout := time.Second * 10
			// Check that both parameters have been modified in each pod
			for _, pod := range podList.Items {
				pod := pod // pin the variable
				Eventually(func() (int, error, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
						"psql", "-U", "postgres", "-tAc", "show max_replication_slots")
					value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
					return value, err, atoiErr
				}, timeout).Should(BeEquivalentTo(16),
					"Pod %v should have updated its config", pod.Name)

				Eventually(func() (int, error, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
						"psql", "-U", "postgres", "-tAc", "show maintenance_work_mem")
					value, atoiErr := strconv.Atoi(strings.Trim(stdout, "MB\n"))
					return value, err, atoiErr
				}, timeout).Should(BeEquivalentTo(256),
					"Pod %v should have updated its config", pod.Name)
			}
			// Check that a switchover happened
			Eventually(func() (bool, error) {
				c, err := env.GetCluster(upgradeNamespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				GinkgoWriter.Printf("Current Primary: %s, Current Primary timestamp: %s\n",
					c.Status.CurrentPrimary, c.Status.CurrentPrimaryTimestamp)

				if c.Status.CurrentPrimary != oldPrimary {
					return true, nil
				} else if c.Status.CurrentPrimaryTimestamp != oldPrimaryTimestamp {
					return true, nil
				}

				return false, nil
			}, timeout, "1s").Should(BeTrue())
		})

		By("verifying that all the standbys streams from the primary", func() {
			// To check this we find the primary and create a table on it.
			// The table should be replicated on the standbys.
			primary, err := env.GetClusterPrimary(upgradeNamespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			commandTimeout := time.Second * 10
			query := "CREATE TABLE IF NOT EXISTS postswitch(i int);"
			_, _, err = env.EventuallyExecCommand(env.Ctx, *primary, specs.PostgresContainerName, &commandTimeout,
				"psql", "-U", "postgres", "appdb", "-tAc", query)
			Expect(err).ToNot(HaveOccurred())

			for i := 1; i < 4; i++ {
				podName := fmt.Sprintf("%v-%v", clusterName, i)
				podNamespacedName := types.NamespacedName{
					Namespace: upgradeNamespace,
					Name:      podName,
				}
				Eventually(func() (string, error) {
					pod := &corev1.Pod{}
					if err := env.Client.Get(env.Ctx, podNamespacedName, pod); err != nil {
						return "", err
					}
					out, _, err := env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName,
						&commandTimeout, "psql", "-U", "postgres", "appdb", "-tAc",
						"SELECT count(*) = 0 FROM postswitch")
					return strings.TrimSpace(out), err
				}, 240).Should(BeEquivalentTo("t"),
					"Pod %v should have followed the new primary", podName)
			}
		})
	}

	assertManagerRollout := func() {
		retryCheckingEvents := wait.Backoff{
			Duration: 10 * time.Second,
			Steps:    5,
		}
		notUpdated := errors.New("notUpdated")
		err := retry.OnError(retryCheckingEvents, func(err error) bool {
			return errors.Is(err, notUpdated)
		}, func() error {
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

			var count int
			for _, event := range eventList.Items {
				if event.Reason == "InstanceManagerUpgraded" {
					count++
					GinkgoWriter.Printf("%d: %s\n", count, event.Message)
				}
			}

			if count != 3 {
				return fmt.Errorf("expected 3 online rollouts, but %d happened: %w", count, notUpdated)
			}

			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	}

	cleanupNamespace := func(namespace string) error {
		GinkgoWriter.Println("cleaning up")
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			// Dump the operator namespace, as operator is changing too
			env.DumpOperator(operatorNamespace,
				"out/"+CurrentSpecReport().LeafNodeText+"operator.log")
		}

		err := env.DeleteNamespace(namespace)
		if err != nil {
			return fmt.Errorf("could not cleanup. Failed to delete namespace: %v", err)
		}
		// Delete the operator's namespace in case that the previous test make corrupted changes to
		// the operator's namespace so that affects subsequent test
		return env.DeleteNamespaceAndWait(operatorNamespace, 60)
	}

	assertCreateNamespace := func(namespacePrefix string) string {
		var namespace string
		By(fmt.Sprintf(
			"having a '%s' upgradeNamespace",
			namespacePrefix), func() {
			var err error
			// Create a upgradeNamespace for all the resources
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
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
			_, stderr, err := testsUtils.Run(fmt.Sprintf("kubectl apply -f %v", operatorManifestFile))
			Expect(err).NotTo(HaveOccurred(), "stderr: "+stderr)
		})

		By("waiting for the deployment to be rolled out", func() {
			deployment, err := env.GetOperatorDeployment()
			Expect(err).NotTo(HaveOccurred())

			timeout := 240
			Eventually(func() error {
				_, stderr, err := testsUtils.Run(fmt.Sprintf(
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
	}

	assertClustersWorkAfterOperatorUpgrade := func(upgradeNamespace, operatorManifest string) {
		// Create the secrets used by the clusters and minio
		By("creating the postgres secrets", func() {
			CreateResourceFromFile(upgradeNamespace, pgSecrets)
		})
		By("creating the cloud storage credentials", func() {
			AssertStorageCredentialsAreCreated(upgradeNamespace, "aws-creds", "minio", "minio123")
		})

		// Create the cluster. Since it will take a while, we'll do more stuff
		// in parallel and check for it to be up later.
		By(fmt.Sprintf("creating a Cluster in the '%v' upgradeNamespace",
			upgradeNamespace), func() {
			CreateResourceFromFile(upgradeNamespace, sampleFile)
		})

		By("setting up minio", func() {
			setup, err := testsUtils.MinioDefaultSetup(upgradeNamespace)
			Expect(err).ToNot(HaveOccurred())
			err = testsUtils.InstallMinio(env, setup, uint(testTimeouts[testsUtils.MinioInstallation]))
			Expect(err).ToNot(HaveOccurred())
		})

		// Create the minio client pod and wait for it to be ready.
		// We'll use it to check if everything is archived correctly
		By("setting up minio client pod", func() {
			minioClient := testsUtils.MinioDefaultClient(upgradeNamespace)
			err := testsUtils.PodCreateAndWaitForReady(env, &minioClient, uint(testTimeouts[testsUtils.MinioInstallation]))
			Expect(err).ToNot(HaveOccurred())
		})

		By("having minio resources ready", func() {
			// Wait for the minio pod to be ready
			deploymentName := "minio"
			deploymentNamespacedName := types.NamespacedName{
				Namespace: upgradeNamespace,
				Name:      deploymentName,
			}
			Eventually(func() (int32, error) {
				deployment := &appsv1.Deployment{}
				err := env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
				return deployment.Status.ReadyReplicas, err
			}, 300).Should(BeEquivalentTo(1))

			// Wait for the minio client pod to be ready
			mcNamespacedName := types.NamespacedName{
				Namespace: upgradeNamespace,
				Name:      minioClientName,
			}
			Eventually(func() (bool, error) {
				mc := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, mcNamespacedName, mc)
				return utils.IsPodReady(*mc), err
			}, 180).Should(BeTrue())
		})

		// Cluster ready happens after minio is ready
		By("having a Cluster with three instances ready", func() {
			AssertClusterIsReady(upgradeNamespace, clusterName1, testTimeouts[testsUtils.ClusterIsReady], env)
		})

		By("creating a Pooler with two instances", func() {
			CreateResourceFromFile(upgradeNamespace, pgBouncerSampleFile)
		})

		// Now that everything is in place, we add a bit of data we'll use to
		// check if the backup is working
		By("creating data on the database", func() {
			primary, err := env.GetClusterPrimary(upgradeNamespace, clusterName1)
			Expect(err).ToNot(HaveOccurred())

			commandTimeout := time.Second * 10
			query := "CREATE TABLE IF NOT EXISTS to_restore AS VALUES (1),(2);"
			_, _, err = env.EventuallyExecCommand(env.Ctx, *primary, specs.PostgresContainerName, &commandTimeout,
				"psql", "-U", "postgres", "appdb", "-tAc", query)
			Expect(err).ToNot(HaveOccurred())
		})

		// Create a WAL on the primary and check if it arrives on
		// minio within a short time.
		By("archiving WALs on minio", func() {
			primary := clusterName1 + "-1"
			out, _, err := env.ExecCommandInInstancePod(
				testsUtils.PodLocator{
					Namespace: upgradeNamespace,
					PodName:   primary,
				}, nil,
				"psql", "-U", "postgres", "appdb", "-v", "SHOW_ALL_RESULTS=off", "-tAc",
				"CHECKPOINT; SELECT pg_walfile_name(pg_switch_wal())")
			Expect(err).ToNot(HaveOccurred())
			latestWAL := strings.TrimSpace(out)

			Eventually(func() (int, error, error) {
				// In the fixture WALs are compressed with gzip
				findCmd := fmt.Sprintf(
					"mc find minio --name %v.gz | wc -l",
					latestWAL)
				out, _, err := env.ExecCommandInContainer(
					testsUtils.ContainerLocator{
						Namespace:     upgradeNamespace,
						PodName:       minioClientName,
						ContainerName: "mc",
					}, nil,
					"sh", "-c", findCmd)
				value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
				return value, err, atoiErr
			}, 60).Should(BeEquivalentTo(1))
		})

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
				out, _, err := env.ExecCommandInContainer(
					testsUtils.ContainerLocator{
						Namespace:     upgradeNamespace,
						PodName:       minioClientName,
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
		AssertScheduledBackupsAreScheduled(upgradeNamespace)

		assertPGBouncerPodsAreReady(upgradeNamespace, pgBouncerSampleFile, 2)

		var podUIDs []types.UID
		podList, err := env.GetClusterPodList(upgradeNamespace, clusterName1)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podUIDs = append(podUIDs, pod.GetUID())
		}

		deployOperator(operatorManifest)

		operatorConfigMapNamespacedName := types.NamespacedName{
			Namespace: operatorNamespace,
			Name:      configName,
		}

		// We need to check here if we were able to upgrade the cluster,
		// be it rolling or online
		// We look for the setting in the operator configMap
		operatorConfigMap := &corev1.ConfigMap{}
		err = env.Client.Get(env.Ctx, operatorConfigMapNamespacedName, operatorConfigMap)
		if err != nil || operatorConfigMap.Data["ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES"] == "false" {
			GinkgoWriter.Printf("rolling upgrade\n")
			// Wait for rolling update. We expect all the pods to change UID
			Eventually(func() (int, error) {
				var currentUIDs []types.UID
				currentPodList, err := env.GetClusterPodList(upgradeNamespace, clusterName1)
				if err != nil {
					return 0, err
				}
				if len(currentPodList.Items) != len(podUIDs) {
					return 0, fmt.Errorf("unexpected number of pods. Should have %d, has %d",
						len(podUIDs), len(currentPodList.Items))
				}
				for _, pod := range currentPodList.Items {
					currentUIDs = append(currentUIDs, pod.GetUID())
				}
				return len(funk.Join(currentUIDs, podUIDs, funk.InnerJoin).([]types.UID)), nil
			}, 300).Should(BeEquivalentTo(0), "No pods should have the same UID they had before the upgrade")
		} else {
			GinkgoWriter.Printf("online upgrade\n")
			// Pods shouldn't change and there should be an event
			assertManagerRollout()
			GinkgoWriter.Printf("assertManagerRollout is done\n")
			Eventually(func() (int, error) {
				var currentUIDs []types.UID
				currentPodList, err := env.GetClusterPodList(upgradeNamespace, clusterName1)
				if err != nil {
					return 0, err
				}
				for _, pod := range currentPodList.Items {
					currentUIDs = append(currentUIDs, pod.GetUID())
				}
				return len(funk.Join(currentUIDs, podUIDs, funk.InnerJoin).([]types.UID)), nil
			}, 300).Should(BeEquivalentTo(3))
		}
		AssertClusterIsReady(upgradeNamespace, clusterName1, 300, env)

		// the instance pods should not restart
		By("verifying that the instance pods are not restarted", func() {
			podList, err := env.GetClusterPodList(upgradeNamespace, clusterName1)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				Expect(pod.Status.ContainerStatuses[0].RestartCount).To(BeEquivalentTo(0))
			}
		})

		AssertConfUpgrade(clusterName1, upgradeNamespace)

		By("installing a second Cluster on the upgraded operator", func() {
			CreateResourceFromFile(upgradeNamespace, sampleFile2)
			AssertClusterIsReady(upgradeNamespace, clusterName2, testTimeouts[testsUtils.ClusterIsReady], env)
		})

		AssertConfUpgrade(clusterName2, upgradeNamespace)

		// We verify that the backup taken before the upgrade is usable to
		// create a v1 cluster
		By("restoring the backup taken from the first Cluster in a new cluster", func() {
			restoredClusterName := "cluster-restore"
			CreateResourceFromFile(upgradeNamespace, restoreFile)
			AssertClusterIsReady(upgradeNamespace, restoredClusterName, testTimeouts[testsUtils.ClusterIsReadySlow], env)

			// Test data should be present on restored primary
			primary := restoredClusterName + "-1"
			out, _, err := env.ExecQueryInInstancePod(
				testsUtils.PodLocator{
					Namespace: upgradeNamespace,
					PodName:   primary,
				},
				testsUtils.DatabaseName("appdb"),
				"SELECT count(*) FROM to_restore")
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))

			// Restored primary should be a timeline higher than 1, because
			// we expect a promotion. We can't enforce "2" because the timeline
			// ID will also depend on the history files existing in the cloud
			// storage and we don't know the status of that.
			out, _, err = env.ExecQueryInInstancePod(
				testsUtils.PodLocator{
					Namespace: upgradeNamespace,
					PodName:   primary,
				},
				testsUtils.DatabaseName("appdb"),
				"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)")
			Expect(err).NotTo(HaveOccurred())
			Expect(strconv.Atoi(strings.Trim(out, "\n"))).To(
				BeNumerically(">", 1))

			// Restored standbys should soon attach themselves to restored primary
			Eventually(func() (string, error) {
				out, _, err = env.ExecQueryInInstancePod(
					testsUtils.PodLocator{
						Namespace: upgradeNamespace,
						PodName:   primary,
					},
					testsUtils.DatabaseName("appdb"),
					"SELECT count(*) FROM pg_stat_replication")
				return strings.Trim(out, "\n"), err
			}, 180).Should(BeEquivalentTo("2"))
		})
		AssertScheduledBackupsAreScheduled(upgradeNamespace)

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
	}

	When("upgrading from the most recent tag to the current operator", func() {
		It("keeps clusters working after a rolling upgrade", func() {
			upgradeNamespacePrefix := rollingUpgradeNamespace
			By("applying environment changes for current upgrade to be performed", func() {
				testsUtils.CreateOperatorConfigurationMap(operatorNamespace, configName, false, env)
			})
			mostRecentTag, err := testsUtils.GetMostRecentReleaseTag("../../releases")
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("installing the recent CNPG tag %s\n", mostRecentTag)
			testsUtils.InstallLatestCNPGOperator(mostRecentTag, env)
			upgradeNamespace := assertCreateNamespace(upgradeNamespacePrefix)
			DeferCleanup(cleanupNamespace, upgradeNamespace)

			assertClustersWorkAfterOperatorUpgrade(upgradeNamespace, currentOperatorManifest)
		})

		It("keeps clusters working after an online upgrade", func() {
			upgradeNamespacePrefix := onlineUpgradeNamespace
			By("applying environment changes for current upgrade to be performed", func() {
				testsUtils.CreateOperatorConfigurationMap(operatorNamespace, configName, true, env)
			})

			mostRecentTag, err := testsUtils.GetMostRecentReleaseTag("../../releases")
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("installing the recent CNPG tag %s\n", mostRecentTag)
			testsUtils.InstallLatestCNPGOperator(mostRecentTag, env)

			upgradeNamespace := assertCreateNamespace(upgradeNamespacePrefix)
			DeferCleanup(cleanupNamespace, upgradeNamespace)
			assertClustersWorkAfterOperatorUpgrade(upgradeNamespace, currentOperatorManifest)
			assertManagerRollout()
		})
	})

	When("upgrading from the current operator to a `prime` operator with a new hash", func() {
		It("keeps clusters working after an online upgrade", func() {
			upgradeNamespacePrefix := onlineUpgradeNamespace
			By("applying environment changes for current upgrade to be performed", func() {
				testsUtils.CreateOperatorConfigurationMap(operatorNamespace, configName, true, env)
			})

			GinkgoWriter.Printf("installing the current operator %s\n", currentOperatorManifest)
			deployOperator(currentOperatorManifest)

			upgradeNamespace := assertCreateNamespace(upgradeNamespacePrefix)
			DeferCleanup(cleanupNamespace, upgradeNamespace)

			assertClustersWorkAfterOperatorUpgrade(upgradeNamespace, primeOperatorManifest)
		})

		It("keeps clusters working after a rolling upgrade", func() {
			upgradeNamespacePrefix := rollingUpgradeNamespace
			By("applying environment changes for current upgrade to be performed", func() {
				testsUtils.CreateOperatorConfigurationMap(operatorNamespace, configName, false, env)
			})
			GinkgoWriter.Printf("installing the current operator %s\n", currentOperatorManifest)
			deployOperator(currentOperatorManifest)

			upgradeNamespace := assertCreateNamespace(upgradeNamespacePrefix)
			DeferCleanup(cleanupNamespace, upgradeNamespace)

			assertClustersWorkAfterOperatorUpgrade(upgradeNamespace, primeOperatorManifest)
		})
	})
})
