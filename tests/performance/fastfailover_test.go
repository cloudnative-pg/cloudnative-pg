/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package performance

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fast failover", func() {
	const namespace = "primary-failover-time"
	const sampleFile = "./fixtures/base/cluster-example.yaml"
	const clusterName = "cluster-example"
	const maxFailoverTime = 10
	const maxReattachTime = 60
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
	// Confirm that a standby closely following the primary doesn't need more
	// than 10 seconds to be promoted and be able to start inserting records.
	// We test this setting up an application pointing to the rw service,
	// forcing a failover and measuring how much time passes between the
	// last row written on timeline 1 and the first one on timeline 2
	It(fmt.Sprintf("can fail over in less than %v seconds", maxFailoverTime), func() {
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
				err := env.Client.Get(env.Ctx, namespacedName, namespaceResource)
				return namespaceResource.GetName(), err
			}, timeout).Should(BeEquivalentTo(namespace))
		})
		By(fmt.Sprintf("creating a Cluster in the %v namespace",
			namespace), func() {
			_, _, err := tests.Run(
				"kubectl create -n " + namespace + " -f " + sampleFile)
			Expect(err).ToNot(HaveOccurred())
		})
		By("having a Cluster with three instances ready", func() {
			timeout := 600
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}

			Eventually(func() (int32, error) {
				cluster := &clusterv1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.ReadyInstances, err
			}, timeout).Should(BeEquivalentTo(3))
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
			err := env.Client.Get(env.Ctx, endpointNamespacedName,
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
			err := env.Client.Get(env.Ctx, primaryPodNamespacedName, primaryPod)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = env.ExecCommand(env.Ctx, *primaryPod, "postgres",
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
			Expect(err).ToNot(HaveOccurred())
		})
		By("starting load", func() {
			// We set up hey and postgrest. Hey, a load generator,
			// continuously calls postgrest api to execute inserts
			// on the postgres primary. We make sure that the first
			// records appear on the database before moving to the next
			// step.

			_, _, err := tests.Run("kubectl create -n " + namespace +
				" -f ./fixtures/base/postgrest.yaml")
			Expect(err).ToNot(HaveOccurred())
			_, _, err = tests.Run("kubectl create -n " + namespace +
				" -f ./fixtures/base/hey-job.yaml")
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
				err := env.Client.Get(env.Ctx, primaryPodNamespacedName, primaryPod)
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
			err := env.DeletePod(namespace, lm, forceDelete)
			Expect(err).ToNot(HaveOccurred())
		})

		AssertStandbysFollowPromotion(namespace, clusterName, maxReattachTime)

		AssertWritesResumedBeforeTimeout(namespace, clusterName, maxFailoverTime)
	})
})
