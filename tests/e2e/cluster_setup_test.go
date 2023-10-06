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
	"bytes"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster setup", Label(tests.LabelSmoke, tests.LabelBasic), func() {
	const (
		sampleFile  = fixturesDir + "/base/cluster-storage-class.yaml.template"
		clusterName = "postgresql-storage-class"
		level       = tests.Highest
	)
	var namespace string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("sets up a cluster", func(ctx SpecContext) {
		const namespacePrefix = "cluster-storageclass-e2e"
		var err error

		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})

		AssertCreateCluster(namespace, clusterName, sampleFile, env)
		cluster, err := env.GetCluster(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		var buf bytes.Buffer
		GinkgoWriter.Println("Putting Tail on the cluster logs")
		go func() {
			err = env.TailClusterLogs(cluster, &buf, false)
			if err != nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "\nError tailing cluster logs: %v\n", err)
			}
		}()
		DeferCleanup(func(ctx SpecContext) {
			if CurrentSpecReport().Failed() {
				specName := CurrentSpecReport().FullText()
				capLines := 10
				GinkgoWriter.Printf("DUMPING tailed Cluster Logs with error/warning (at most %v lines). Failed Spec: %v\n",
					capLines, specName)
				GinkgoWriter.Println("================================================================================")
				saveLogs(&buf, "cluster_logs_", strings.ReplaceAll(specName, " ", "_"), GinkgoWriter, capLines)
				GinkgoWriter.Println("================================================================================")
			}
		})

		By("having three PostgreSQL pods with status ready", func() {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(utils.CountReadyPods(podList.Items), err).Should(BeEquivalentTo(3))
		})

		By("being able to restart a killed pod without losing it", func() {
			commandTimeout := time.Second * 10
			timeout := 120
			podName := clusterName + "-1"
			pod := &corev1.Pod{}
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      podName,
			}
			err := env.Client.Get(env.Ctx, namespacedName, pod)
			Expect(err).ToNot(HaveOccurred())

			// Put something in the database. We'll check later if it still exists
			appUser, appUserPass, err := testsUtils.GetCredentials(
				clusterName, namespace, apiv1.ApplicationUserSecretSuffix, env)
			Expect(err).NotTo(HaveOccurred())
			host, err := testsUtils.GetHostName(namespace, clusterName, env)
			Expect(err).NotTo(HaveOccurred())
			query := "CREATE TABLE IF NOT EXISTS test (id bigserial PRIMARY KEY, t text);"
			_, _, err = testsUtils.RunQueryFromPod(
				psqlClientPod,
				host,
				testsUtils.AppDBName,
				appUser,
				appUserPass,
				query,
				env,
			)
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
			_, _, err = env.EventuallyExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
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

			Eventually(func() (bool, error) {
				query = "SELECT * FROM test"
				_, _, err = env.ExecCommandWithPsqlClient(
					namespace,
					clusterName,
					psqlClientPod,
					apiv1.ApplicationUserSecretSuffix,
					testsUtils.AppDBName,
					query,
				)
				return err == nil, err
			}, timeout).Should(BeTrue())
		})
	})

	It("tests cluster readiness conditions work", func() {
		const namespacePrefix = "cluster-conditions"

		var err error
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})

		By(fmt.Sprintf("having a %v namespace", namespace), func() {
			// Creating a namespace should be quick
			timeout := 20
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      namespace,
			}
			Eventually(func() (string, error) {
				namespaceResource := &corev1.Namespace{}
				err := env.Client.Get(env.Ctx, namespacedName, namespaceResource)
				return namespaceResource.GetName(), err
			}, timeout).Should(BeEquivalentTo(namespace))
		})

		By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
			CreateResourceFromFile(namespace, sampleFile)
		})

		By("verifying cluster reaches ready condition", func() {
			AssertClusterReadinessStatusIsReached(namespace, clusterName, apiv1.ConditionTrue, 600, env)
		})

		// scale up the cluster to verify if the cluster remains in Ready
		By("scaling up the cluster size", func() {
			err := env.ScaleClusterSize(namespace, clusterName, 5)
			Expect(err).ToNot(HaveOccurred())
		})

		By("verifying cluster readiness condition is false just after scale-up", func() {
			// Just after scale up the cluster, the condition status set to be `False` and cluster is not ready state.
			AssertClusterReadinessStatusIsReached(namespace, clusterName, apiv1.ConditionFalse, 180, env)
		})

		By("verifying cluster reaches ready condition after additional waiting", func() {
			AssertClusterReadinessStatusIsReached(namespace, clusterName, apiv1.ConditionTrue, 180, env)
		})
	})
})
