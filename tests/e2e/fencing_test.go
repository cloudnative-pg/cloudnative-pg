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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fencing", Label(tests.LabelPlugin), func() {
	const (
		sampleFile = fixturesDir + "/base/cluster-storage-class.yaml.template"
		level      = tests.Medium
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	var namespace, clusterName string
	var pod corev1.Pod

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	checkInstanceStatusReadyOrNot := func(instanceName, namespace string, isReady bool) {
		var pod corev1.Pod
		Eventually(func() (bool, error) {
			err := env.Client.Get(env.Ctx,
				ctrlclient.ObjectKey{Namespace: namespace, Name: instanceName},
				&pod)
			if err != nil {
				return false, err
			}
			for _, podInfo := range pod.Status.ContainerStatuses {
				if podInfo.Name == specs.PostgresContainerName {
					if podInfo.Ready == isReady {
						return true, nil
					}
				}
			}
			return false, nil
		}, 120, 5).Should(BeTrue())
	}

	checkInstanceIsStreaming := func(instanceName, namespace string) {
		timeout := time.Second * 10
		Eventually(func() (int, error) {
			err := env.Client.Get(env.Ctx,
				ctrlclient.ObjectKey{Namespace: namespace, Name: instanceName},
				&pod)
			if err != nil {
				return 0, err
			}
			out, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &timeout,
				"psql", "-U", "postgres", "-tAc", "SELECT count(*) FROM pg_stat_wal_receiver")
			if err != nil {
				return 0, err
			}
			value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
			return value, atoiErr
		}, 120).Should(BeEquivalentTo(1))
	}

	checkPostgresConnection := func(podName, namespace string) {
		err := testUtils.GetObject(env, ctrlclient.ObjectKey{Namespace: namespace, Name: podName}, &pod)
		Expect(err).ToNot(HaveOccurred())
		timeout := time.Second * 10
		dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v sslmode=require",
			testUtils.PGLocalSocketDir, "postgres", "postgres", "")
		stdOut, stdErr, err := utils.ExecCommand(env.Ctx, env.Interface, env.RestClientConfig, pod,
			specs.PostgresContainerName, &timeout,
			"psql", dsn, "-tAc", "SELECT 1")
		Expect(err).To(HaveOccurred(), stdErr, stdOut)
	}

	checkFencingAnnotationSet := func(fencingMethod testUtils.FencingMethod, content []string) {
		if fencingMethod != testUtils.UsingAnnotation {
			return
		}
		By("checking the cluster has the expected annotation set", func() {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
			if len(content) == 0 {
				Expect(cluster.Annotations).To(Or(Not(HaveKey(utils.FencedInstanceAnnotation)),
					HaveKeyWithValue(utils.FencedInstanceAnnotation, "")))
				return
			}
			fencedInstances := make([]string, 0, len(content))
			Expect(json.Unmarshal([]byte(cluster.Annotations[utils.FencedInstanceAnnotation]), &fencedInstances)).
				NotTo(HaveOccurred())
			Expect(fencedInstances).To(BeEquivalentTo(content))
		})
	}

	assertFencingPrimaryWorks := func(fencingMethod testUtils.FencingMethod) {
		It("can fence a primary instance", func() {
			var beforeFencingPodName string
			By("fencing the primary instance", func() {
				primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				beforeFencingPodName = primaryPod.GetName()
				Expect(testUtils.FencingOn(env, beforeFencingPodName,
					namespace, clusterName, fencingMethod)).Should(Succeed())
			})
			By("check the instance is not ready, but kept as primary instance", func() {
				checkInstanceStatusReadyOrNot(beforeFencingPodName, namespace, false)
				currentPrimaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(beforeFencingPodName).To(Equal(currentPrimaryPodInfo.GetName()))
			})
			checkFencingAnnotationSet(fencingMethod, []string{beforeFencingPodName})

			By("check postgres connection on primary", func() {
				checkPostgresConnection(beforeFencingPodName, namespace)
			})
			By("lift the fencing", func() {
				Expect(testUtils.FencingOff(env, beforeFencingPodName,
					namespace, clusterName, fencingMethod)).ToNot(HaveOccurred())
			})
			By("the old primary becomes ready", func() {
				checkInstanceStatusReadyOrNot(beforeFencingPodName, namespace, true)
			})
			By("the old primary should still be the primary instance", func() {
				currentPrimaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(beforeFencingPodName).Should(BeEquivalentTo(currentPrimaryPodInfo.GetName()))
			})
			By("all followers should be streaming again from the primary instance", func() {
				AssertClusterStandbysAreStreaming(namespace, clusterName, 120)
			})
			checkFencingAnnotationSet(fencingMethod, nil)
		})
	}
	assertFencingFollowerWorks := func(fencingMethod testUtils.FencingMethod) {
		It("can fence a follower instance", func() {
			var beforeFencingPodName string
			AssertClusterIsReady(namespace, clusterName, testTimeouts[testUtils.ClusterIsReadyQuick], env)
			By("fence a follower instance", func() {
				podList, _ := env.GetClusterPodList(namespace, clusterName)
				Expect(len(podList.Items)).To(BeEquivalentTo(3))
				for _, pod := range podList.Items {
					if specs.IsPodStandby(pod) {
						beforeFencingPodName = pod.Name
						break
					}
				}
				Expect(beforeFencingPodName).ToNot(BeEmpty())
				Expect(testUtils.FencingOn(env, beforeFencingPodName,
					namespace, clusterName, fencingMethod)).ToNot(HaveOccurred())
			})
			checkFencingAnnotationSet(fencingMethod, []string{beforeFencingPodName})

			By("check the instance is not ready", func() {
				checkInstanceStatusReadyOrNot(beforeFencingPodName, namespace, false)
			})
			By("check postgres connection follower instance", func() {
				checkPostgresConnection(beforeFencingPodName, namespace)
			})
			By("lift the fencing", func() {
				Expect(testUtils.FencingOff(env, beforeFencingPodName,
					namespace, clusterName, fencingMethod)).ToNot(HaveOccurred())
			})
			By("the instance becomes ready", func() {
				checkInstanceStatusReadyOrNot(beforeFencingPodName, namespace, true)
			})
			By("the instance is streaming again from the primary", func() {
				checkInstanceIsStreaming(beforeFencingPodName, namespace)
			})
			checkFencingAnnotationSet(fencingMethod, nil)
		})
	}
	assertFencingClusterWorks := func(fencingMethod testUtils.FencingMethod) {
		It("can fence all the instances in a cluster", func() {
			primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			primaryPodName := primaryPod.GetName()
			By("fence the whole cluster using \"(*)\"", func() {
				Expect(testUtils.FencingOn(env, "*", namespace, clusterName, fencingMethod)).ToNot(HaveOccurred())
			})
			checkFencingAnnotationSet(fencingMethod, []string{"*"})
			By("check all instances are not ready", func() {
				podList, err := env.GetClusterPodList(namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())
				for _, pod := range podList.Items {
					checkInstanceStatusReadyOrNot(pod.GetName(), namespace, false)
				}
			})
			By("check postgres connection on all instances", func() {
				podList, err := env.GetClusterPodList(namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())
				for _, pod := range podList.Items {
					checkPostgresConnection(pod.GetName(), namespace)
				}
			})
			By("lift the fencing", func() {
				Expect(testUtils.FencingOff(env, "*", namespace, clusterName, fencingMethod)).ToNot(HaveOccurred())
			})
			By("all instances become ready", func() {
				podList, err := env.GetClusterPodList(namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())
				for _, pod := range podList.Items {
					checkInstanceStatusReadyOrNot(pod.GetName(), namespace, true)
				}
			})
			By("the old primary is still the primary instance", func() {
				podName, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(primaryPodName).Should(BeEquivalentTo(podName.GetName()))
			})
			By("cluster functionality are back", func() {
				AssertClusterIsReady(namespace, clusterName, 30, env)
			})
			checkFencingAnnotationSet(fencingMethod, nil)
		})
	}

	Context("using kubectl-cnpg plugin", Ordered, func() {
		var err error
		BeforeAll(func() {
			const namespacePrefix = "fencing-using-plugin"
			clusterName, err = env.GetResourceNameFromYAML(sampleFile)
			Expect(err).ToNot(HaveOccurred())
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
		})
		assertFencingPrimaryWorks(testUtils.UsingPlugin)
		assertFencingFollowerWorks(testUtils.UsingPlugin)
		assertFencingClusterWorks(testUtils.UsingPlugin)
	})

	Context("using annotation", Ordered, func() {
		var err error
		BeforeAll(func() {
			const namespacePrefix = "fencing-using-annotation"
			clusterName, err = env.GetResourceNameFromYAML(sampleFile)
			Expect(err).ToNot(HaveOccurred())
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
		})
		assertFencingPrimaryWorks(testUtils.UsingAnnotation)
		assertFencingFollowerWorks(testUtils.UsingAnnotation)
		assertFencingClusterWorks(testUtils.UsingAnnotation)
	})
})
