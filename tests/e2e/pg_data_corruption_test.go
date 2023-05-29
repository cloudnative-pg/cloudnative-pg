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

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PGDATA Corruption", Label(tests.LabelRecovery), func() {
	const (
		namespacePrefix = "pg-data-corruption"
		sampleFile      = fixturesDir + "/pg_data_corruption/cluster-pg-data-corruption.yaml.template"
		clusterName     = "cluster-pg-data-corruption"
		level           = tests.Medium
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	var namespace string
	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	It("can recover cluster after pgdata corruption on primary", func() {
		var oldPrimaryPodName, oldPrimaryPVCName string
		var err error
		tableName := "test_pg_data_corruption"
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})
		AssertCreateCluster(namespace, clusterName, sampleFile, env)
		AssertCreateTestData(namespace, clusterName, tableName, psqlClientPod)

		By("gathering current primary pod and pvc", func() {
			oldPrimaryPod, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			oldPrimaryPodName = oldPrimaryPod.GetName()
			// Get the PVC related to the pod
			pvcName := oldPrimaryPod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
			pvc := &corev1.PersistentVolumeClaim{}
			namespacedPVCName := types.NamespacedName{
				Namespace: namespace,
				Name:      pvcName,
			}
			err = env.Client.Get(env.Ctx, namespacedPVCName, pvc)
			Expect(err).ToNot(HaveOccurred())
			oldPrimaryPVCName = pvc.GetName()
		})

		By("corrupting primary pod by removing PGDATA", func() {
			cmd := fmt.Sprintf("kubectl exec %v -n %v postgres -- /bin/bash -c 'rm -fr %v/base/*'",
				oldPrimaryPodName, namespace, specs.PgDataPath)
			_, _, err = testsUtils.Run(cmd)
			Expect(err).ToNot(HaveOccurred())
		})

		By("verifying failover happened after the primary pod PGDATA got corrupted", func() {
			Eventually(func() string {
				newPrimaryPod, err := env.GetClusterPrimary(namespace, clusterName)
				if err != nil {
					return ""
				}
				return newPrimaryPod.GetName()
			}, 120, 5).ShouldNot(BeEquivalentTo(oldPrimaryPodName),
				"operator did not perform the failover")
		})

		By("verifying the old primary pod health", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      oldPrimaryPodName,
			}
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

		By("removing the old primary pod and its pvc", func() {
			// Check if walStorage is enabled
			walStorageEnabled, err := testsUtils.IsWalStorageEnabled(namespace, clusterName, env)
			Expect(err).ToNot(HaveOccurred())

			// Force delete setting
			quickDelete := &client.DeleteOptions{
				GracePeriodSeconds: &quickDeletionPeriod,
			}

			// removing old primary pod attached pvc
			namespacedPVCName := types.NamespacedName{
				Namespace: namespace,
				Name:      oldPrimaryPVCName,
			}
			oldPrimaryPVC := &corev1.PersistentVolumeClaim{}
			err = env.Client.Get(env.Ctx, namespacedPVCName, oldPrimaryPVC)
			Expect(err).ToNot(HaveOccurred())
			err = env.Client.Delete(env.Ctx, oldPrimaryPVC, quickDelete)
			Expect(err).ToNot(HaveOccurred())

			// removing walStorage PVC if needed
			if walStorageEnabled {
				oldPrimaryWalPVCName := fmt.Sprintf("%v-wal", oldPrimaryPVCName)
				namespacedWalPVCName := types.NamespacedName{
					Namespace: namespace,
					Name:      oldPrimaryWalPVCName,
				}
				oldPrimaryWalPVC := &corev1.PersistentVolumeClaim{}
				err = env.Client.Get(env.Ctx, namespacedWalPVCName, oldPrimaryWalPVC)
				Expect(err).ToNot(HaveOccurred())
				err = env.Client.Delete(env.Ctx, oldPrimaryWalPVC, quickDelete)
				Expect(err).ToNot(HaveOccurred())
			}

			// Deleting old primary pod
			err = env.DeletePod(namespace, oldPrimaryPodName, quickDelete)
			Expect(err).ToNot(HaveOccurred())

			// checking that the old primary pod is eventually gone
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      oldPrimaryPodName,
			}
			Eventually(func() bool {
				err := env.Client.Get(env.Ctx, namespacedName, &corev1.Pod{})
				return apierrs.IsNotFound(err)
			}, 300).Should(BeTrue())
		})

		By("verifying new pod should join as standby", func() {
			newPodName := clusterName + "-4"
			newPodNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      newPodName,
			}
			Eventually(func() (bool, error) {
				pod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, newPodNamespacedName, &pod)
				if err != nil {
					return false, err
				}
				if utils.IsPodActive(pod) && utils.IsPodReady(pod) && specs.IsPodStandby(pod) {
					return true, nil
				}
				return false, nil
			}, 300).Should(BeTrue())
		})
		AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReadyQuick], env)
		AssertDataExpectedCount(namespace, clusterName, tableName, 2, psqlClientPod)
		AssertClusterStandbysAreStreaming(namespace, clusterName, 120)
	})
})
