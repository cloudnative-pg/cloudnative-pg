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
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	devUtils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Configuration update", Ordered, func() {
	const (
		clusterName          = "postgresql-storage-class"
		namespace            = "cluster-update-config-e2e"
		sampleFile           = fixturesDir + "/base/cluster-storage-class.yaml"
		level                = tests.High
		autoVacuumMaxWorkers = 4
		timeout              = 60
	)
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      clusterName,
	}
	commandTimeout := time.Second * 2

	checkErrorOutFixedAndBlockedConfigurationParameter := func(sampleFile string) {
		// Update the configuration
		Eventually(func() error {
			_, _, err := utils.RunUnchecked("kubectl apply -n " + namespace + " -f " + sampleFile)
			return err
			// Expecting an error when a blockedConfigurationParameter is modified
		}, RetryTimeout, PollingTime).ShouldNot(BeNil())

		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

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

	BeforeAll(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		By("create cluster with default configuration", func() {
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
		})
	})
	AfterAll(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	It("01. reloading Pg when a parameter requiring reload is modified", func() {
		// max_connection increase to 110
		reloadSampleFile := fixturesDir + "/config_update/01-reload.yaml"

		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By("apply configuration update", func() {
			// Update the configuration
			CreateResourceFromFile(namespace, reloadSampleFile)
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

	It("02. reloading Pg when pg_hba rules are modified", func() {
		pgHbaReloadSampleFile := fixturesDir + "/config_update/02-pg_hba_reload.yaml"
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
			CreateResourceFromFile(namespace, pgHbaReloadSampleFile)
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
				stdout, _, err := env.ExecCommand(env.Ctx, podList.Items[0],
					specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-h", endpointName, "-tAc", "select 1")
				value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
				return value, err, atoiErr
			}, timeout).Should(BeEquivalentTo(1))
		})
	})
	// nolint:dupl
	It("03. restarting and switching Pg when a parameter requiring restart is modified", func() {
		restartRequiredSampleFile := fixturesDir + "/config_update/03-restart.yaml"
		timeout := 300

		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		cluster := &apiv1.Cluster{}
		err = env.Client.Get(env.Ctx, namespacedName, cluster)
		Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
		oldPrimary := cluster.Status.CurrentPrimary

		By("apply configuration update", func() {
			// Update the configuration
			CreateResourceFromFile(namespace, restartRequiredSampleFile)
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

	It("04. restarting and switching Pg when mixed parameters are modified", func() {
		mixParamsSampleFile := fixturesDir + "/config_update/04-mixed-params.yaml"
		timeout := 300
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		cluster := &apiv1.Cluster{}
		err = env.Client.Get(env.Ctx, namespacedName, cluster)
		Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
		oldPrimary := cluster.Status.CurrentPrimary

		By("apply configuration update", func() {
			// Update the configuration
			CreateResourceFromFile(namespace, mixParamsSampleFile)
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

	It("05. error out when a fixedConfigurationParameter is modified", func() {
		fixedParamsSampleFile := fixturesDir + "/config_update/05-fixed-params.yaml"
		checkErrorOutFixedAndBlockedConfigurationParameter(fixedParamsSampleFile)
	})

	It("06. error out when a blockedConfigurationParameter is modified", func() {
		blockedParamsSampleFile := fixturesDir + "/config_update/06-blocked-params.yaml"
		checkErrorOutFixedAndBlockedConfigurationParameter(blockedParamsSampleFile)
	})

	// nolint:dupl
	It("07. restarting and not switching Pg when a hot standby sensible parameter requiring "+
		"to restart first the primary instance is decreased",
		func() {
			// max_connection decrease to 105
			restartOnDescreaseSampleFile := fixturesDir + "/config_update/07-restart-decrease.yaml"
			timeout := 300
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
			oldPrimary := cluster.Status.CurrentPrimary

			By("apply configuration update", func() {
				// Update the configuration
				CreateResourceFromFile(namespace, restartOnDescreaseSampleFile)
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
	It("08. restarting and not switching Pg when a hot standby sensible parameter requiring "+
		"to restart first the primary instance is decreased, resetting to the default value",
		func() {
			// max_connection is removed (decrease to default)
			restartOnDecreaseSampleFile := fixturesDir + "/config_update/08-restart-decrease-removing.yaml"
			timeout := 300

			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
			oldPrimary := cluster.Status.CurrentPrimary

			By("apply configuration update", func() {
				// Update the configuration
				CreateResourceFromFile(namespace, restartOnDecreaseSampleFile)
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

var _ = Describe("Configuration update with primaryUpdateMethod", func() {
	const level = tests.High

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("primaryUpdateMethod value set to restart", Ordered, func() {
		clusterFileWithPrimaryUpdateRestart := fixturesDir +
			"/config_update/primary_update_method/primary-update-restart.yaml"
		var namespace, clusterName string
		var sourceClusterNamespacedName *types.NamespacedName

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpClusterEnv(namespace, clusterName,
					"out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

		BeforeAll(func() {
			namespace = "config-change-primary-update-restart"
			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err = env.GetResourceNameFromYAML(clusterFileWithPrimaryUpdateRestart)
			Expect(err).ToNot(HaveOccurred())
			sourceClusterNamespacedName = &types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}

			By("setting up cluster with primaryUpdateMethod value set to restart", func() {
				AssertCreateCluster(namespace, clusterName, clusterFileWithPrimaryUpdateRestart, env)
			})
		})

		AfterAll(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should restart primary in place after increasing config parameter `max_connection` value", func() {
			const (
				maxConnectionParamKey = "max_connections"
			)
			var oldPrimaryPodName string
			var newMaxConnectionsValue int
			var primaryStartTime time.Time

			By("getting old primary info", func() {
				commandTimeout := time.Second * 2
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				oldPrimaryPodName = primaryPodInfo.GetName()
				stdout, _, cmdErr := env.EventuallyExecCommand(env.Ctx, *primaryPodInfo, specs.PostgresContainerName,
					&commandTimeout, "psql", "-U", "postgres", "-tAc",
					"select to_char(pg_postmaster_start_time(), 'YYYY-MM-DD HH24:MI:SS');")
				Expect(cmdErr).ToNot(HaveOccurred())

				primaryStartTime, err = devUtils.ParseTargetTime(nil, strings.Trim(stdout, "\n"))
				Expect(err).NotTo(HaveOccurred())

				stdout, _, cmdErr = env.EventuallyExecCommand(env.Ctx, *primaryPodInfo, specs.PostgresContainerName,
					&commandTimeout, "psql", "-U", "postgres", "-tAc", "show max_connections")
				Expect(cmdErr).ToNot(HaveOccurred())

				v, err := strconv.Atoi(strings.Trim(stdout, "\n"))
				Expect(err).NotTo(HaveOccurred())

				newMaxConnectionsValue = v + 10
			})

			By(fmt.Sprintf("updating max_connection value to %v", newMaxConnectionsValue), func() {
				var cluster apiv1.Cluster
				err := env.Client.Get(env.Ctx, *sourceClusterNamespacedName, &cluster)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				updated.Spec.PostgresConfiguration.Parameters[maxConnectionParamKey] = fmt.Sprintf("%v",
					newMaxConnectionsValue)
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(&cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying the new value for max_connections is updated for all instances", func() {
				podList, err := env.GetClusterPodList(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := time.Second * 2
				for _, pod := range podList.Items {
					Eventually(func() (int, error, error) {
						stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
							"psql", "-U", "postgres", "-tAc", "show max_connections")
						value, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
						return value, err, atoiErr
					}, 180).Should(BeEquivalentTo(newMaxConnectionsValue),
						"Pod %v should have updated its configuration", pod.Name)
				}
			})

			By("verifying the old primary is still the primary", func() {
				cluster := apiv1.Cluster{}
				Eventually(func() (string, error) {
					err := env.Client.Get(env.Ctx, *sourceClusterNamespacedName, &cluster)
					return cluster.Status.CurrentPrimary, err
				}, 60).Should(BeEquivalentTo(oldPrimaryPodName))
			})

			By("verifying that old primary was actually restarted", func() {
				commandTimeout := time.Second * 2
				pod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      oldPrimaryPodName,
				}, &pod)
				Expect(err).ToNot(HaveOccurred())

				// take pg postmaster start time
				stdout, _, cmdErr := env.EventuallyExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-tAc",
					"select to_char(pg_postmaster_start_time(), 'YYYY-MM-DD HH24:MI:SS');")
				Expect(cmdErr).ToNot(HaveOccurred())

				newStartTime, err := devUtils.ParseTargetTime(nil, strings.Trim(stdout, "\n"))
				Expect(err).NotTo(HaveOccurred())

				// verify that pg postmaster start time is greater than currentTimestamp which was taken before restart
				Expect(newStartTime).Should(BeTemporally(">", primaryStartTime))
			})
		})

		It("work_mem config change should not require a restart", func() {
			const expectedNewValueForWorkMem = "10MB"
			commandTimeout := time.Second * 2

			By("updating work mem ", func() {
				var cluster apiv1.Cluster
				err := env.Client.Get(env.Ctx, *sourceClusterNamespacedName, &cluster)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				updated.Spec.PostgresConfiguration.Parameters["work_mem"] = expectedNewValueForWorkMem
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(&cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("verify that work_mem result as expected", func() {
				podList, err := env.GetClusterPodList(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				// Check that the parameter has been modified in every pod
				for _, pod := range podList.Items {
					pod := pod // pin the variable
					Eventually(func() (int, error, error) {
						stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
							"psql", "-U", "postgres", "-tAc", "show work_mem")
						value, atoiErr := strconv.Atoi(strings.Trim(stdout, "MB\n"))
						return value, err, atoiErr
					}, 160).Should(BeEquivalentTo(10))
				}
			})
			AssertPostgresNoPendingRestart(namespace, clusterName, commandTimeout, 120)
		})
	})
})
