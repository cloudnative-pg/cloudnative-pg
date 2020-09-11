/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/
package performance

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster", func() {
	// Confirm that a standby closely following the primary doesn't need more
	// than 5 seconds to be promoted and be able to start inserting records.
	// We test this setting up an application pointing to the rw service,
	// forcing a failover and measuring how much time passes between the
	// last row written on timeline 1 and the first one on timeline 2
	Context("Cluster primary fails over in less than five seconds", func() {
		const namespace = "primary-failover-time"
		const sampleFile = samplesDir + "/cluster-example.yaml"
		const clusterName = "cluster-example"
		BeforeEach(func() {
			if err := env.CreateNamespace(namespace); err != nil {
				Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
			}
		})
		AfterEach(func() {
			if err := env.DeleteNamespace(namespace); err != nil {
				Fail(fmt.Sprintf("Unable to delete %v namespace", namespace))
			}
		})
		It("can fail over in less than five seconds", func() {
			By(fmt.Sprintf("having a %v namespace", namespace), func() {
				// Creating a namespace should be quick
				timeout := 20
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      namespace,
				}

				Eventually(func() (string, error) {
					cr := &corev1.Namespace{}
					err := env.Client.Get(env.Ctx, namespacedName, cr)
					return cr.GetName(), err
				}, timeout).Should(BeEquivalentTo(namespace))
			})
			By(fmt.Sprintf("creating a Cluster in the %v namespace",
				namespace), func() {
				_, _, err := tests.Run(
					"kubectl create -n " + namespace + " -f " + sampleFile)
				Expect(err).To(BeNil())
			})
			By("having a Cluster with three instances ready", func() {
				timeout := 60
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}

				Eventually(func() (int32, error) {
					cr := &clusterv1alpha1.Cluster{}
					err := env.Client.Get(env.Ctx, namespacedName, cr)
					return cr.Status.ReadyInstances, err
				}, timeout).Should(BeEquivalentTo(3))
			})
			// Node 1 should be the primary, so the -rw service should
			// point there. We verify this.
			By("having the current primary on node1", func() {
				endpointName := clusterName + "-rw"
				endpointCr := &corev1.Endpoints{}
				endpointNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      endpointName,
				}
				podName := clusterName + "-1"
				podCr := &corev1.Pod{}
				podNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}
				err := env.Client.Get(env.Ctx, endpointNamespacedName,
					endpointCr)
				Expect(err).To(BeNil())
				err = env.Client.Get(env.Ctx, podNamespacedName, podCr)
				Expect(err).To(BeNil())
				Expect(endpointCr.Subsets[0].Addresses[0].IP).To(
					BeEquivalentTo(podCr.Status.PodIP))
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
				lm := clusterName + "-1"
				lmCr := &corev1.Pod{}
				lmNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      lm,
				}
				err := env.Client.Get(env.Ctx, lmNamespacedName, lmCr)
				Expect(err).To(BeNil())
				_, _, err = utils.ExecCommand(env.Ctx, *lmCr, "postgres",
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
				Expect(err).To(BeNil())
			})
			By("starting load", func() {
				// We set up hey and postgrest. Hey, a load generator,
				// continuously calls postgrest api to execute inserts
				// on the postgres primary. We make sure that the first
				// records appear on the database before moving to the next
				// step.

				_, _, err := tests.Run("kubectl create -n " + namespace +
					" -f ./fixtures/fastfailover/postgrest.yaml")
				Expect(err).To(BeNil())
				_, _, err = tests.Run("kubectl create -n " + namespace +
					" -f ./fixtures/fastfailover/hey-job.yaml")
				Expect(err).To(BeNil())

				commandTimeout := time.Second * 2
				timeout := 60
				lm := clusterName + "-1"
				lmNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      lm,
				}
				Eventually(func() (string, error) {
					lmCr := &corev1.Pod{}
					err := env.Client.Get(env.Ctx, lmNamespacedName, lmCr)
					out, _, _ := utils.ExecCommand(env.Ctx, *lmCr, "postgres",
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
				Expect(err).To(BeNil())
			})

			// Take the time when the pod was deleted
			start := time.Now()

			By("waiting for the first write with on timeline 2", func() {
				// One of the standbys will be promoted and the rw service
				// should point to it. We'll be able to recognise the records
				// inserted after the promotion because they'll be marked
				// with timeline '00000002'. There should be one of them
				// in the database soon.

				commandTimeout := time.Second * 2
				timeout := 10
				lm := clusterName + "-2"
				lmNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      lm,
				}
				Eventually(func() (string, error) {
					lmCr := &corev1.Pod{}
					err := env.Client.Get(env.Ctx, lmNamespacedName, lmCr)
					out, _, _ := utils.ExecCommand(env.Ctx, *lmCr, "postgres",
						&commandTimeout, "psql", "-U", "postgres", "app", "-tAc",
						"SELECT count(*) > 0 FROM tps.tl "+
							"WHERE timeline = '00000002'")
					return strings.TrimSpace(out), err
				}, timeout).Should(BeEquivalentTo("t"))
			})
			By("resuming writing in less than 5 sec", func() {
				// We measure the difference between the last entry with
				// timeline 1 and the first one with timeline 2.
				// It should be less than 5 seconds.
				query := "WITH a AS ( " +
					"  SELECT * " +
					"  , t-lag(t) OVER (order by t) AS timediff " +
					"  FROM tps.tl " +
					") " +
					"SELECT EXTRACT ('EPOCH' FROM timediff) " +
					"FROM a " +
					"WHERE timeline = ( " +
					"  SELECT timeline " +
					"  FROM tps.tl " +
					"  ORDER BY t DESC " +
					"  LIMIT 1 " +
					") " +
					"ORDER BY t ASC " +
					"LIMIT 1;"
				lm := clusterName + "-2"
				lmNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      lm,
				}
				var switchTime float64
				commandTimeout := time.Second * 5
				lmCr := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, lmNamespacedName, lmCr)
				Expect(err).To(BeNil())
				out, _, _ := utils.ExecCommand(env.Ctx, *lmCr, "postgres",
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
				switchTime, err = strconv.ParseFloat(strings.TrimSpace(out), 64)
				Expect(err).To(BeNil())
				fmt.Printf("Failover performed in %v seconds\n", switchTime)
				Expect(switchTime).Should(BeNumerically("<", 5))
			})

			By("recovering from degraded state having a cluster with 3 instances ready", func() {
				// Recreating an instance usually takes 15s`
				timeout := 45
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				var elapsed time.Duration
				Eventually(func() int32 {
					cr := &clusterv1alpha1.Cluster{}
					if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
						Fail("Unable to get cluster " + clusterName)
					}
					elapsed = time.Since(start)
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(3))

				fmt.Printf("Cluster has been in a degraded state for %v seconds\n", elapsed)

				Expect(elapsed / time.Second).Should(BeNumerically("<", 30))
			})
		})
	})
})
