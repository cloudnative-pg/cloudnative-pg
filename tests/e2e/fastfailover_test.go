/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fast failover", Serial, Label(tests.LabelPerformance), func() {
	const (
		sampleFile             = fixturesDir + "/fastfailover/cluster-fast-failover.yaml"
		sampleFileSyncReplicas = fixturesDir + "/fastfailover/cluster-syncreplicas-fast-failover.yaml"
		webTestFile            = fixturesDir + "/fastfailover/webtest.yaml"
		webTestSyncReplicas    = fixturesDir + "/fastfailover/webtest-syncreplicas.yaml"
		webTestJob             = fixturesDir + "/fastfailover/hey-job-webtest.yaml"
		level                  = tests.Highest
	)
	var (
		namespace       string
		clusterName     string
		maxReattachTime int32 = 60
		maxFailoverTime int32 = 10
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		// Sometimes on AKS the promotion itself takes more than 10 seconds.
		// Nothing to be done operator side, we raise the timeout to avoid
		// failures in the test.
		isAKS, err := env.IsAKS()
		if err != nil {
			fmt.Println("Couldn't verify if tests are running on AKS, assuming they aren't")
		}

		if isAKS {
			maxFailoverTime = 30
		}

		// GKE has a higher kube-proxy timeout, and the connections could try
		// using a service, for which the routing table hasn't changed, getting
		// stuck for a while.
		// We raise the timeout, since we can't intervene on GKE configuration.
		isGKE, err := env.IsGKE()
		if err != nil {
			fmt.Println("Couldn't verify if tests are running on GKE, assuming they aren't")
		}

		if isGKE {
			maxReattachTime = 180
			maxFailoverTime = 20
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

	Context("with async replicas cluster", func() {
		// Confirm that a standby closely following the primary doesn't need more
		// than 10 seconds to be promoted and be able to start inserting records.
		// We test this setting up an application pointing to the rw service,
		// forcing a failover and measuring how much time passes between the
		// last row written on timeline 1 and the first one on timeline 2.
		It("can do a fast failover", func() {
			namespace = "primary-failover-time"
			clusterName = "cluster-fast-failover"
			AssertFastFailOver(namespace, sampleFile, clusterName, webTestFile, webTestJob, maxReattachTime, maxFailoverTime)
		})
	})

	Context("with sync replicas cluster", func() {
		It("can do a fast failover", func() {
			namespace = "primary-failover-time-sync-replicas"
			clusterName = "cluster-syncreplicas-fast-failover"
			AssertFastFailOver(
				namespace, sampleFileSyncReplicas, clusterName, webTestSyncReplicas, webTestJob, maxReattachTime, maxFailoverTime)
		})
	})
})

func AssertFastFailOver(
	namespace,
	sampleFile,
	clusterName,
	webTestFile,
	webTestJob string,
	maxReattachTime,
	maxFailoverTime int32) {
	// Create a cluster in a namespace we'll delete after the test
	err := env.CreateNamespace(namespace)
	Expect(err).ToNot(HaveOccurred())

	By(fmt.Sprintf("having a %v namespace", namespace), func() {
		// Creating a namespace should be quick
		timeout := 20
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      namespace,
		}

		Eventually(func() (string, error) {
			namespaceResource := &corev1.Namespace{}
			err = env.Client.Get(env.Ctx, namespacedName, namespaceResource)
			return namespaceResource.GetName(), err
		}, timeout).Should(BeEquivalentTo(namespace))
	})

	By(fmt.Sprintf("creating a Cluster in the %v namespace",
		namespace), func() {
		_, _, err = tests.Run(
			"kubectl create -n " + namespace + " -f " + sampleFile)
		Expect(err).ToNot(HaveOccurred())
	})

	By("having a Cluster with three instances ready", func() {
		AssertClusterIsReady(namespace, clusterName, 600, env)
	})

	// Node 1 should be the primary, so the -rw service should
	// point there. We verify this.
	By("having the current primary on node1", func() {
		endpointName := clusterName + "-rw"
		endpoint := &corev1.Endpoints{}
		endpointNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      endpointName,
		}
		podName := clusterName + "-1"
		pod := &corev1.Pod{}
		podNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      podName,
		}
		err = env.Client.Get(env.Ctx, endpointNamespacedName,
			endpoint)
		Expect(err).ToNot(HaveOccurred())
		err = env.Client.Get(env.Ctx, podNamespacedName, pod)
		Expect(tests.FirstEndpointIP(endpoint), err).To(
			BeEquivalentTo(pod.Status.PodIP))
	})

	By("preparing the db for the test scenario", func() {
		// Create the table used by the scenario
		query := "CREATE SCHEMA tps; " +
			"CREATE TABLE tps.tl ( " +
			"id BIGSERIAL" +
			", timeline TEXT DEFAULT (substring(pg_walfile_name(" +
			"    pg_current_wal_lsn()), 1, 8))" +
			", t timestamp DEFAULT (clock_timestamp() AT TIME ZONE 'UTC')" +
			", source text NOT NULL" +
			", PRIMARY KEY (id)" +
			")"

		commandTimeout := time.Second * 5
		primaryPodName := clusterName + "-1"
		primaryPod := &corev1.Pod{}
		primaryPodNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      primaryPodName,
		}

		err = env.Client.Get(env.Ctx, primaryPodNamespacedName, primaryPod)
		Expect(err).ToNot(HaveOccurred())
		_, _, err = env.ExecCommand(env.Ctx, *primaryPod, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
	})

	By("starting load", func() {
		// We set up hey and webtest. Hey, a load generator,
		// continuously calls the webtest api to execute inserts
		// on the postgres primary. We make sure that the first
		// records appear on the database before moving to the next
		// step.
		_, _, err = tests.Run("kubectl create -n " + namespace +
			" -f " + webTestFile)
		Expect(err).ToNot(HaveOccurred())

		_, _, err = tests.Run("kubectl create -n " + namespace +
			" -f " + webTestJob)
		Expect(err).ToNot(HaveOccurred())

		commandTimeout := time.Second * 2
		timeout := 60
		primaryPodName := clusterName + "-1"
		primaryPodNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      primaryPodName,
		}

		Eventually(func() (string, error) {
			primaryPod := &corev1.Pod{}
			err = env.Client.Get(env.Ctx, primaryPodNamespacedName, primaryPod)
			out, _, _ := env.ExecCommand(env.Ctx, *primaryPod, "postgres",
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc",
				"SELECT count(*) > 0 FROM tps.tl")
			return strings.TrimSpace(out), err
		}, timeout).Should(BeEquivalentTo("t"))
	})

	By("deleting the primary", func() {
		// The primary is force-deleted.
		zero := int64(0)
		forceDelete := &ctrlclient.DeleteOptions{
			GracePeriodSeconds: &zero,
		}
		lm := clusterName + "-1"
		err = env.DeletePod(namespace, lm, forceDelete)

		Expect(err).ToNot(HaveOccurred())
	})

	AssertStandbysFollowPromotion(namespace, clusterName, maxReattachTime)

	AssertWritesResumedBeforeTimeout(namespace, clusterName, maxFailoverTime)
}
