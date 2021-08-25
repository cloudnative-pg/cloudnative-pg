/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup and restore", func() {
	const (
		targetDBOne             = "test"
		targetDBTwo             = "test1"
		targetDBSecret          = "secret_test"
		testTableName           = "test_table"
		customQueriesSampleFile = fixturesDir + "/metrics/custom-queries-with-target-databases.yaml"
		sampleFile              = fixturesDir + "/backup/cluster-with-backup.yaml"
		azureBlobSampleFile     = fixturesDir + "/backup/cluster-with-backup-azure-blob.yaml"
	)

	var namespace, clusterName, restoredClusterName, azStorageAccount, azStorageKey string

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

	Context("using minio as object storage", func() {
		It("restores a backed up cluster", func() {
			namespace = "cluster-backup-minio"
			clusterName = "pg-backup"
			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			By("creating the credentials for minio", func() {
				createStorageCredentials(namespace, "minio", "minio123")
			})
			By("setting up minio", func() {
				installMinio(namespace)
			})
			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly.
			By("setting up minio client pod", func() {
				installMinioClient(namespace)
			})

			// Create ConfigMap and secrets to verify metrics for target database after backup restore
			AssertCustomMetricsConfigMapsSecrets(namespace, customQueriesSampleFile, 1, 1)

			// Create the Cluster
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Create required test data
			CreateTestDataForTargetDB(namespace, clusterName, targetDBOne, testTableName)
			CreateTestDataForTargetDB(namespace, clusterName, targetDBTwo, testTableName)
			CreateTestDataForTargetDB(namespace, clusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, "to_restore")

			// Create a WAL on the primary and check if it arrives on
			// minio within a short time.
			By("archiving WALs and verifying they exist", func() {
				primary := clusterName + "-1"
				switchWalCmd := "psql -U postgres app -tAc 'CHECKPOINT; SELECT pg_walfile_name(pg_switch_wal())'"
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
					out, _, err := tests.RunUnchecked(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						mcName,
						findCmd))

					value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
					return value, err, atoiErr
				}, timeout).Should(BeEquivalentTo(1))
			})

			By("uploading a backup", func() {
				// We create a Backup
				backupFile := fixturesDir + "/backup/backup.yaml"
				executeBackup(namespace, backupFile)

				// A file called data.tar should be available on minio
				mcName := "mc"
				Eventually(func() (int, error, error) {
					findCmd := "sh -c 'mc find minio --name data.tar | wc -l'"
					out, _, err := tests.RunUnchecked(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						mcName,
						findCmd))
					value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
					return value, err, atoiErr
				}, 30).Should(BeEquivalentTo(1))
			})

			// Restore backup in a new cluster
			restoreCluster(namespace)

			AssertMetricsData(namespace, restoredClusterName, targetDBOne, targetDBTwo, targetDBSecret)

			By("scheduling backups", func() {
				backupFile := fixturesDir + "/backup/scheduled-backup.yaml"
				scheduleBackup(namespace, backupFile)

				// After a while we should be able to find two more backups
				mcName := "mc"
				Eventually(func() (int, error) {
					findCmd := "sh -c 'mc find minio --name data.tar | wc -l'"
					out, _, err := tests.RunUnchecked(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						mcName,
						findCmd))
					if err != nil {
						return 0, err
					}
					return strconv.Atoi(strings.Trim(out, "\n"))
				}, 30).Should(BeNumerically(">=", 3))
			})
		})
	})

	Context("using azure blobs as object storage", func() {
		BeforeEach(func() {
			isAKS, err := env.IsAKS()
			Expect(err).ToNot(HaveOccurred())
			if !isAKS {
				Skip("This test is only executed on AKS clusters")
			}
			azStorageAccount = os.Getenv("AZURE_STORAGE_ACCOUNT")
			azStorageKey = os.Getenv("AZURE_STORAGE_KEY")
		})

		It("restores a backed up cluster", func() {
			namespace = "cluster-backup-azure-blob"
			clusterName = "pg-backup-azure-blob"
			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			// The Azure Blob Storage should have been created ad-hoc for the test,
			// we get the credentials from the environment variables as we can't create
			// a fixture for them
			By("creating the Azure Blob Storage credentials",
				func() {
					createStorageCredentials(namespace, azStorageAccount, azStorageKey)
				})

			// Create the Cluster
			AssertCreateCluster(namespace, clusterName, azureBlobSampleFile, env)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, "to_restore")

			// Create a WAL on the primary and check if it arrives on
			// Azure Blob Storage within a short time
			By("archiving WALs and verifying they exist", func() {
				primary := clusterName + "-1"
				switchWalCmd := "psql -U postgres app -tAc 'CHECKPOINT; SELECT pg_walfile_name(pg_switch_wal())'"
				out, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					primary,
					switchWalCmd))
				Expect(err).ToNot(HaveOccurred())

				latestWAL := strings.TrimSpace(out)
				// verifying on blob storage using az
				Eventually(func() (bool, error) {
					// In the fixture WALs are compressed with gzip
					// az command to validate compressed file on blob storage
					cmd := fmt.Sprintf("az storage blob exists --account-name %v "+
						"--account-key %v --container-name %v "+
						"--name %v/wals/0000000100000000/%v.gz", azStorageAccount, azStorageKey, clusterName, clusterName, latestWAL)
					out, _, err := tests.RunUnchecked(cmd)
					return strings.Contains(out, "true"), err
				}, 30).Should(BeTrue())
			})

			By("uploading a backup", func() {
				// We create a Backup
				backupFile := fixturesDir + "/backup/backup-azure-blob.yaml"
				executeBackup(namespace, backupFile)

				// Verifying file called data.tar should be available on Azure blob storage
				Eventually(func() (bool, error) {
					findCmd := fmt.Sprintf("az storage blob list --account-name %v "+
						"--account-key %v "+
						"--container-name %v --query \"[?contains(@.name, \\`data.tar\\`)==\\`true\\`].name\"",
						azStorageAccount, azStorageKey, clusterName)
					out, _, err := tests.RunUnchecked(findCmd)
					return strings.Contains(out, "data.tar"), err
				}, 30).Should(BeTrue())
			})

			// Restore backup in a new cluster
			restoreCluster(namespace)

			By("scheduling backups", func() {
				backupFile := fixturesDir + "/backup/scheduled-backup-azure-blob.yaml"
				scheduleBackup(namespace, backupFile)

				timeout := 480
				Eventually(func() (int, error) {
					findCmd := fmt.Sprintf("az storage blob list --account-name %v  "+
						"--account-key %v  "+
						"--container-name %v --query \"[?contains(@.name, \\`data.tar\\`)==\\`true\\`].name\"",
						azStorageAccount, azStorageKey, clusterName)
					out, _, err := tests.RunUnchecked(findCmd)
					dataCount := strings.Split(out, ",")
					return len(dataCount), err
				}, timeout).Should(BeNumerically(">=", 3))
			})
		})
	})
})

func createStorageCredentials(namespace string, id string, key string) {
	_, _, err := tests.Run(fmt.Sprintf("kubectl create secret generic backup-storage-creds -n %v "+
		"--from-literal=ID=%v "+
		"--from-literal=KEY=%v",
		namespace, id, key))
	Expect(err).ToNot(HaveOccurred())
}

func restoreCluster(namespace string) {
	By("Restoring a backup in a new cluster", func() {
		backupFile := fixturesDir + "/backup/cluster-from-restore.yaml"
		restoredClusterName := "cluster-restore"
		_, _, err := tests.Run(fmt.Sprintf(
			"kubectl apply -n %v -f %v",
			namespace, backupFile))
		Expect(err).ToNot(HaveOccurred())

		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, restoredClusterName, 800, env)

		// Test data should be present on restored primary
		primary := restoredClusterName + "-1"
		postgresLogin := "psql -U postgres app -tAc "

		cmd := postgresLogin + "'SELECT count(*) FROM to_restore'"
		out, _, err := tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			cmd))
		Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))

		// Restored primary should be on timeline 2
		cmd = postgresLogin + "'select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)'"
		out, _, err = tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			cmd))
		Expect(strings.Trim(out, "\n"), err).To(Equal("00000002"))

		// Restored standby should be attached to restored primary
		cmd = postgresLogin + "'SELECT count(*) FROM pg_stat_replication'"
		out, _, err = tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			cmd))
		Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
	})
}

func installMinio(namespace string) {
	// Create a PVC-based deployment for the minio version
	// minio/minio:RELEASE.2020-04-23T00-58-49Z
	minioPVCFile := fixturesDir + "/backup/minio-pvc.yaml"
	minioDeploymentFile := fixturesDir +
		"/backup/minio-deployment.yaml"

	_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, minioPVCFile))
	Expect(err).ToNot(HaveOccurred())
	_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, minioDeploymentFile))
	Expect(err).ToNot(HaveOccurred())

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

	// Create a minio service
	serviceFile := fixturesDir + "/backup/minio-service.yaml"
	_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, serviceFile))
	Expect(err).ToNot(HaveOccurred())
}

func installMinioClient(namespace string) {
	clientFile := fixturesDir + "/backup/minio-client.yaml"
	_, _, err := tests.Run(fmt.Sprintf(
		"kubectl apply -n %v -f %v",
		namespace, clientFile))
	Expect(err).ToNot(HaveOccurred())
	timeout := 180
	mcName := "mc"
	mcNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      mcName,
	}
	Eventually(func() (bool, error) {
		mc := &corev1.Pod{}
		err := env.Client.Get(env.Ctx, mcNamespacedName, mc)
		return utils.IsPodReady(*mc), err
	}, timeout).Should(BeTrue())
}

func executeBackup(namespace string, backupFile string) {
	_, _, err := tests.Run(fmt.Sprintf(
		"kubectl apply -n %v -f %v",
		namespace, backupFile))
	Expect(err).ToNot(HaveOccurred())

	// After a while the Backup should be completed
	timeout := 180
	backupName := "cluster-backup"
	backupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      backupName,
	}
	backup := &apiv1.Backup{}
	// Verifying backup status
	Eventually(func() (apiv1.BackupPhase, error) {
		err := env.Client.Get(env.Ctx, backupNamespacedName, backup)
		return backup.Status.Phase, err
	}, timeout).Should(BeEquivalentTo(apiv1.BackupPhaseCompleted))
	Eventually(func() (string, error) {
		err := env.Client.Get(env.Ctx, backupNamespacedName, backup)
		if err != nil {
			return "", err
		}
		backupStatus := backup.GetStatus()
		return backupStatus.BeginLSN, err
	}, timeout).ShouldNot(BeEmpty())

	backupStatus := backup.GetStatus()
	Expect(backupStatus.BeginWal).NotTo(BeEmpty())
	Expect(backupStatus.EndLSN).NotTo(BeEmpty())
	Expect(backupStatus.EndWal).NotTo(BeEmpty())
}

func scheduleBackup(namespace string, backupYAMLPath string) {
	_, _, err := tests.Run(fmt.Sprintf(
		"kubectl apply -n %v -f %v",
		namespace, backupYAMLPath))
	Expect(err).NotTo(HaveOccurred())

	// We expect the scheduled backup to be scheduled before a
	// timeout
	timeout := 480
	scheduledBackupName := "scheduled-backup"
	scheduledBackupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      scheduledBackupName,
	}

	Eventually(func() (*v1.Time, error) {
		scheduledBackup := &apiv1.ScheduledBackup{}
		err := env.Client.Get(env.Ctx,
			scheduledBackupNamespacedName, scheduledBackup)
		return scheduledBackup.Status.LastScheduleTime, err
	}, timeout).ShouldNot(BeNil())

	// Within a few minutes we should have at least two backups
	Eventually(func() (int, error) {
		// Get all the backups children of the ScheduledBackup
		scheduledBackup := &apiv1.ScheduledBackup{}
		err := env.Client.Get(env.Ctx, scheduledBackupNamespacedName,
			scheduledBackup)
		Expect(err).NotTo(HaveOccurred())
		// Get all the backups children of the ScheduledBackup
		backups := &apiv1.BackupList{}
		err = env.Client.List(env.Ctx, backups,
			ctrlclient.InNamespace(namespace))
		Expect(err).NotTo(HaveOccurred())
		completed := 0
		for _, backup := range backups.Items {
			if strings.HasPrefix(backup.Name, scheduledBackup.Name+"-") &&
				backup.Status.Phase == apiv1.BackupPhaseCompleted {
				completed++
			}
		}
		return completed, nil
	}, timeout).Should(BeNumerically(">=", 2))
}
