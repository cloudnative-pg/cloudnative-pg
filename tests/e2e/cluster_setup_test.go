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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster setup", func() {
	const (
		namespace   = "cluster-storageclass-e2e"
		sampleFile  = fixturesDir + "/base/cluster-storage-class.yaml"
		clusterName = "postgresql-storage-class"
		level       = tests.Highest
	)
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
	It("sets up a cluster", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("having three PostgreSQL pods with status ready", func() {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(utils.CountReadyPods(podList.Items), err).Should(BeEquivalentTo(3))
		})

		By("being able to restart a killed pod without losing it", func() {
			aSecond := time.Second
			timeout := 120
			podName := clusterName + "-1"
			pod := &corev1.Pod{}
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      podName,
			}
			err := env.Client.Get(env.Ctx, namespacedName, pod)
			Expect(err).ToNot(HaveOccurred())

			// Put something in the database. We'll check later if it
			// still exists
			query := "CREATE TABLE test (id bigserial PRIMARY KEY, t text)"
			_, _, err = env.EventuallyExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &aSecond,
				"psql", "-U", "postgres", "app", "-tAc", query)
			Expect(err).ToNot(HaveOccurred())

			// We kill the pid 1 process.
			// The pod should be restarted and the count of the restarts
			// should increase by one
			restart := int32(-1)
			for _, data := range pod.Status.ContainerStatuses {
				if data.Name == specs.PostgresContainerName {
					restart = data.RestartCount
				}
			}
			_, _, err = env.EventuallyExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &aSecond,
				"sh", "-c", "kill 1")
			Expect(err).ToNot(HaveOccurred())
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
			}, timeout).Should(BeEquivalentTo(restart + 1))

			// That pod should also be ready
			Eventually(func() (bool, error) {
				pod := &corev1.Pod{}
				if err := env.Client.Get(env.Ctx, namespacedName, pod); err != nil {
					return false, err
				}

				if !utils.IsPodActive(*pod) || !utils.IsPodReady(*pod) {
					return false, nil
				}

				query = "SELECT * FROM test"
				_, _, err = env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &aSecond,
					"psql", "-U", "postgres", "app", "-tAc", query)
				return err == nil, err
			}, timeout).Should(BeTrue())
		})
	})
})
