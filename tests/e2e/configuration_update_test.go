/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Configuration update", func() {
	const namespace = "cluster-update-config-e2e"
	const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml"
	const clusterName = "postgresql-storage-class"
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

	It("can manage cluster configuration changes", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("reloading Pg when a parameter requring reload is modified", func() {
			sample := fixturesDir + "/config_update/01-reload.yaml"
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			// Update the configuration
			_, _, err = tests.Run("kubectl apply -n " + namespace + " -f " + sample)
			Expect(err).ToNot(HaveOccurred())
			timeout := 60
			commandtimeout := time.Second * 2
			// Check that the parameter has been modified in every pod
			for _, pod := range podList.Items {
				pod := pod // pin the variable
				Eventually(func() (int, error, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &commandtimeout,
						"psql", "-U", "postgres", "-tAc", "show work_mem")
					value, atoiErr := strconv.Atoi(strings.Trim(stdout, "MB\n"))
					return value, err, atoiErr
				}, timeout).Should(BeEquivalentTo(8))
			}
		})
		By("reloading Pg when pg_hba rules are modified", func() {
			sample := fixturesDir + "/config_update/02-pg_hba_reload.yaml"
			endpointName := clusterName + "-rw"
			timeout := 60
			commandtimeout := time.Second * 2
			// Connection should fail now because we are not supplying a password
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			stdout, _, err := env.ExecCommand(env.Ctx, podList.Items[0], "postgres", &commandtimeout,
				"psql", "-U", "postgres", "-h", endpointName, "-tAc", "select 1")
			Expect(err).To(HaveOccurred())
			// Update the configuration
			_, _, err = tests.Run("kubectl apply -n " + namespace + " -f " + sample)
			Expect(err).ToNot(HaveOccurred())
			// The new pg_hba rule should be present in every pod
			for _, pod := range podList.Items {
				pod := pod // pin the variable
				Eventually(func() (string, error) {
					stdout, _, err = env.ExecCommand(env.Ctx, pod, "postgres", &commandtimeout,
						"psql", "-U", "postgres", "-tAc",
						"select count(*) from pg_hba_file_rules where type = 'host' and auth_method = 'trust'")
					return strings.Trim(stdout, "\n"), err
				}, timeout).Should(BeEquivalentTo("1"))
			}
			// The connection should work now
			Eventually(func() (int, error, error) {
				stdout, _, err = env.ExecCommand(env.Ctx, podList.Items[0], "postgres", &commandtimeout,
					"psql", "-U", "postgres", "-h", endpointName, "-tAc", "select 1")
				value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
				return value, err, atoiErr
			}, timeout).Should(BeEquivalentTo(1))
		})
		By("restarting and switching Pg when a parameter requiring restart is modified", func() {
			sample := fixturesDir + "/config_update/03-restart.yaml"
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
			// Update the configuration
			_, _, err = tests.Run("kubectl apply -n " + namespace + " -f " + sample)
			Expect(err).ToNot(HaveOccurred())
			timeout := 300
			commandtimeout := time.Second * 2
			// Check that the new parameter has been modified in every pod
			for _, pod := range podList.Items {
				pod := pod
				Eventually(func() (int, error, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &commandtimeout,
						"psql", "-U", "postgres", "-tAc", "show shared_buffers")
					value, atoiErr := strconv.Atoi(strings.Trim(stdout, "MB\n"))
					return value, err, atoiErr
				}, timeout).Should(BeEquivalentTo(256),
					"Pod %v should have updated its configuration", pod.Name)
			}
			// Check that a switchover happened
			Eventually(func() (string, error) {
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.CurrentPrimary, err
			}, timeout).ShouldNot(BeEquivalentTo(oldPrimary))
		})
		By("restarting and switching Pg when mixed parameters are modified", func() {
			sample := fixturesDir + "/config_update/04-mixed-params.yaml"
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
			// Update the configuration
			_, _, err = tests.Run("kubectl apply -n " + namespace + " -f " + sample)
			Expect(err).ToNot(HaveOccurred())
			timeout := 300
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
		By("Erroring out when a fixedConfigurationParameter is modified", func() {
			sample := fixturesDir + "/config_update/05-fixed-params.yaml"
			// Update the configuration
			_, _, err := tests.RunUnchecked("kubectl apply -n " + namespace + " -f " + sample)
			// Expecting an error when a fixedConfigurationParameter is modified
			Expect(err).To(HaveOccurred())
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			timeout := 60
			commandtimeout := time.Second * 2
			// Expect other config parameters applied together with a fixedParameter to not have changed
			for _, pod := range podList.Items {
				pod := pod // pin the variable
				Eventually(func() (int, error, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &commandtimeout,
						"psql", "-U", "postgres", "-tAc", "show autovacuum_max_workers")
					value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
					return value, err, atoiErr
				}, timeout).ShouldNot(BeEquivalentTo(4))
			}
		})
		By("Erroring out when a blockedConfigurationParameter is modified", func() {
			sample := fixturesDir + "/config_update/06-blocked-params.yaml"
			// Update the configuration
			_, _, err := tests.RunUnchecked("kubectl apply -n " + namespace + " -f " + sample)
			// Expecting an error when a blockedConfigurationParameter is modified
			Expect(err).To(HaveOccurred())
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			timeout := 60
			commandtimeout := time.Second * 2
			// Expect other config parameters applied together with a blockedParameter to not have changed
			for _, pod := range podList.Items {
				pod := pod
				Eventually(func() (int, error, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &commandtimeout,
						"psql", "-U", "postgres", "-tAc", "show autovacuum_max_workers")
					value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
					return value, err, atoiErr
				}, timeout).ShouldNot(BeEquivalentTo(4))
			}
		})
	})

})
