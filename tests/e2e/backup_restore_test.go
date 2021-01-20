/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup and restore", func() {

	const namespace = "cluster-backup"
	const sampleFile = fixturesDir + "/backup/cluster-with-backup.yaml"
	const clusterName = "pg-backup"
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
	It("restores a backed up cluster", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		// First we create the secrets for minio
		By("creating the cloud storage credentials", func() {
			secretFile := fixturesDir + "/backup/aws-creds.yaml"
			_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
				namespace, secretFile))
			Expect(err).ToNot(HaveOccurred())
		})

		By("setting up minio to hold the backups", func() {
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
		})

		// Create the minio client pod and wait for it to be ready.
		// We'll use it to check if everything is archived correctly.
		By("setting up minio client pod", func() {
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
		})

		// Create the Cluster
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("creating data on the database", func() {
			primary := clusterName + "-1"
			cmd := "psql -U postgres app -tAc 'CREATE TABLE to_restore AS VALUES (1), (2);'"
			_, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primary,
				cmd))
			Expect(err).ToNot(HaveOccurred())
		})

		// Create a WAL on the lead-master and check if it arrives on
		// minio within a short time.
		By("archiving WALs on minio", func() {
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
			backupFile := fixturesDir + "/backup/backup.yaml"
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
			Eventually(func() (clusterv1alpha1.BackupPhase, error) {
				backup := &clusterv1alpha1.Backup{}
				err := env.Client.Get(env.Ctx, backupNamespacedName, backup)
				return backup.GetStatus().Phase, err
			}, timeout).Should(BeEquivalentTo(clusterv1alpha1.BackupPhaseCompleted))

			// A file called data.tar should be available on minio
			mcName := "mc"
			timeout = 30
			Eventually(func() (int, error, error) {
				findCmd := "sh -c 'mc find minio --name data.tar | wc -l'"
				out, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					mcName,
					findCmd))
				value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
				return value, err, atoiErr
			}, timeout).Should(BeEquivalentTo(1))
		})

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
			cmd := "psql -U postgres app -tAc 'SELECT count(*) FROM to_restore'"
			out, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primary,
				cmd))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))

			// Restored primary should be on timeline 2
			cmd = "psql -U postgres app -tAc 'select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)'"
			out, _, err = tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primary,
				cmd))
			Expect(strings.Trim(out, "\n"), err).To(Equal("00000002"))

			// Restored standby should be attached to restored primary
			cmd = "psql -U postgres app -tAc 'SELECT count(*) FROM pg_stat_replication'"
			out, _, err = tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primary,
				cmd))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
		})

		By("scheduling backups", func() {
			// We create a ScheduledBackup
			backupFile := fixturesDir + "/backup/scheduled-backup.yaml"
			_, _, err := tests.Run(fmt.Sprintf(
				"kubectl apply -n %v -f %v",
				namespace, backupFile))
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
				scheduledBackup := &clusterv1alpha1.ScheduledBackup{}
				err := env.Client.Get(env.Ctx,
					scheduledBackupNamespacedName, scheduledBackup)
				return scheduledBackup.GetStatus().LastScheduleTime, err
			}, timeout).ShouldNot(BeNil())

			// Within a few minutes we should have at least two backups
			Eventually(func() (int, error) {
				// Get all the backups children of the ScheduledBackup
				scheduledBackup := &clusterv1alpha1.ScheduledBackup{}
				err := env.Client.Get(env.Ctx, scheduledBackupNamespacedName,
					scheduledBackup)
				Expect(err).NotTo(HaveOccurred())
				// Get all the backups children of the ScheduledBackup
				backups := &clusterv1alpha1.BackupList{}
				err = env.Client.List(env.Ctx, backups,
					ctrlclient.InNamespace(namespace))
				Expect(err).NotTo(HaveOccurred())
				completed := 0
				for _, backup := range backups.Items {
					for _, owner := range backup.GetObjectMeta().GetOwnerReferences() {
						if owner.Name == scheduledBackup.Name &&
							backup.GetStatus().Phase == clusterv1alpha1.BackupPhaseCompleted {
							completed++
						}
					}
				}
				return completed, nil
			}, timeout).Should(BeNumerically(">=", 2))

			// Two more data.tar files should be on minio
			mcName := "mc"
			timeout = 30
			Eventually(func() (int, error) {
				findCmd := "sh -c 'mc find minio --name data.tar | wc -l'"
				out, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					mcName,
					findCmd))
				if err != nil {
					return 0, err
				}
				return strconv.Atoi(strings.Trim(out, "\n"))
			}, timeout).Should(BeNumerically(">=", 3))
		})
	})
})
