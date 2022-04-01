/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package e2e

import (
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	devUtils "github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Configuration update", func() {
	const (
		clusterName          = "postgresql-storage-class"
		namespace            = "cluster-update-config-e2e"
		sampleFile           = fixturesDir + "/base/cluster-storage-class.yaml"
		level                = tests.High
		autoVacuumMaxWorkers = 4
	)

	checkErrorOutFixedAndBlockedConfigurationParameter := func(sample string) {
		// Update the configuration
		Eventually(func() error {
			_, _, err := utils.RunUnchecked("kubectl apply -n " + namespace + " -f " + sample)
			return err
			// Expecting an error when a blockedConfigurationParameter is modified
		}, RetryTimeout, PollingTime).ShouldNot(BeNil())

		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		const timeout = 60
		commandTimeout := time.Second * 2
		// Expect other config parameters applied together with a blockedParameter to not have changed
		for idx := range podList.Items {
			pod := podList.Items[idx]
			Eventually(func(g Gomega) int {
				stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-tAc", "show autovacuum_max_workers")
				g.Expect(err).ToNot(HaveOccurred())

				value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
				g.Expect(atoiErr).ToNot(HaveOccurred())

				return value
			}, timeout).ShouldNot(BeEquivalentTo(autoVacuumMaxWorkers))
		}
	}

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("can manage cluster configuration changes", func() {
		commandTimeout := time.Second * 2
		timeout := 60
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("01. reloading Pg when a parameter requiring reload is modified", func() {
			// max_connection increase to 110
			sample := fixturesDir + "/config_update/01-reload.yaml"

			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By("apply configuration update", func() {
				// Update the configuration
				CreateResourceFromFile(namespace, sample)
				AssertPostgresNoPendingRestart(namespace, clusterName, commandTimeout, 300)
			})

			By("verify that work_mem result as expected", func() {
				// Check that the parameter has been modified in every pod
				for _, pod := range podList.Items {
					pod := pod // pin the variable
					Eventually(func() (int, error, error) {
						stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
							"psql", "-U", "postgres", "-tAc", "show work_mem")
						value, atoiErr := strconv.Atoi(strings.Trim(stdout, "MB\n"))
						return value, err, atoiErr
					}, timeout).Should(BeEquivalentTo(8))
				}
			})
		})

		By("02. reloading Pg when pg_hba rules are modified", func() {
			sample := fixturesDir + "/config_update/02-pg_hba_reload.yaml"
			endpointName := clusterName + "-rw"

			// Connection should fail now because we are not supplying a password
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By("verify that connection should failed by default", func() {
				_, _, err := devUtils.ExecCommand(
					env.Ctx,
					env.Interface,
					env.RestClientConfig,
					podList.Items[0],
					specs.PostgresContainerName,
					&commandTimeout,
					"psql", "-U", "postgres", "-h", endpointName, "-tAc", "select 1",
				)
				Expect(err).To(HaveOccurred())
			})

			By("apply configuration update", func() {
				// Update the configuration
				CreateResourceFromFile(namespace, sample)
				AssertPostgresNoPendingRestart(namespace, clusterName, commandTimeout, 300)
			})

			By("verify that connection should success after pg_hba_reload", func() {
				// The new pg_hba rule should be present in every pod
				for _, pod := range podList.Items {
					pod := pod // pin the variable
					Eventually(func() (string, error) {
						stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
							"psql", "-U", "postgres", "-tAc",
							"select count(*) from pg_hba_file_rules where type = 'host' and auth_method = 'trust'")
						return strings.Trim(stdout, "\n"), err
					}, timeout).Should(BeEquivalentTo("1"))
				}
				// The connection should work now
				Eventually(func() (int, error, error) {
					stdout, _, err := env.ExecCommand(env.Ctx, podList.Items[0], specs.PostgresContainerName, &commandTimeout,
						"psql", "-U", "postgres", "-h", endpointName, "-tAc", "select 1")
					value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
					return value, err, atoiErr
				}, timeout).Should(BeEquivalentTo(1))
			})
		})
		// nolint:dupl
		By("03. restarting and switching Pg when a parameter requiring restart is modified", func() {
			sample := fixturesDir + "/config_update/03-restart.yaml"
			timeout := 300

			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
			oldPrimary := cluster.Status.CurrentPrimary

			By("apply configuration update", func() {
				// Update the configuration
				CreateResourceFromFile(namespace, sample)
				AssertPostgresNoPendingRestart(namespace, clusterName, commandTimeout, timeout)
			})

			By("verify that shared_buffers setting changed", func() {
				// Check that the new parameter has been modified in every pod
				for _, pod := range podList.Items {
					pod := pod
					Eventually(func() (int, error, error) {
						stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
							"psql", "-U", "postgres", "-tAc", "show shared_buffers")
						value, atoiErr := strconv.Atoi(strings.Trim(stdout, "MB\n"))
						return value, err, atoiErr
					}, timeout).Should(BeEquivalentTo(256),
						"Pod %v should have updated its configuration", pod.Name)
				}
			})
			By("verify that a switchover happened", func() {
				// Check that a switchover happened
				Eventually(func() (string, error) {
					err := env.Client.Get(env.Ctx, namespacedName, cluster)
					return cluster.Status.CurrentPrimary, err
				}, timeout).ShouldNot(BeEquivalentTo(oldPrimary))
			})
		})
		By("04. restarting and switching Pg when mixed parameters are modified", func() {
			sample := fixturesDir + "/config_update/04-mixed-params.yaml"
			timeout := 300
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
			oldPrimary := cluster.Status.CurrentPrimary

			By("apply configuration update", func() {
				// Update the configuration
				CreateResourceFromFile(namespace, sample)
				AssertPostgresNoPendingRestart(namespace, clusterName, commandTimeout, timeout)
			})

			By("verify that both parameters have been modified in each pod", func() {
				// Check that both parameters have been modified in each pod
				for _, pod := range podList.Items {
					pod := pod // pin the variable
					Eventually(func() (int, error, error) {
						stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
							"psql", "-U", "postgres", "-tAc", "show max_replication_slots")
						value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
						return value, err, atoiErr
					}, timeout).Should(BeEquivalentTo(16))

					Eventually(func() (int, error, error) {
						stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
							"psql", "-U", "postgres", "-tAc", "show maintenance_work_mem")
						value, atoiErr := strconv.Atoi(strings.Trim(stdout, "MB\n"))
						return value, err, atoiErr
					}, timeout).Should(BeEquivalentTo(128))
				}
			})

			By("verify that a switchover happened", func() {
				// Check that a switchover happened
				Eventually(func() (string, error) {
					err := env.Client.Get(env.Ctx, namespacedName, cluster)
					return cluster.Status.CurrentPrimary, err
				}, timeout).ShouldNot(BeEquivalentTo(oldPrimary))
			})
		})

		By("05. Erroring out when a fixedConfigurationParameter is modified", func() {
			sample := fixturesDir + "/config_update/05-fixed-params.yaml"
			checkErrorOutFixedAndBlockedConfigurationParameter(sample)
		})
		By("06. Erroring out when a blockedConfigurationParameter is modified", func() {
			sample := fixturesDir + "/config_update/06-blocked-params.yaml"
			checkErrorOutFixedAndBlockedConfigurationParameter(sample)
		})

		// nolint:dupl
		By("07. restarting and not switching Pg when a hot standby sensible parameter requiring "+
			"to restart first the primary instance is decreased",
			func() {
				// max_connection decrease to 105
				sample := fixturesDir + "/config_update/07-restart-decrease.yaml"
				timeout := 300
				podList, err := env.GetClusterPodList(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				cluster := &apiv1.Cluster{}
				err = env.Client.Get(env.Ctx, namespacedName, cluster)
				Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
				oldPrimary := cluster.Status.CurrentPrimary

				By("apply configuration update", func() {
					// Update the configuration
					CreateResourceFromFile(namespace, sample)
					AssertPostgresNoPendingRestart(namespace, clusterName, commandTimeout, timeout)
				})

				By("verify that max_connections has been decreased in every pod", func() {
					// Check that the new parameter has been modified in every pod
					for _, pod := range podList.Items {
						pod := pod
						Eventually(func() (int, error, error) {
							stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
								"psql", "-U", "postgres", "-tAc", "show max_connections")
							value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
							return value, err, atoiErr
						}, timeout).Should(BeEquivalentTo(105),
							"Pod %v should have updated its configuration", pod.Name)
					}
				})
				By("verify that a switchover not happened", func() {
					// Check that a switchover did not happen
					Eventually(func() (string, error) {
						err := env.Client.Get(env.Ctx, namespacedName, cluster)
						return cluster.Status.CurrentPrimary, err
					}, timeout).Should(BeEquivalentTo(oldPrimary))
				})
			})

		// nolint:dupl
		By("08. restarting and not switching Pg when a hot standby sensible parameter requiring "+
			"to restart first the primary instance is decreased, resetting to the default value",
			func() {
				// max_connection is removed (decrease to default)
				sample := fixturesDir + "/config_update/08-restart-decrease-removing.yaml"
				timeout := 300

				podList, err := env.GetClusterPodList(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				cluster := &apiv1.Cluster{}
				err = env.Client.Get(env.Ctx, namespacedName, cluster)
				Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
				oldPrimary := cluster.Status.CurrentPrimary

				By("apply configuration update", func() {
					// Update the configuration
					CreateResourceFromFile(namespace, sample)
					AssertPostgresNoPendingRestart(namespace, clusterName, commandTimeout, timeout)
				})

				By("verify that the max_connections has been set to default in every pod", func() {
					// Check that the new parameter has been modified in every pod
					for _, pod := range podList.Items {
						pod := pod
						Eventually(func() (int, error, error) {
							stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
								"psql", "-U", "postgres", "-tAc", "show max_connections")
							value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
							return value, err, atoiErr
						}, timeout).Should(BeEquivalentTo(100),
							"Pod %v should have updated its configuration", pod.Name)
					}
				})
				By("verify that a switchover not happened", func() {
					// Check that a switchover did not happen
					Eventually(func() (string, error) {
						err := env.Client.Get(env.Ctx, namespacedName, cluster)
						return cluster.Status.CurrentPrimary, err
					}, timeout).Should(BeEquivalentTo(oldPrimary))
				})
			})
	})
})
