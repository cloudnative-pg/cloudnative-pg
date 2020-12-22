package e2e

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster setup", func() {
	const namespace = "cluster-storageclass-e2e"
	const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml"
	const clusterName = "postgresql-storage-class"
	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+".log")
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
			_, _, err = env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &aSecond,
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
			_, _, err = env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &aSecond,
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
