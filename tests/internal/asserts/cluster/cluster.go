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

// Package cluster provides Ginkgo/Gomega assertions that operate on a
// Cluster resource as a whole: create it, observe its readiness, force
// switchovers, count its PVCs, etc.
package cluster

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	webhookv1 "github.com/cloudnative-pg/cloudnative-pg/internal/webhook/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	testsutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/nodes"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	pgutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// AssertSwitchover forces a switchover from the current primary to the
// second pod by name, and waits for the cluster to converge.
func AssertSwitchover(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, clusterName string,
) {
	GinkgoHelper()
	assertSwitchoverWithHistory(env, testTimeouts, namespace, clusterName, false)
}

// AssertSwitchoverOnReplica is the replica-cluster variant of AssertSwitchover:
// it expects the same switchover behaviour but does not require a new
// timeline (history file) to appear.
func AssertSwitchoverOnReplica(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, clusterName string,
) {
	GinkgoHelper()
	assertSwitchoverWithHistory(env, testTimeouts, namespace, clusterName, true)
}

// assertSwitchoverWithHistory does a switchover and waits until the old
// primary is streaming from the new primary. In a primary cluster it
// checks a new timeline was created by watching for history files. In a
// replica cluster, a switchover per se does not switch the timeline.
func assertSwitchoverWithHistory(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, clusterName string,
	isReplica bool,
) {
	var pods []string
	var oldPrimary, targetPrimary string
	var oldPodListLength int

	// First we check that the starting situation is the expected one
	By("checking that CurrentPrimary and TargetPrimary are the same", func() {
		var cluster *apiv1.Cluster

		Eventually(func(g Gomega) {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cluster.Status.CurrentPrimary, err).To(
				BeEquivalentTo(cluster.Status.TargetPrimary),
			)
		}).Should(Succeed())

		oldPrimary = cluster.Status.CurrentPrimary

		// Gather pod names
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		oldPodListLength = len(podList.Items)
		for _, p := range podList.Items {
			pods = append(pods, p.Name)
		}
		sort.Strings(pods)
		// TODO: this algorithm is very naïve, only works if we're lucky and the `-1` instance
		// is the primary and the -2 is the most advanced replica
		Expect(pods[0]).To(BeEquivalentTo(oldPrimary))
		targetPrimary = pods[1]
	})

	By(fmt.Sprintf("setting the TargetPrimary node to trigger a switchover to %s", targetPrimary), func() {
		err := retry.OnError(retry.DefaultBackoff, objects.IsRetryableConflictOrTransientError, func() error {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			cluster.Status.TargetPrimary = targetPrimary
			return env.Client.Status().Update(env.Ctx, cluster)
		})
		Expect(err).ToNot(HaveOccurred())
	})

	By("waiting that the TargetPrimary become also CurrentPrimary", func() {
		Eventually(func(g Gomega) {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cluster.Status.CurrentPrimary).To(BeEquivalentTo(targetPrimary))
		}, testTimeouts[timeouts.NewPrimaryAfterSwitchover]).Should(Succeed())
	})

	By("waiting that the old primary become ready", func() {
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      oldPrimary,
		}
		timeout := 120
		Eventually(func() (bool, error) {
			pod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &pod)
			return utils.IsPodActive(pod) && utils.IsPodReady(pod), err
		}, timeout).Should(BeTrue())
	})

	By("waiting that the old primary become a standby", func() {
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      oldPrimary,
		}
		timeout := 120
		Eventually(func() (bool, error) {
			pod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &pod)
			return specs.IsPodStandby(pod), err
		}, timeout).Should(BeTrue())
	})

	// After we finish the switchover, we should wait for the cluster to be ready
	// otherwise, anyone executing this may not wait and also, the following part of the function
	// may fail because the switchover hasn't properly finish yet.
	AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])

	if !isReplica {
		By("confirming that the all postgres containers have *.history file after switchover", func() {
			timeout := 120

			// Gather pod names
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(len(podList.Items), err).To(BeEquivalentTo(oldPodListLength))
			pods = make([]string, 0, len(podList.Items))
			for _, p := range podList.Items {
				pods = append(pods, p.Name)
			}

			Eventually(func() error {
				count := 0
				for _, pod := range pods {
					out, _, err := exec.CommandInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{
							Namespace: namespace,
							PodName:   pod,
						}, nil, "sh", "-c", "ls $PGDATA/pg_wal/*.history",
					)
					if err != nil {
						return err
					}

					numHistory := len(strings.Split(strings.TrimSpace(out), "\n"))
					GinkgoWriter.Printf("count %d: pod: %s, the number of history file in pg_wal: %d\n", count, pod,
						numHistory)
					count++
					if numHistory > 0 {
						continue
					}

					return errors.New("at least 1 .history file expected but none found")
				}
				return nil
			}, timeout).ShouldNot(HaveOccurred())
		})
	}
}

// AssertCreateCluster creates the cluster from a sample file and verifies
// that the ready pods correspond to the number of Instances in the cluster
// spec. Important: this is not equivalent to "kubectl apply", and is not
// able to apply a patch to an existing object.
func AssertCreateCluster(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, clusterName, sampleFile string,
) {
	GinkgoHelper()
	By(fmt.Sprintf("having a %v namespace", namespace), func() {
		// Creating a namespace should be quick
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      namespace,
		}
		Eventually(func() (string, error) {
			namespaceResource := &corev1.Namespace{}
			err := env.Client.Get(env.Ctx, namespacedName, namespaceResource)
			return namespaceResource.GetName(), err
		}, testTimeouts[timeouts.NamespaceCreation]).Should(BeEquivalentTo(namespace))
	})

	By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
		resources.CreateResourceFromFile(env, namespace, sampleFile)
	})
	// Setting up a cluster with three pods is slow, usually 200-600s
	AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])

	// Verify pod sequentiality on fresh cluster creation
	// This should only be checked here, not in AssertClusterIsReady,
	// because after scale operations or pod deletions, gaps are expected
	cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	assertClusterHasSequentialPods(env, namespace, clusterName, cluster.Spec.Instances)
}

// AssertClusterIsReady checks the cluster has as many pods as in spec, that
// none of them are going to be deleted, and that the status is Healthy.
func AssertClusterIsReady(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	timeout int,
) {
	GinkgoHelper()
	By(fmt.Sprintf("having a Cluster %s with each instance in status ready", clusterName), func() {
		// Eventually the number of ready instances should be equal to the
		// amount of instances defined in the cluster and
		// the cluster status should be in healthy state
		var cluster *apiv1.Cluster

		Eventually(func(g Gomega) {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
		}).Should(Succeed())

		start := time.Now()
		Eventually(func() (string, error) {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			if err != nil {
				return "", err
			}
			if cluster.Spec.Instances == utils.CountReadyPods(podList.Items) {
				for _, pod := range podList.Items {
					if pod.DeletionTimestamp != nil {
						return fmt.Sprintf("Pod '%s' is waiting for deletion", pod.Name), nil
					}
				}
				cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				return cluster.Status.Phase, err
			}
			return fmt.Sprintf("Ready pod is not as expected. Spec Instances: %d, ready pods: %d \n",
				cluster.Spec.Instances,
				utils.CountReadyPods(podList.Items)), nil
		}, timeout, 2).Should(
			BeEquivalentTo(apiv1.PhaseHealthy),
			func() string {
				clusterDump := testsutils.PrintClusterResources(env.Ctx, env.Client, namespace, clusterName)
				kubeNodes, _ := nodes.DescribeKubernetesNodes(env.Ctx, env.Client)
				return fmt.Sprintf("CLUSTER STATE\n%s\n\nK8S NODES\n%s",
					clusterDump, kubeNodes)
			},
		)

		if cluster.Spec.Instances != 1 {
			Eventually(func(g Gomega) {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred(), "cannot get cluster pod list")

				primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred(), "cannot find cluster primary pod")

				replicaNamesList := make([]string, 0, len(podList.Items)-1)
				for _, pod := range podList.Items {
					if pod.Name != primaryPod.Name {
						replicaNamesList = append(replicaNamesList, pq.QuoteLiteral(pod.Name))
					}
				}
				replicaNamesString := strings.Join(replicaNamesList, ",")
				out, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod.Name,
					},
					"postgres",
					fmt.Sprintf("SELECT COUNT(*) FROM pg_catalog.pg_stat_replication WHERE application_name IN (%s)",
						replicaNamesString),
				)
				g.Expect(err).ToNot(HaveOccurred(), "cannot extract the list of streaming replicas")
				g.Expect(strings.TrimSpace(out)).To(BeEquivalentTo(fmt.Sprintf("%d", len(replicaNamesList))))
			}, timeout, 2).Should(Succeed(), "Replicas are attached via streaming connection")
		}
		GinkgoWriter.Println("Cluster ready, took", time.Since(start))
	})
}

// assertClusterHasSequentialPods verifies that pod serial numbers are
// sequential starting from 1. Non-sequential numbering on a freshly created
// cluster suggests pods were recreated during initial creation, indicating
// a potential issue requiring investigation.
func assertClusterHasSequentialPods(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	expectedInstances int,
) {
	podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred(), "failed to list cluster pods")
	Expect(podList.Items).To(HaveLen(expectedInstances),
		"cluster should have exactly %d pods", expectedInstances)

	// Extract pod serial numbers
	podSerials := make([]int, 0, len(podList.Items))
	for _, pod := range podList.Items {
		var serial int
		_, err := fmt.Sscanf(pod.Name, clusterName+"-%d", &serial)
		Expect(err).ToNot(HaveOccurred(),
			"pod name %s should match expected format %s-<number>", pod.Name, clusterName)
		podSerials = append(podSerials, serial)
	}

	// Build expected sequential serials [1, 2, 3, ..., expectedInstances]
	expectedSerials := make([]int, expectedInstances)
	for i := 0; i < expectedInstances; i++ {
		expectedSerials[i] = i + 1
	}

	// Sort actual serials for comparison
	slices.Sort(podSerials)

	// Check if serials are sequential
	if !slices.Equal(podSerials, expectedSerials) {
		// Calculate which serials were skipped
		if len(podSerials) == 0 {
			Fail("No pods found for cluster")
		}
		maxSerial := podSerials[len(podSerials)-1]
		var skippedSerials []int
		serialIdx := 0
		for i := 1; i <= maxSerial; i++ {
			if serialIdx < len(podSerials) && podSerials[serialIdx] == i {
				serialIdx++
			} else {
				skippedSerials = append(skippedSerials, i)
			}
		}

		// Build detailed error message
		errorMsg := fmt.Sprintf(
			"Pod serial numbers are non-sequential on a freshly created cluster.\n"+
				"Expected: %v\n"+
				"Actual:   %v\n",
			expectedSerials, podSerials,
		)

		if len(skippedSerials) > 0 {
			errorMsg += fmt.Sprintf("Skipped serial number(s): %v\n", skippedSerials)
		}

		errorMsg += "\nThis requires investigation to determine the root cause.\n"

		Fail(errorMsg)
	}
}

// AssertClusterDefault validates that the cluster object survives the
// CustomValidator without complaint, i.e. defaults are properly populated.
func AssertClusterDefault(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
) {
	GinkgoHelper()
	By("having a Cluster object populated with default values", func() {
		var cluster *apiv1.Cluster
		Eventually(func(g Gomega) {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
		}).Should(Succeed())

		validator := webhookv1.ClusterCustomValidator{}
		validationWarn, validationErr := validator.ValidateCreate(env.Ctx, cluster)
		Expect(validationErr).ToNot(HaveOccurred())
		// The fixtures used by this test intentionally omit synchronous
		// replication, so the only warning they can legitimately raise is
		// the one about that.
		Expect(validationWarn).To(Or(
			BeEmpty(),
			HaveEach(ContainSubstring("no synchronous replication configured")),
		))
	})
}

// AssertNewPrimary verifies that a new primary was elected and that it
// accepts writes.
func AssertNewPrimary(env *environment.TestingEnvironment, namespace, clusterName, oldPrimary string) {
	GinkgoHelper()
	var newPrimaryPod string
	By(fmt.Sprintf("verifying the new primary pod, oldPrimary is %s", oldPrimary), func() {
		timeout := 120
		var cluster *apiv1.Cluster
		Eventually(func(g Gomega) {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cluster.Status.TargetPrimary).ToNot(Or(
				BeEquivalentTo(oldPrimary),
				BeEquivalentTo(apiv1.PendingFailoverMarker),
			))
		}, timeout).Should(Succeed())
		newPrimary := cluster.Status.TargetPrimary

		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      newPrimary,
		}
		Eventually(func() (bool, error) {
			pod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &pod)
			return specs.IsPodPrimary(pod), err
		}, timeout).Should(BeTrue())
		newPrimaryPod = newPrimary
	})
	By(fmt.Sprintf("verifying write operation on the new primary pod: %s", newPrimaryPod), func() {
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      newPrimaryPod,
		}
		pod := corev1.Pod{}
		err := env.Client.Get(env.Ctx, namespacedName, &pod)
		Expect(err).ToNot(HaveOccurred())
		query := "CREATE TABLE IF NOT EXISTS assert_new_primary(var1 text);"
		_, _, err = exec.EventuallyExecQueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: pod.Namespace,
				PodName:   pod.Name,
			}, pgutils.AppDBName,
			query,
			environment.RetryTimeout,
			objects.PollingTime,
		)
		Expect(err).ToNot(HaveOccurred())
	})
}

// AssertClusterReadinessStatusIsReached waits until the cluster's
// ConditionClusterReady condition reaches the provided status.
func AssertClusterReadinessStatusIsReached(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	conditionStatus apiv1.ConditionStatus,
	timeout int,
) {
	GinkgoHelper()
	By(fmt.Sprintf("waiting for cluster condition status in cluster '%v'", clusterName), func() {
		Eventually(func() (string, error) {
			clusterCondition, err := backups.GetConditionsInClusterStatus(
				env.Ctx, env.Client,
				namespace, clusterName, apiv1.ConditionClusterReady,
			)
			if err != nil {
				return "", err
			}
			return string(clusterCondition.Status), nil
		}, timeout, 2).Should(BeEquivalentTo(conditionStatus))
	})
}

// AssertPVCCount checks that the cluster status reports the expected PVCCount
// and that the PVC list in the namespace also matches that count.
func AssertPVCCount(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	pvcCount, timeout int,
) {
	GinkgoHelper()
	By(fmt.Sprintf("verify cluster %v healthy pvc list", clusterName), func() {
		Eventually(func(g Gomega) {
			cluster, _ := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(cluster.Status.PVCCount).To(BeEquivalentTo(pvcCount))

			pvcList := &corev1.PersistentVolumeClaimList{}
			err := env.Client.List(
				env.Ctx, pvcList, ctrlclient.MatchingLabels{utils.ClusterLabelName: clusterName},
				ctrlclient.InNamespace(namespace),
			)
			g.Expect(err).ToNot(HaveOccurred())

			g.Expect(cluster.Status.PVCCount).To(BeEquivalentTo(len(pvcList.Items)))
		}, timeout, 4).Should(Succeed())
	})
}

// AssertClusterPhaseIsConsistent expects the phase of a cluster to be
// consistent for a given number of seconds.
func AssertClusterPhaseIsConsistent(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	phase []string,
	timeout int,
) {
	GinkgoHelper()
	By(fmt.Sprintf("verifying cluster '%v' phase '%+q' is consistent", clusterName, phase), func() {
		assert := assertPredicateClusterHasPhase(env, namespace, clusterName, phase)
		Consistently(assert, timeout, 2).Should(Succeed())
	})
}

// AssertClusterEventuallyReachesPhase checks the phase of a cluster reaches
// one of the listed phases within the specified timeout.
func AssertClusterEventuallyReachesPhase(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	phase []string,
	timeout int,
) {
	GinkgoHelper()
	By(fmt.Sprintf("verifying cluster '%v' phase should eventually become one of '%+q'", clusterName, phase), func() {
		assert := assertPredicateClusterHasPhase(env, namespace, clusterName, phase)
		Eventually(assert, timeout).Should(Succeed())
	})
}

// assertPredicateClusterHasPhase returns a Gomega predicate that succeeds
// when the Cluster's phase is contained in the given slice of phases.
func assertPredicateClusterHasPhase(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	phase []string,
) func(g Gomega) {
	return func(g Gomega) {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(slices.Contains(phase, cluster.Status.Phase)).To(BeTrue())
	}
}

// AssertPrimaryUpdateMethod verifies that the -rw endpoint points to the
// expected primary, and checks if the new primary is the same as before
// (Restart) or has changed (Switchover).
func AssertPrimaryUpdateMethod(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	oldPrimaryPod *corev1.Pod,
	primaryUpdateMethod apiv1.PrimaryUpdateMethod,
) {
	GinkgoHelper()
	var cluster *apiv1.Cluster
	var err error

	Eventually(func(g Gomega) {
		cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		g.Expect(err).ToNot(HaveOccurred())
		if primaryUpdateMethod == apiv1.PrimaryUpdateMethodSwitchover {
			g.Expect(cluster.Status.CurrentPrimary).ToNot(BeEquivalentTo(oldPrimaryPod.Name))
		} else {
			g.Expect(cluster.Status.CurrentPrimary).To(BeEquivalentTo(oldPrimaryPod.Name))
		}
	}, environment.RetryTimeout).Should(Succeed())

	// Get the new current primary Pod
	currentPrimaryPod, err := podutils.Get(env.Ctx, env.Client, namespace, cluster.Status.CurrentPrimary)
	Expect(err).ToNot(HaveOccurred())

	endpointName := clusterName + "-rw"
	// we give 10 seconds to the apiserver to update the endpoint
	timeout := 10
	Eventually(func() (string, error) {
		endpointSlice, err := testsutils.GetEndpointSliceByServiceName(env.Ctx, env.Client, namespace, endpointName)
		return testsutils.FirstEndpointSliceIP(endpointSlice), err
	}, timeout).Should(BeEquivalentTo(currentPrimaryPod.Status.PodIP))
}

// AssertPluginLoaded waits until the named CNPG-I plugin is reported as
// loaded in the cluster status, with a version, within timeoutSeconds.
func AssertPluginLoaded(
	env *environment.TestingEnvironment,
	namespace, clusterName, pluginName string,
	timeoutSeconds int,
) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		g.Expect(err).ToNot(HaveOccurred())

		var pluginVersion string
		for _, plugin := range cluster.Status.PluginStatus {
			if plugin.Name == pluginName {
				pluginVersion = plugin.Version
			}
		}
		g.Expect(pluginVersion).ToNot(BeEmpty(),
			"the %s plugin is not reported as loaded in the cluster status", pluginName)
	}, timeoutSeconds).Should(Succeed())
}
