/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package upgrade

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	apiv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

/*
This test affects the operator itself, so it must be run isolated from the
others.

We test the following:
* A Cluster created with v1alpha1 is moved to v1 without issues. We test this
  changing the configuration. That will also perform a switchover.
* A Backup created with v1alpha1 is moved to v1 and
  can be used to bootstrap a v1 cluster.
* A ScheduledBackup created with v1alpha1 is still scheduled after the upgrade.
* A Cluster with v1alpha1 is created as v1 after the upgrade.
*/

// TODO: this test contains duplicated code from the e2e tests. It should be
// refactored. It also contains duplicated code within itself.

var _ = Describe("Upgrade", func() {
	const operatorNamespace = "postgresql-operator-system"
	const operatorName = "postgresql-operator-controller-manager"
	const operatorUpgradeFile = "./fixtures/current-manifest.yaml"

	const namespace = "operator-upgrade"
	const pgSecrets = "./fixtures/pgsecrets.yaml" //nolint:gosec

	// This cluster is a v1a1 cluster created before the operator upgrade
	const clusterName = "cluster-v1alpha1"
	const sampleFile = "./fixtures/cluster-v1alpha1.yaml"
	const updateConfFile = "./fixtures/conf-update.yaml"

	// This cluster is a v1a1 cluster created after the operator upgrade
	const clusterName2 = "cluster2-v1alpha1"
	const sampleFile2 = "./fixtures/cluster2-v1alpha1.yaml"
	const updateConfFile2 = "./fixtures/conf-update2.yaml"

	const minioSecret = "./fixtures/minio-secret.yaml" //nolint:gosec
	const minioPVCFile = "./fixtures/minio-pvc.yaml"
	const minioDeploymentFile = "./fixtures/minio-deployment.yaml"
	const serviceFile = "./fixtures/minio-service.yaml"
	const clientFile = "./fixtures/minio-client.yaml"
	const minioClientName = "mc"
	const backupName = "cluster-backup"
	const backupFile = "./fixtures/backup-v1alpha1.yaml"
	const restoreFile = "./fixtures/cluster-from-v1alpha1-restore.yaml"
	const scheduledBackupFile = "./fixtures/scheduled-backup.yaml"
	const scheduledBackupName = "scheduled-backup"
	const countBackupsScript = "sh -c 'mc find minio --name data.tar.gz | wc -l'"

	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	AssertAPIChange := func(resourceName string, previousAPI client.Object, currentAPI client.Object) {
		By(fmt.Sprintf("verifying that the both API work for %v", resourceName), func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      resourceName,
			}

			resourcePrevious := previousAPI
			err := env.Client.Get(env.Ctx, namespacedName, resourcePrevious)
			Expect(err).ToNot(HaveOccurred())

			resourceCurrent := currentAPI
			err = env.Client.Get(env.Ctx, namespacedName, resourceCurrent)
			Expect(err).ToNot(HaveOccurred())
		})
	}
	// Check that the amount of backups is increasing on minio.
	// This check relies on the fact that nothing is performing backups
	// but a single scheduled backups during the check
	AssertScheduledBackupsAreScheduled := func() {
		By("verifying scheduled backups are still happening", func() {
			timeout := 120
			out, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				minioClientName,
				countBackupsScript))
			Expect(err).ToNot(HaveOccurred())
			currentBackups, err := strconv.Atoi(strings.Trim(out, "\n"))
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() (int, error) {
				out, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					minioClientName,
					countBackupsScript))
				if err != nil {
					return 0, err
				}
				return strconv.Atoi(strings.Trim(out, "\n"))
			}, timeout).Should(BeNumerically(">", currentBackups))
		})
	}

	AssertConfUpgrade := func(clusterName string, updateConfFile string) {
		By("checking basic functionality performing a configuration upgrade on the cluster", func() {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			// Gather current primary
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
			oldPrimary := cluster.Status.CurrentPrimary
			// Update the configuration. It may take some time after the
			// upgrade for the webhook "mcluster.kb.io" to work and accept
			// the apply
			timeout := 60
			Eventually(func() error {
				stdout, stderr, err := tests.Run("kubectl apply -n " + namespace + " -f " + updateConfFile)
				fmt.Printf("STDOUT: %v\nSTDERR: %v\n ERR: %v", stdout, stderr, err)
				return err
			}, timeout).ShouldNot(HaveOccurred())

			timeout = 300
			commandtimeout := time.Second * 2
			// Check that both parameters have been modified in each pod
			for _, pod := range podList.Items {
				pod := pod // pin the variable
				Eventually(func() (int, error, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &commandtimeout,
						"psql", "-U", "postgres", "-tAc", "show max_replication_slots")
					value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
					return value, err, atoiErr
				}, timeout).Should(BeEquivalentTo(16))

				Eventually(func() (int, error, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &commandtimeout,
						"psql", "-U", "postgres", "-tAc", "show maintenance_work_mem")
					value, atoiErr := strconv.Atoi(strings.Trim(stdout, "MB\n"))
					return value, err, atoiErr
				}, timeout).Should(BeEquivalentTo(128))
			}
			// Check that a switchover happened
			Eventually(func() (string, error) {
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.CurrentPrimary, err
			}, timeout).ShouldNot(BeEquivalentTo(oldPrimary))
		})

		By("verifying that all the standbys streams from the primary", func() {
			// To check this we find the primary an create a table on it.
			// The table should be replicated on the standbys.
			primary, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			commandTimeout := time.Second * 2
			timeout := 120
			_, _, err = env.ExecCommand(env.Ctx, *primary, "postgres", &commandTimeout,
				"psql", "-U", "postgres", "appdb", "-tAc", "CREATE TABLE postswitch(i int)")
			Expect(err).ToNot(HaveOccurred())

			for i := 1; i < 4; i++ {
				podName := fmt.Sprintf("%v-%v", clusterName, i)
				podNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}
				Eventually(func() (string, error) {
					pod := &corev1.Pod{}
					if err := env.Client.Get(env.Ctx, podNamespacedName, pod); err != nil {
						return "", err
					}
					out, _, err := env.ExecCommand(env.Ctx, *pod, "postgres",
						&commandTimeout, "psql", "-U", "postgres", "appdb", "-tAc",
						"SELECT count(*) = 0 FROM postswitch")
					return strings.TrimSpace(out), err
				}, timeout).Should(BeEquivalentTo("t"),
					"Pod %v should have followed the new primary", podName)
			}
		})
	}

	It("works after an upgrade to v1", func() {

		// Create a namespace for all the resources
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		By(fmt.Sprintf("having a %v namespace", namespace), func() {
			// Creating a namespace should be quick
			timeout := 20
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      namespace,
			}

			Eventually(func() (string, error) {
				namespaceResource := &corev1.Namespace{}
				err := env.Client.Get(env.Ctx, namespacedName, namespaceResource)
				return namespaceResource.GetName(), err
			}, timeout).Should(BeEquivalentTo(namespace))
		})

		// Create the secrets used by the clusters and minio
		By("creating the postgres secrets", func() {
			_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
				namespace, pgSecrets))
			Expect(err).ToNot(HaveOccurred())
		})
		By("creating the cloud storage credentials", func() {
			_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
				namespace, minioSecret))
			Expect(err).ToNot(HaveOccurred())
		})

		// Create the cluster. Since it will take a while, we'll do more stuff
		// in parallel and check for it to be up later.
		By(fmt.Sprintf("creating a v1alpha1 Cluster in the %v namespace",
			namespace), func() {
			_, _, err := tests.Run(
				"kubectl create -n " + namespace + " -f " + sampleFile)
			Expect(err).ToNot(HaveOccurred())
		})

		// Create the minio deployment and the client in parallel.
		By("creating minio resources", func() {
			// Create a PVC-based deployment for the minio version
			// minio/minio:RELEASE.2020-04-23T00-58-49Z
			_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
				namespace, minioPVCFile))
			Expect(err).ToNot(HaveOccurred())
			_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
				namespace, minioDeploymentFile))
			Expect(err).ToNot(HaveOccurred())
			_, _, err = tests.Run(fmt.Sprintf(
				"kubectl apply -n %v -f %v",
				namespace, clientFile))
			Expect(err).ToNot(HaveOccurred())
			// Create a minio service
			_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
				namespace, serviceFile))
			Expect(err).ToNot(HaveOccurred())
		})

		By("having a Cluster with three instances ready", func() {
			timeout := 600
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}

			Eventually(func() (int32, error) {
				cluster := &apiv1alpha1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.ReadyInstances, err
			}, timeout).Should(BeEquivalentTo(3))
		})

		// The cluster should be found by the v1alpha1 client and not by the v1 one
		By("verifying cluster is running on v1alpha1", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}

			clusterAlpha := &apiv1alpha1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, clusterAlpha)
			Expect(err).ToNot(HaveOccurred())

			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(err).To(HaveOccurred())
		})

		By("having minio resources ready", func() {
			// Wait for the minio pod to be ready
			timeout := 300
			deploymentName := "minio"
			deploymentNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      deploymentName,
			}
			Eventually(func() (int32, error) {
				deployment := &appsv1.Deployment{}
				err := env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
				return deployment.Status.ReadyReplicas, err
			}, timeout).Should(BeEquivalentTo(1))

			// Wait for the minio client pod to be ready
			timeout = 180
			mcNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      minioClientName,
			}
			Eventually(func() (bool, error) {
				mc := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, mcNamespacedName, mc)
				return utils.IsPodReady(*mc), err
			}, timeout).Should(BeTrue())
		})

		// Now that everything is in place, we add a bit of data we'll use to
		// check if the backup is working
		By("creating data on the database", func() {
			primary := clusterName + "-1"
			cmd := "psql -U postgres appdb -tAc 'CREATE TABLE to_restore AS VALUES (1), (2);'"
			_, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primary,
				cmd))
			Expect(err).ToNot(HaveOccurred())
		})

		// Create a WAL on the primary and check if it arrives on
		// minio within a short time.
		By("archiving WALs on minio", func() {
			primary := clusterName + "-1"
			switchWalCmd := "psql -U postgres appdb -tAc 'CHECKPOINT; SELECT pg_walfile_name(pg_switch_wal())'"
			out, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primary,
				switchWalCmd))
			Expect(err).ToNot(HaveOccurred())
			latestWAL := strings.TrimSpace(out)

			mcName := "mc"
			timeout := 30
			Eventually(func() (int, error, error) {
				// In the fixture WALs are compressed with gzip
				findCmd := fmt.Sprintf(
					"sh -c 'mc find minio --name %v.gz | wc -l'",
					latestWAL)
				out, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					mcName,
					findCmd))

				value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
				return value, err, atoiErr
			}, timeout).Should(BeEquivalentTo(1))
		})

		By("uploading a backup on minio", func() {
			// We create a Backup
			_, _, err := tests.Run(fmt.Sprintf(
				"kubectl apply -n %v -f %v",
				namespace, backupFile))
			Expect(err).ToNot(HaveOccurred())
		})

		By("Verifying that a backup has actually completed", func() {
			timeout := 180
			backupNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      backupName,
			}
			Eventually(func() (apiv1alpha1.BackupPhase, error) {
				backup := &apiv1alpha1.Backup{}
				err := env.Client.Get(env.Ctx, backupNamespacedName, backup)
				return backup.Status.Phase, err
			}, timeout).Should(BeEquivalentTo(apiv1.BackupPhaseCompleted))

			// A file called data.tar.gz should be available on minio
			timeout = 30
			Eventually(func() (int, error, error) {
				out, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					minioClientName,
					countBackupsScript))
				value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
				return value, err, atoiErr
			}, timeout).Should(BeEquivalentTo(1))
		})

		By("creating a ScheduledBackup", func() {
			// We create a ScheduledBackup
			_, _, err := tests.Run(fmt.Sprintf(
				"kubectl apply -n %v -f %v",
				namespace, scheduledBackupFile))
			Expect(err).ToNot(HaveOccurred())
		})
		AssertScheduledBackupsAreScheduled()

		By("upgrading the operator to a version with API v1", func() {
			timeout := 120
			_, _, err := tests.Run(fmt.Sprintf("kubectl apply -f %v", operatorUpgradeFile))
			Expect(err).NotTo(HaveOccurred())
			namespacedName := types.NamespacedName{
				Namespace: operatorNamespace,
				Name:      operatorName,
			}
			// After deploying the new operator, Kubernetes should immediately
			// increase the generation counter. We read it to verify that we
			// have actually performed the deployment.
			Eventually(func() (int64, error) {
				deployment := &appsv1.Deployment{}
				err := env.Client.Get(env.Ctx, namespacedName, deployment)
				return deployment.Status.ObservedGeneration, err
			}, timeout).Should(BeEquivalentTo(2))
			// With the new deployment, a new pod should be started. When it's
			// ready, the old one is removed. We wait for the number of replicas
			// to decrease to 1.
			Eventually(func() (int32, error) {
				deployment := &appsv1.Deployment{}
				err := env.Client.Get(env.Ctx, namespacedName, deployment)
				return deployment.Status.Replicas, err
			}, timeout).Should(BeEquivalentTo(1))
			// For a final check, we verify the pod is ready
			Eventually(func() (int32, error) {
				deployment := &appsv1.Deployment{}
				err := env.Client.Get(env.Ctx, namespacedName, deployment)
				return deployment.Status.ReadyReplicas, err
			}, timeout).Should(BeEquivalentTo(1))
		})

		// The API version should have automatically changed for this cluster
		AssertAPIChange(clusterName, &apiv1alpha1.Cluster{}, &apiv1.Cluster{})

		AssertConfUpgrade(clusterName, updateConfFile)

		By("installing a second v1alpha1 cluster on the upgraded operator", func() {
			_, _, err := tests.Run(
				"kubectl create -n " + namespace + " -f " + sampleFile2)
			Expect(err).ToNot(HaveOccurred())

			timeout := 600
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName2,
			}

			Eventually(func() (int32, error) {
				cluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.ReadyInstances, err
			}, timeout).Should(BeEquivalentTo(3))
		})

		// The API version should have automatically changed for this cluster
		AssertAPIChange(clusterName2, &apiv1alpha1.Cluster{}, &apiv1.Cluster{})

		AssertConfUpgrade(clusterName2, updateConfFile2)

		// The API version should have automatically changed for our Backup
		AssertAPIChange(backupName, &apiv1alpha1.Backup{}, &apiv1.Backup{})

		// We verify that the backup taken before the upgrade is usable to
		// create a v1 cluster
		By("restoring the backup taken from a v1alpha1 cluster in a new cluster", func() {
			restoredClusterName := "cluster-restore"
			_, _, err := tests.Run(fmt.Sprintf(
				"kubectl apply -n %v -f %v",
				namespace, restoreFile))
			Expect(err).ToNot(HaveOccurred())

			timeout := 800
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      restoredClusterName,
			}

			Eventually(func() (int32, error) {
				cluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.ReadyInstances, err
			}, timeout).Should(BeEquivalentTo(3))

			// Test data should be present on restored primary
			primary := restoredClusterName + "-1"
			cmd := "psql -U postgres appdb -tAc 'SELECT count(*) FROM to_restore'"
			out, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primary,
				cmd))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))

			// Restored primary should be a timeline higher than 1, because
			// we expect a promotion. We can't enforce "2" because the timeline
			// ID will also depend on the history files existing in the cloud
			// storage and we don't know the status of that.
			cmd = "psql -U postgres appdb -tAc 'select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)'"
			out, _, err = tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primary,
				cmd))
			Expect(err).NotTo(HaveOccurred())
			Expect(strconv.Atoi(strings.Trim(out, "\n"))).To(
				BeNumerically(">", 1))

			// Restored standbys should be attached to restored primary
			cmd = "psql -U postgres appdb -tAc 'SELECT count(*) FROM pg_stat_replication'"
			out, _, err = tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primary,
				cmd))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
		})

		// The API version should have automatically changed for our ScheduledBackup
		AssertAPIChange(scheduledBackupName, &apiv1alpha1.ScheduledBackup{}, &apiv1.ScheduledBackup{})
		AssertScheduledBackupsAreScheduled()
	})
})
