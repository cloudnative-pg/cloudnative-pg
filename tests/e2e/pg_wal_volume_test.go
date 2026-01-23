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
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Separate pg_wal volume", Label(tests.LabelStorage), func() {
	const (
		sampleFileWithPgWal      = fixturesDir + "/pg_wal_volume/cluster-with-pg-wal-volume.yaml.template"
		sampleFileWithoutPgWal   = fixturesDir + "/pg_wal_volume/cluster-without-pg-wal-volume.yaml.template"
		sampleFileWithSwitchover = fixturesDir + "/pg_wal_volume/cluster-no-pg-wal-switchover-volume.yaml.template"
		clusterName              = "cluster-pg-wal-volume"
		level                    = tests.High
		expectedPvcCount         = 6
	)
	var namespace string
	verifyPgWal := func(namespace string) {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(len(podList.Items), err).To(BeEquivalentTo(3))
		By("checking that pg_wal PVC has been created", func() {
			for _, pod := range podList.Items {
				pvcName := pod.GetName() + "-wal"
				pvc := &corev1.PersistentVolumeClaim{}
				namespacedPVCName := types.NamespacedName{
					Namespace: namespace,
					Name:      pvcName,
				}
				err := env.Client.Get(env.Ctx, namespacedPVCName, pvc)
				Expect(pvc.GetName(), err).To(BeEquivalentTo(pvcName))
			}
			AssertPvcHasLabels(namespace, clusterName)
		})
		By("checking that pg_wal is a symlink to the dedicated volume", func() {
			for _, pod := range podList.Items {
				commandTimeout := time.Second * 10
				out, _, err := env.EventuallyExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
					"readlink", "-f", specs.PgWalPath)
				Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo(specs.PgWalVolumePgWalPath))
			}
		})
		By("checking WALs are archived in the dedicated volume", func() {
			for _, pod := range podList.Items {
				cmd := fmt.Sprintf(
					"find %v -maxdepth 1 -type f -regextype sed -regex %v -print | wc -l",
					specs.PgWalVolumePgWalPath,
					".*[0-9]$")
				timeout := 300
				Eventually(func() (int, error, error) {
					out, _, err := exec.CommandInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{
							Namespace: namespace,
							PodName:   pod.GetName(),
						}, nil,
						"sh", "-c", cmd)
					value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
					return value, err, atoiErr
				}, timeout).Should(BeNumerically(">=", 1))
			}
		})
	}

	// Inline function to patch walStorage in existing cluster
	updateWalStorage := func(namespace, clusterName string) {
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
			WalStorageClass := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
			cluster.Spec.WalStorage = &apiv1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &WalStorageClass,
			}
			return env.Client.Update(env.Ctx, cluster)
		})
		Expect(err).ToNot(HaveOccurred())
	}

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// This test checks for separate and dedicated pg_wal volume well behaving, by
	// ensuring WAL files are archived to the correct location and a symlink
	// to the PATH is present inside the PGDATA.
	It("having a dedicated WAL volume", func() {
		const namespacePrefix = "pg-wal-volume-e2e"
		var err error
		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFileWithPgWal, env)
		verifyPgWal(namespace)
	})

	assertAddWALVolume := func() {
		By(fmt.Sprintf("adding pg_wal volume in existing cluster: %v", clusterName), func() {
			updateWalStorage(namespace, clusterName)
		})
		AssertPVCCount(namespace, clusterName, expectedPvcCount, 120)
		AssertClusterEventuallyReachesPhase(namespace, clusterName,
			[]string{apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 30)
		AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)
		AssertClusterPhaseIsConsistent(namespace, clusterName, []string{apiv1.PhaseHealthy}, 30)
		verifyPgWal(namespace)
	}

	Context("on a plain cluster with primaryUpdateMethod defaulting to restart", func() {
		It("can add a dedicated WAL volume after cluster is created", func() {
			const namespacePrefix = "add-pg-wal-volume-e2e"
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFileWithoutPgWal, env)
			initialPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			assertAddWALVolume()

			By("verifying the primary did not switch", func() {
				AssertPrimaryWasUpdated(namespace, clusterName, initialPrimary, apiv1.PrimaryUpdateMethodRestart)
			})
		})
	})

	Context("on a plain cluster with primaryUpdateMethod set to switchover", func() {
		It("can add a dedicated WAL volume after cluster is created", func() {
			const namespacePrefix = "add-pg-wal-volume-e2e"
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFileWithSwitchover, env)
			initialPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			assertAddWALVolume()

			By("verifying the primary did switch", func() {
				AssertPrimaryWasUpdated(namespace, clusterName, initialPrimary, apiv1.PrimaryUpdateMethodSwitchover)
			})
		})
	})
})
