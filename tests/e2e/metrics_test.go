package e2e

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Metrics", func() {
	// Cluster identifiers
	const namespace = "cluster-metrics-e2e"
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
	It("can gather metrics", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("collecting metrics on each pod", func() {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			// Gather metrics in each pod
			for _, pod := range podList.Items {
				pod := pod // pin the variable
				// cnp_collector_last_collection_error returns 0 if no metric collection failed
				metricsCmd := "sh -c 'curl -s 127.0.0.1:8000/metrics | grep ^cnp_collector_last_collection_error'"

				out, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					pod.GetName(),
					metricsCmd))
				Expect(err).ToNot(HaveOccurred())
				collectionError, err := strconv.Atoi(strings.Split(strings.TrimSpace(out), " ")[1])
				Expect(collectionError, err).To(BeEquivalentTo(0))
			}
		})
	})
})
