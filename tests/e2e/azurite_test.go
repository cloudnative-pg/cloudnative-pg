/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup and restore with Azurite", func() {
	const (
		targetDBOne   = "test"
		testTableName = "test_table"
		namespace     = "azurite-test"
		sampleFile    = fixturesDir + "/azurite/cluster-backup.yaml"
		clusterName   = "pg-backup"
	)

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
		By("creating the Azurite storage credentials", func() {
			secretFile := fixturesDir + "/azurite/azurite-secret.yaml"
			_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
				namespace, secretFile))
			Expect(err).ToNot(HaveOccurred())
		})

		By("setting up Azurite to hold the backups", func() {
			azuriteDeploymentFile := fixturesDir +
				"/azurite/azurite-deployment.yaml"
			_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
				namespace, azuriteDeploymentFile))
			Expect(err).ToNot(HaveOccurred())

			// Wait for Azurite to be ready
			_, _, err = tests.Run(fmt.Sprintf(
				"kubectl wait --for=condition=Ready -n %v -l app=azurite pod",
				namespace))
			Expect(err).ToNot(HaveOccurred())

			// Create a minio service
			serviceFile := fixturesDir + "/azurite/azurite-service.yaml"
			_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
				namespace, serviceFile))
			Expect(err).ToNot(HaveOccurred())
		})

		// Create the Cluster
		AssertCreateCluster(namespace, clusterName, sampleFile, env)
		CreateTestDataForTargetDB(namespace, clusterName, targetDBOne, testTableName)

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

		By("uploading a backup on Azurite", func() {
			// We create a Backup
			backupFile := fixturesDir + "/azurite/backup.yaml"
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
		})

		By("Restoring a backup in a new cluster", func() {
			backupFile := fixturesDir + "/azurite/cluster-from-restore.yaml"
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
		})
	})
})
