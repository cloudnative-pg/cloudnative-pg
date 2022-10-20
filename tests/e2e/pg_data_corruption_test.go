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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PGDATA Corruption", func() {
	const (
		namespace   = "pg-data-corruption"
		sampleFile  = fixturesDir + "/pg_data_corruption/cluster-pg-data-corruption.yaml.template"
		clusterName = "cluster-pg-data-corruption"
		level       = tests.Medium
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	It("cluster can be recovered after pgdata corruption on primary", func() {
		var oldPrimaryPodName, oldPrimaryPVCName string
		var oldPrimaryPodInfo, newPrimaryPodInfo *corev1.Pod
		var err error
		tableName := "test_pg_data_corruption"
		err = env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})
		AssertCreateCluster(namespace, clusterName, sampleFile, env)
		AssertCreateTestData(namespace, clusterName, tableName)
		By("gather current primary pod and pvc info", func() {
			oldPrimaryPodInfo, err = env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			oldPrimaryPodName = oldPrimaryPodInfo.GetName()
			// Get the PVC related to the pod
			pvcName := oldPrimaryPodInfo.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
			pvc := &corev1.PersistentVolumeClaim{}
			namespacedPVCName := types.NamespacedName{
				Namespace: namespace,
				Name:      pvcName,
			}
			err = env.Client.Get(env.Ctx, namespacedPVCName, pvc)
			Expect(err).ToNot(HaveOccurred())
			oldPrimaryPVCName = pvc.GetName()
		})
		By("corrupting primary pod by removing pg data", func() {
			cmd := fmt.Sprintf("kubectl exec %v -n %v postgres -- /bin/bash -c 'rm -fr %v/base/*'",
				oldPrimaryPodInfo.GetName(), namespace, specs.PgDataPath)
			_, _, err = testsUtils.Run(cmd)
			Expect(err).ToNot(HaveOccurred())
		})
		By("verify failover after primary pod pg data corruption", func() {
			// check operator will perform a failover
			Eventually(func() string {
				newPrimaryPodInfo, err = env.GetClusterPrimary(namespace, clusterName)
				if err != nil {
					return ""
				}
				return newPrimaryPodInfo.GetName()
			}, 120, 5).ShouldNot(BeEquivalentTo(oldPrimaryPodName),
				"operator did not perform the failover")
		})
		By("verify the old primary pod health", func() {
			// old primary get restarted check that
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      oldPrimaryPodName,
			}
			pod := &corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, pod)
			Expect(err).ToNot(HaveOccurred())
			// The pod should be restarted and the count of the restarts should greater than 0
			Eventually(func() (int32, error) {
				pod := &corev1.Pod{}
				if err := env.Client.Get(env.Ctx, namespacedName, pod); err != nil {
					return 0, err
				}
				for _, data := range pod.Status.ContainerStatuses {
					if data.Name == specs.PostgresContainerName {
						return data.RestartCount, nil
					}
				}
				return int32(-1), nil
			}, 120).Should(BeNumerically(">", 0))
		})
		By("removing old primary pod and attached pvc", func() {
			// Check if walStorage is enabled
			walStorageEnabled, err := testsUtils.IsWalStorageEnabled(namespace, clusterName, env)
			Expect(err).ToNot(HaveOccurred())

			// removing old primary pod attached pvc
			_, _, err = testsUtils.Run(
				fmt.Sprintf("kubectl delete pvc %v -n %v --wait=false", oldPrimaryPVCName, namespace))
			Expect(err).ToNot(HaveOccurred())

			// removing WalStorage PVC if needed
			if walStorageEnabled {
				_, _, err = testsUtils.Run(
					fmt.Sprintf("kubectl delete pvc %v-wal -n %v --wait=false", oldPrimaryPVCName, namespace))
				Expect(err).ToNot(HaveOccurred())
			}

			zero := int64(0)
			forceDelete := &client.DeleteOptions{
				GracePeriodSeconds: &zero,
			}
			// Deleting old primary pod
			err = env.DeletePod(namespace, oldPrimaryPodName, forceDelete)
			Expect(err).ToNot(HaveOccurred())

			// checking that pod and pvc should be removed
			NamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      oldPrimaryPodName,
			}
			Pod := &corev1.Pod{}
			err = env.Client.Get(env.Ctx, NamespacedName, Pod)
			Expect(err).To(HaveOccurred(), "pod %v is not deleted", oldPrimaryPodName)
		})
		By("verify new pod should join as standby", func() {
			newPodName := clusterName + "-4"
			newPodNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      newPodName,
			}
			Eventually(func() (bool, error) {
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, newPodNamespacedName, pod)
				if err != nil {
					return false, err
				}
				if utils.IsPodActive(*pod) || utils.IsPodReady(*pod) {
					return true, nil
				}
				return false, nil
			}, 300).Should(BeTrue())

			newPod := &corev1.Pod{}
			err = env.Client.Get(env.Ctx, newPodNamespacedName, newPod)
			Expect(err).ToNot(HaveOccurred())
			// check that pod should join as in recovery mode
			commandTimeout := time.Second * 5
			Eventually(func() (string, error) {
				stdOut, _, err := env.ExecCommand(env.Ctx, *newPod, specs.PostgresContainerName,
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", "select pg_is_in_recovery();")
				return strings.Trim(stdOut, "\n"), err
			}, 60, 2).Should(BeEquivalentTo("t"))
			// verify test data
			AssertDataExpectedCount(namespace, newPodName, tableName, 2)
		})
		// verify test data on new primary
		newPrimaryPodInfo, err = env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		AssertDataExpectedCount(namespace, newPrimaryPodInfo.GetName(), tableName, 2)
		assertClusterStandbysAreStreaming(namespace, clusterName)
	})
})
