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
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PGDATA Corruption", Label(tests.LabelRecovery), Ordered, func() {
	const (
		namespacePrefix = "pg-data-corruption"
		level           = tests.Medium
	)
	var namespace string
	BeforeAll(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		var err error
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
	})

	testDataCorruption := func(
		namespace string,
		sampleFile string,
	) {
		var oldPrimaryPodName, oldPrimaryPVCName string
		var err error
		tableName := "test_pg_data_corruption"
		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)
		tableLocator := TableLocator{
			Namespace:    namespace,
			ClusterName:  clusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		AssertCreateTestData(env, tableLocator)

		By("gathering current primary pod and pvc", func() {
			oldPrimaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
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
			cmd := fmt.Sprintf("find %v/base/* -type f -delete", specs.PgDataPath)
			_, _, err = exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   oldPrimaryPodName,
				}, nil,
				"/bin/bash", "-c", cmd)
			Expect(err).ToNot(HaveOccurred())
		})

		By("verifying failover happened after the primary pod PGDATA got corrupted", func() {
			Eventually(func() (string, error) {
				newPrimaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				if err != nil {
					return "", err
				}
				return newPrimaryPod.GetName(), nil
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
			walStorageEnabled, err := storage.IsWalStorageEnabled(
				env.Ctx, env.Client,
				namespace, clusterName,
			)
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
			err = podutils.Delete(env.Ctx, env.Client, namespace, oldPrimaryPodName, quickDelete)
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
			Eventually(func() (bool, error) {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				if err != nil {
					return false, err
				}
				// Find a pod that wasn't the deleted primary and is a standby
				for _, pod := range podList.Items {
					if pod.Name != oldPrimaryPodName && utils.IsPodActive(pod) &&
						utils.IsPodReady(pod) && specs.IsPodStandby(pod) {
						return true, nil
					}
				}
				return false, nil
			}, 300).Should(BeTrue())
		})
		AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReadyQuick], env)
		AssertDataExpectedCount(env, tableLocator, 2)
		AssertClusterStandbysAreStreaming(namespace, clusterName, 140)
	}

	Context("plain cluster", func() {
		It("can recover cluster after pgdata corruption on primary", func() {
			const sampleFile = fixturesDir + "/pg_data_corruption/cluster-pg-data-corruption.yaml.template"
			DeferCleanup(func() {
				_ = DeleteResourcesFromFile(namespace, sampleFile)
			})
			testDataCorruption(namespace, sampleFile)
		})
	})

	Context("cluster without replication slots", func() {
		It("can recover cluster after pgdata corruption on primary", func() {
			const sampleFile = fixturesDir + "/pg_data_corruption/cluster-pg-data-corruption-no-slots.yaml.template"
			DeferCleanup(func() {
				_ = DeleteResourcesFromFile(namespace, sampleFile)
			})
			testDataCorruption(namespace, sampleFile)
		})
	})

	Context("cluster with managed roles", func() {
		It("can recover cluster after pgdata corruption on primary", func() {
			const sampleFile = fixturesDir + "/pg_data_corruption/cluster-pg-data-corruption-roles.yaml.template"
			DeferCleanup(func() {
				_ = DeleteResourcesFromFile(namespace, sampleFile)
			})
			testDataCorruption(namespace, sampleFile)
		})
	})
})
