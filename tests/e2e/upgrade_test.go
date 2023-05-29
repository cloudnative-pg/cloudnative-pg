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
This test affects the operator itself, so it must be run isolated from the
others.

We test the following:
* A cluster created with the previous (most recent release tag before the actual one) version
  is moved to the current one.
  We test this changing the configuration. That will also perform a switchover.
* A Backup created with the previous version is moved to the current one and
  can be used to bootstrap a cluster.
* A ScheduledBackup created with the previous version is still scheduled after the upgrade.
* A cluster with the previous version is created as a current version one after the upgrade.
* We reply all the previous tests, but we enable the online upgrade in the final CLuster.
*/

var _ = Describe("Upgrade", Label(tests.LabelUpgrade, tests.LabelNoOpenshift), Ordered, Serial, func() {
	const (
		operatorNamespace   = "cnpg-system"
		configName          = "cnpg-controller-manager-config"
		operatorUpgradeFile = fixturesDir + "/upgrade/current-manifest.yaml"

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
		level               = tests.Lowest
	)

	BeforeEach(func() {
		if os.Getenv("TEST_SKIP_UPGRADE") != "" {
			Skip("Skipping upgrade test because TEST_SKIP_UPGRADE variable is defined")
		}
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// Check that the amount of backups is increasing on minio.
	// This check relies on the fact that nothing is performing backups
	// but a single scheduled backups during the check
	AssertScheduledBackupsAreScheduled := func(upgradeNamespace string) {
		By("verifying scheduled backups are still happening", func() {
			out, _, err := testsUtils.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				upgradeNamespace,
				minioClientName,
				countBackupsScript))
			Expect(err).ToNot(HaveOccurred())
			currentBackups, err := strconv.Atoi(strings.Trim(out, "\n"))
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() (int, error) {
				out, _, err := testsUtils.RunUnchecked(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					upgradeNamespace,
					minioClientName,
					countBackupsScript))
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
			namespacedName := types.NamespacedName{
				Namespace: upgradeNamespace,
				Name:      clusterName,
			}
			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))

			oldPrimary := cluster.Status.CurrentPrimary
			oldPrimaryTimestamp := cluster.Status.CurrentPrimaryTimestamp
			// Update the configuration. It may take some time after the
			// upgrade for the webhook "mcluster.kb.io" to work and accept
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
				c := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, c)
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

	applyUpgrade := func(upgradeNamespace string) {
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
			err := testsUtils.PodCreateAndWaitForReady(env, &minioClient, 240)
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
			out, _, err := testsUtils.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				upgradeNamespace,
				primary,
				"psql -U postgres appdb -v SHOW_ALL_RESULTS=off -tAc 'CHECKPOINT; SELECT pg_walfile_name(pg_switch_wal())'"))
			Expect(err).ToNot(HaveOccurred())
			latestWAL := strings.TrimSpace(out)

			Eventually(func() (int, error, error) {
				// In the fixture WALs are compressed with gzip
				findCmd := fmt.Sprintf(
					"sh -c 'mc find minio --name %v.gz | wc -l'",
					latestWAL)
				out, _, err := testsUtils.RunUnchecked(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					upgradeNamespace,
					minioClientName,
					findCmd))

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
				out, _, err := testsUtils.RunUnchecked(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					upgradeNamespace,
					minioClientName,
					countBackupsScript))
				value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
				return value, err, atoiErr
			}, 60).Should(BeEquivalentTo(1))
		})

		By("creating a ScheduledBackup", func() {
			// We create a ScheduledBackup
			CreateResourceFromFile(upgradeNamespace, scheduledBackupFile)
		})
		AssertScheduledBackupsAreScheduled(upgradeNamespace)

		var podUIDs []types.UID
		podList, err := env.GetClusterPodList(upgradeNamespace, clusterName1)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podUIDs = append(podUIDs, pod.GetUID())
		}

		By("upgrading the operator to current version", func() {
			timeout := 120
			// Upgrade to the new version
			_, _, err := testsUtils.Run(fmt.Sprintf("kubectl apply -f %v", operatorUpgradeFile))
			Expect(err).NotTo(HaveOccurred())
			// With the new deployment, a new pod should be started. When it's
			// ready, the old one is removed. We wait for the number of replicas
			// to decrease to 1.
			Eventually(func() (int32, error) {
				deployment, err := env.GetOperatorDeployment()
				if err != nil {
					return 0, err
				}
				return deployment.Status.Replicas, err
			}, timeout).Should(BeEquivalentTo(1))
			// For a final check, we verify the pod is ready
			Eventually(func() (int32, error) {
				deployment, err := env.GetOperatorDeployment()
				if err != nil {
					return 0, err
				}
				return deployment.Status.ReadyReplicas, err
			}, timeout).Should(BeEquivalentTo(1))
		})

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
				for _, pod := range currentPodList.Items {
					currentUIDs = append(currentUIDs, pod.GetUID())
				}
				return len(funk.Join(currentUIDs, podUIDs, funk.InnerJoin).([]types.UID)), nil
			}, 300).Should(BeEquivalentTo(0))
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
			cmd := "psql -U postgres appdb -tAc 'SELECT count(*) FROM to_restore'"
			out, _, err := testsUtils.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				upgradeNamespace,
				primary,
				cmd))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))

			// Restored primary should be a timeline higher than 1, because
			// we expect a promotion. We can't enforce "2" because the timeline
			// ID will also depend on the history files existing in the cloud
			// storage and we don't know the status of that.
			cmd = "psql -U postgres appdb -tAc 'select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)'"
			out, _, err = testsUtils.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				upgradeNamespace,
				primary,
				cmd))
			Expect(err).NotTo(HaveOccurred())
			Expect(strconv.Atoi(strings.Trim(out, "\n"))).To(
				BeNumerically(">", 1))

			// Restored standbys should soon attach themselves to restored primary
			Eventually(func() (string, error) {
				cmd = "psql -U postgres appdb -tAc 'SELECT count(*) FROM pg_stat_replication'"
				out, _, err = testsUtils.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					upgradeNamespace,
					primary,
					cmd))
				return strings.Trim(out, "\n"), err
			}, 180).Should(BeEquivalentTo("2"))
		})
		AssertScheduledBackupsAreScheduled(upgradeNamespace)
	}

	It("works after an upgrade with rolling upgrade ", func() {
		// set upgradeNamespace for log naming
		upgradeNamespacePrefix := rollingUpgradeNamespace
		mostRecentTag, err := testsUtils.GetMostRecentReleaseTag("../../releases")
		Expect(err).NotTo(HaveOccurred())

		GinkgoWriter.Printf("installing the recent CNPG tag %s\n", mostRecentTag)
		testsUtils.InstallLatestCNPGOperator(mostRecentTag, env)
		upgradeNamespace := assertCreateNamespace(upgradeNamespacePrefix)
		DeferCleanup(cleanupNamespace, upgradeNamespace)
		applyUpgrade(upgradeNamespace)
	})

	It("works after an upgrade with online upgrade", func() {
		// set upgradeNamespace for log naming
		upgradeNamespacePrefix := onlineUpgradeNamespace
		By("applying environment changes for current upgrade to be performed", func() {
			testsUtils.EnableOnlineUpgradeForInstanceManager(operatorNamespace, configName, env)
		})

		mostRecentTag, err := testsUtils.GetMostRecentReleaseTag("../../releases")
		Expect(err).NotTo(HaveOccurred())

		GinkgoWriter.Printf("installing the recent CNPG tag %s\n", mostRecentTag)
		testsUtils.InstallLatestCNPGOperator(mostRecentTag, env)

		upgradeNamespace := assertCreateNamespace(upgradeNamespacePrefix)
		DeferCleanup(cleanupNamespace, upgradeNamespace)
		applyUpgrade(upgradeNamespace)

		assertManagerRollout()
	})
})
